package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// auditUser records an event about a person in every organization they belong to, so each
// organization's admins can see it. A person with no organization gets one hub-level event.
func auditUser(ctx context.Context, q *db.Queries, userID uuid.UUID, actor *uuid.UUID, typ, summary string) error {
	orgs, err := q.ListUserOrganizations(ctx, userID)
	if err != nil {
		return err
	}
	insert := func(org *uuid.UUID) error {
		return q.InsertAuditEvent(ctx, db.InsertAuditEventParams{ID: newID(), OrgID: org, ActorUserID: actor, Type: typ, Summary: summary})
	}
	if len(orgs) == 0 {
		return insert(nil)
	}
	for _, o := range orgs {
		if err := insert(&o.ID); err != nil {
			return err
		}
	}
	return nil
}

// claimCode checks an authenticator code for a signed-in person (changing something sensitive
// asks again) and spends its time step.
func (s *Server) claimCode(ctx context.Context, userID uuid.UUID, code string) error {
	u, err := s.Store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.TotpEnabledAt == nil || u.TotpSecret == nil {
		return errNotEnrolled
	}
	step, ok := auth.CheckTotp(*u.TotpSecret, code, s.Now())
	if !ok {
		return errBadCode
	}
	n, err := s.Store.ClaimTotpStep(ctx, db.ClaimTotpStepParams{ID: userID, TotpLastStep: &step})
	if err != nil {
		return err
	}
	if n == 0 {
		return errBadCode
	}
	return nil
}

func (s *Server) decodeCode(r *http.Request) (string, error) {
	var req struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return "", err
	}
	if req.Code == "" {
		return "", httpx.Validation{"code": "Enter your code."}.Err()
	}
	return req.Code, nil
}

// handleNewRecoveryCodes replaces the recovery codes; the old ones stop working.
func (s *Server) handleNewRecoveryCodes(w http.ResponseWriter, r *http.Request) error {
	code, err := s.decodeCode(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	if err := s.claimCode(ctx, id.UserID, code); err != nil {
		return err
	}
	codes := auth.NewRecoveryCodes(auth.RecoveryCodeCount)
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		if err := q.DeleteRecoveryCodes(ctx, id.UserID); err != nil {
			return err
		}
		for _, c := range codes {
			if err := q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{ID: newID(), UserID: id.UserID, CodeHash: auth.HashRecoveryCode(c)}); err != nil {
				return err
			}
		}
		return auditUser(ctx, q, id.UserID, &id.UserID, "user.recovery_codes_renewed", "made new two-factor recovery codes")
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
	return nil
}

// handleChangeTwoFactor clears the authenticator and recovery codes, ends every other session
// and sends this one back to enroll, so the person sets up a new app right away.
func (s *Server) handleChangeTwoFactor(w http.ResponseWriter, r *http.Request) error {
	code, err := s.decodeCode(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	if err := s.claimCode(ctx, id.UserID, code); err != nil {
		return err
	}
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		if err := q.ClearUserTotp(ctx, id.UserID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, id.UserID); err != nil {
			return err
		}
		if err := q.DeleteOtherUserSessions(ctx, db.DeleteOtherUserSessionsParams{UserID: id.UserID, ID: id.SessionID}); err != nil {
			return err
		}
		if err := q.MarkSessionPending(ctx, id.SessionID); err != nil {
			return err
		}
		return auditUser(ctx, q, id.UserID, &id.UserID, "user.two_factor_changed", "is changing their authenticator")
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"next": "enroll"})
	return nil
}

// ResetTwoFactor clears a person's authenticator and recovery codes and ends all their
// sessions, so they enroll again at next sign-in, then emails them. actor is the hub admin
// who asked, or nil when it was done from the server.
func ResetTwoFactor(ctx context.Context, st *store.Store, mailer mail.Sender, userID uuid.UUID, actor *uuid.UUID) error {
	var u db.User
	by := "someone with access to the hub’s server"
	err := st.Tx(ctx, func(q *db.Queries) error {
		var err error
		if u, err = q.GetUserByID(ctx, userID); err != nil {
			return err
		}
		if actor != nil {
			a, err := q.GetUserByID(ctx, *actor)
			if err != nil {
				return err
			}
			by = "the hub admin " + a.Name
		}
		if err := q.ClearUserTotp(ctx, userID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
			return err
		}
		if err := q.DeleteUserSessions(ctx, userID); err != nil {
			return err
		}
		return auditUser(ctx, q, userID, actor, "user.two_factor_reset", "had two-factor reset by "+by)
	})
	if err != nil {
		return err
	}
	if err := mailer.Send(ctx, mail.Message{
		To:      u.Email,
		Subject: "Your Canopy two-factor setup was reset",
		Text: fmt.Sprintf("Your two-factor setup was reset by %s. You were signed out everywhere and will set up a new authenticator app the next time you sign in.\n\n"+
			"If you didn’t expect this, tell your hub admin.\n", by),
	}); err != nil {
		slog.ErrorContext(ctx, "send two-factor reset email", "err", err)
	}
	return nil
}

// ResetTwoFactorByEmail is what `canopy-hub reset-two-factor` runs on the server.
func ResetTwoFactorByEmail(ctx context.Context, st *store.Store, mailer mail.Sender, email string) error {
	u, err := st.GetUserByEmail(ctx, normalizeEmail(email))
	if store.IsNotFound(err) {
		return fmt.Errorf("no account uses %s", email)
	} else if err != nil {
		return err
	}
	return ResetTwoFactor(ctx, st, mailer, u.ID, nil)
}

// handleAdminResetTwoFactor is behind requireHubAdmin, so everyone else gets 404.
func (s *Server) handleAdminResetTwoFactor(w http.ResponseWriter, r *http.Request) error {
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	actor := identity(r).UserID
	if err := ResetTwoFactor(r.Context(), s.Store, s.Mail, userID, &actor); err != nil {
		if store.IsNotFound(err) {
			return httpx.ErrNotFound
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// notifyRecoveryCodeUsed warns the person that a recovery code signed someone in.
func (s *Server) notifyRecoveryCodeUsed(ctx context.Context, userID uuid.UUID, email string) {
	left, _ := s.Store.CountUnusedRecoveryCodes(ctx, userID)
	if err := auditUser(ctx, s.Store.Queries, userID, &userID, "user.recovery_code_used", "signed in with a recovery code"); err != nil {
		slog.ErrorContext(ctx, "audit recovery code use", "err", err)
	}
	if err := s.Mail.Send(ctx, mail.Message{
		To:      email,
		Subject: "A recovery code was used on your Canopy account",
		Text: fmt.Sprintf("Someone signed in to your Canopy account with a two-factor recovery code. You have %d left.\n\n"+
			"If that wasn’t you, change your password and your authenticator right away.\n", left),
	}); err != nil {
		slog.ErrorContext(ctx, "send recovery code email", "err", err)
	}
}
