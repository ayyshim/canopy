package api

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/skip2/go-qrcode"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
)

const maxTwoFactorFailures = 5

var (
	errAlreadyEnrolled = httpx.NewError(http.StatusConflict, "already_enrolled", "Two-factor is already set up for this account.")
	errNotEnrolled     = httpx.NewError(http.StatusConflict, "not_enrolled", "Set up two-factor first.")
	errNoSetup         = httpx.NewError(http.StatusConflict, "setup_required", "Start the setup first.")
	errBadCode         = httpx.FieldError(http.StatusUnprocessableEntity, "invalid_code", "code", "That code isn’t right. Try the next one.")
	errTooManyCodes    = httpx.NewError(http.StatusUnauthorized, "too_many_attempts", "Too many wrong codes. Sign in again.")
)

// twoFactorNext is the body of a sign-in that still needs a code.
func twoFactorNext(next string) map[string]string { return map[string]string{"two_factor": next} }

func (s *Server) handleTwoFactorState(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, map[string]string{"next": identity(r).Next()})
	return nil
}

// handleTwoFactorSetup makes a fresh secret for someone who isn't enrolled yet. The secret is
// shown here and never again once enrollment finishes.
func (s *Server) handleTwoFactorSetup(w http.ResponseWriter, r *http.Request) error {
	id := identity(r)
	ctx := r.Context()
	u, err := s.Store.GetUserByID(ctx, id.UserID)
	if err != nil {
		return err
	}
	if u.TotpEnabledAt != nil {
		return errAlreadyEnrolled
	}
	secret := auth.NewTotpSecret()
	n, err := s.Store.SetUserTotpSecret(ctx, db.SetUserTotpSecretParams{ID: id.UserID, TotpSecret: &secret})
	if err != nil {
		return err
	}
	if n == 0 {
		return errAlreadyEnrolled
	}
	uri := auth.TotpURL("Canopy", u.Email, secret)
	png, err := qrcode.Encode(uri, qrcode.Medium, 256)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]string{
		"secret":      secret,
		"otpauth_url": uri,
		"qr_png":      "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	})
	return nil
}

// wrongCode counts a failed attempt and ends the session at the limit.
func (s *Server) wrongCode(w http.ResponseWriter, r *http.Request) error {
	id := identity(r)
	n, err := s.Store.AddSessionTwoFactorFailure(r.Context(), id.SessionID)
	if err != nil {
		return err
	}
	if n >= maxTwoFactorFailures {
		if err := s.Store.DeleteSession(r.Context(), id.SessionID); err != nil {
			return err
		}
		s.Sessions.Clear(w)
		return errTooManyCodes
	}
	return errBadCode
}

// handleTwoFactorEnable checks the first code, turns two-factor on, verifies the session
// and returns the recovery codes. This is the only time they are shown.
func (s *Server) handleTwoFactorEnable(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := identity(r)
	ctx := r.Context()
	u, err := s.Store.GetUserByID(ctx, id.UserID)
	if err != nil {
		return err
	}
	if u.TotpEnabledAt != nil {
		return errAlreadyEnrolled
	}
	if u.TotpSecret == nil {
		return errNoSetup
	}
	step, ok := auth.CheckTotp(*u.TotpSecret, req.Code, s.Now())
	if !ok {
		return s.wrongCode(w, r)
	}
	codes := auth.NewRecoveryCodes(auth.RecoveryCodeCount)
	var me meView
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		n, err := q.EnableUserTotp(ctx, db.EnableUserTotpParams{ID: id.UserID, TotpLastStep: &step})
		if err != nil {
			return err
		}
		if n == 0 {
			return errAlreadyEnrolled
		}
		for _, c := range codes {
			if err := q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{
				ID: newID(), UserID: id.UserID, CodeHash: auth.HashRecoveryCode(c),
			}); err != nil {
				return err
			}
		}
		if err := q.MarkSessionVerified(ctx, id.SessionID); err != nil {
			return err
		}
		me, err = s.buildMe(ctx, q, id.UserID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recovery_codes": codes, "me": me})
	return nil
}

// handleTwoFactorVerify accepts an authenticator code or a recovery code and verifies the session.
func (s *Server) handleTwoFactorVerify(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Code         string `json:"code"`
		RecoveryCode string `json:"recovery_code"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := identity(r)
	ctx := r.Context()
	u, err := s.Store.GetUserByID(ctx, id.UserID)
	if err != nil {
		return err
	}
	if u.TotpEnabledAt == nil || u.TotpSecret == nil {
		return errNotEnrolled
	}
	ok, usedRecovery := false, false
	switch {
	case strings.TrimSpace(req.RecoveryCode) != "":
		n, err := s.Store.ClaimRecoveryCode(ctx, db.ClaimRecoveryCodeParams{
			UserID: id.UserID, CodeHash: auth.HashRecoveryCode(req.RecoveryCode),
		})
		if err != nil {
			return err
		}
		ok = n == 1
		usedRecovery = ok
	case strings.TrimSpace(req.Code) != "":
		if step, valid := auth.CheckTotp(*u.TotpSecret, req.Code, s.Now()); valid {
			// A step that was already used is refused, so a code works once.
			n, err := s.Store.ClaimTotpStep(ctx, db.ClaimTotpStepParams{ID: id.UserID, TotpLastStep: &step})
			if err != nil {
				return err
			}
			ok = n == 1
		}
	default:
		return httpx.Validation{"code": "Enter your code."}.Err()
	}
	if !ok {
		return s.wrongCode(w, r)
	}
	if err := s.Store.MarkSessionVerified(ctx, id.SessionID); err != nil {
		return err
	}
	if usedRecovery {
		s.notifyRecoveryCodeUsed(ctx, id.UserID, u.Email)
	}
	me, err := s.buildMe(ctx, s.Store.Queries, id.UserID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, me)
	return nil
}
