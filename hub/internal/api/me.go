package api

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/authz"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/store/db"
)

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) error {
	me, err := s.buildMe(r.Context(), s.Store.Queries, identity(r).UserID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, me)
	return nil
}

type patchMeRequest struct {
	Name            *string      `json:"name"`
	AvatarColor     *string      `json:"avatar_color"`
	LastOrgID       *uuid.UUID   `json:"last_org_id"`
	CurrentPassword *string      `json:"current_password"`
	NewPassword     *string      `json:"new_password"`
	Notifications   notify.Prefs `json:"notifications"`
}

// handlePatchMe updates the profile, switches the current organization and changes the
// password. A password change signs out every other session.
func (s *Server) handlePatchMe(w http.ResponseWriter, r *http.Request) error {
	var req patchMeRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	v := httpx.Validation{}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		req.Name = &n
		if n == "" {
			v.Add("name", "Enter your name.")
		} else if utf8.RuneCountInString(n) > 80 {
			v.Add("name", "Use at most 80 characters.")
		}
	}
	if req.AvatarColor != nil && !hexColor.MatchString(*req.AvatarColor) {
		v.Add("avatar_color", "Pick a color.")
	}
	if req.NewPassword != nil {
		checkPassword(v, "new_password", *req.NewPassword)
	}
	for k := range req.Notifications {
		if !slices.Contains(notify.Kinds, k) {
			v.Add("notifications", "Unknown notification: "+k+".")
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	if req.LastOrgID != nil {
		if _, err := authz.OrgMember(ctx, s.Store.Queries, *req.LastOrgID, id.UserID); err != nil {
			return err
		}
	}
	var newHash *string
	if req.NewPassword != nil {
		u, err := s.Store.GetUserByID(ctx, id.UserID)
		if err != nil {
			return err
		}
		if u.PasswordHash != nil {
			ok := false
			if req.CurrentPassword != nil {
				if ok, err = auth.VerifyPassword(*req.CurrentPassword, *u.PasswordHash); err != nil {
					return err
				}
			}
			if !ok {
				return httpx.FieldError(http.StatusUnprocessableEntity, "wrong_password", "current_password", "That password isn’t right.")
			}
		}
		h, err := auth.HashPassword(s.Argon2, *req.NewPassword)
		if err != nil {
			return err
		}
		newHash = &h
	}

	var me meView
	err := s.Store.Tx(ctx, func(q *db.Queries) error {
		if req.Name != nil || req.AvatarColor != nil {
			if _, err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{ID: id.UserID, Name: req.Name, AvatarColor: req.AvatarColor}); err != nil {
				return err
			}
		}
		if req.Notifications != nil {
			// Merge onto what's saved so a client can change one row at a time.
			u, err := q.GetUserByID(ctx, id.UserID)
			if err != nil {
				return err
			}
			prefs := notify.Parse(u.Notifications)
			maps.Copy(prefs, req.Notifications)
			raw, _ := json.Marshal(prefs)
			if err := q.SetUserNotifications(ctx, db.SetUserNotificationsParams{ID: id.UserID, Notifications: raw}); err != nil {
				return err
			}
		}
		if req.LastOrgID != nil {
			if err := q.SetUserLastOrg(ctx, db.SetUserLastOrgParams{ID: id.UserID, LastOrgID: req.LastOrgID}); err != nil {
				return err
			}
		}
		if newHash != nil {
			if err := q.SetUserPassword(ctx, db.SetUserPasswordParams{ID: id.UserID, PasswordHash: newHash}); err != nil {
				return err
			}
			if err := q.DeleteOtherUserSessions(ctx, db.DeleteOtherUserSessionsParams{UserID: id.UserID, ID: id.SessionID}); err != nil {
				return err
			}
		}
		var err error
		me, err = s.buildMe(ctx, q, id.UserID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, me)
	return nil
}
