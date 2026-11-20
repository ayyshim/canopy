package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// loadGoal loads a goal the caller can see.
func (s *Server) loadGoal(r *http.Request) (*wsView, db.Goal, error) {
	goalID, err := pathID(r, "goalID")
	if err != nil {
		return nil, db.Goal{}, err
	}
	g, err := s.Store.GetGoal(r.Context(), goalID)
	if store.IsNotFound(err) {
		return nil, g, httpx.ErrNotFound
	} else if err != nil {
		return nil, g, err
	}
	view, err := s.loadWSView(r.Context(), g.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, g, err
	}
	if !view.seesMember(g.ManagerID) {
		return nil, g, httpx.ErrNotFound
	}
	return view, g, nil
}

// storeUpload saves the request body by its hash. The name comes from ?name=.
func (s *Server) storeUpload(w http.ResponseWriter, r *http.Request) (db.CreateAttachmentParams, error) {
	var p db.CreateAttachmentParams
	if s.Blobs == nil {
		return p, httpx.NewError(http.StatusServiceUnavailable, "no_storage", "File storage isn’t set up on this hub.")
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, core.MaxAttachmentBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return p, httpx.FieldError(http.StatusRequestEntityTooLarge, "file_too_large", "file", "Files must be 25 MB or smaller.")
		}
		return p, err
	}
	if len(data) == 0 {
		return p, httpx.Validation{"file": "That file is empty."}.Err()
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := s.Blobs.Put(r.Context(), blob.ContentKey(sha), bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		return p, err
	}
	name := core.AttachmentName(r.URL.Query().Get("name"))
	typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if typ == "" {
		typ = http.DetectContentType(data)
	}
	uid := identity(r).UserID
	return db.CreateAttachmentParams{Name: name, Mime: typ, Size: int64(len(data)), Sha256: sha, UserID: &uid}, nil
}

func (s *Server) handleAttachToGoal(w http.ResponseWriter, r *http.Request) error {
	view, g, err := s.loadGoal(r)
	if err != nil {
		return err
	}
	p, err := s.storeUpload(w, r)
	if err != nil {
		return err
	}
	p.GoalID = &g.ID
	return s.saveAttachment(w, r, view, g.ManagerID, p)
}

func (s *Server) handleAttachToTask(w http.ResponseWriter, r *http.Request) error {
	view, t, err := s.loadTask(r)
	if err != nil {
		return err
	}
	p, err := s.storeUpload(w, r)
	if err != nil {
		return err
	}
	p.TaskID = &t.ID
	return s.saveAttachment(w, r, view, t.MemberID, p)
}

func (s *Server) saveAttachment(w http.ResponseWriter, r *http.Request, view *wsView, seenVia *uuid.UUID, p db.CreateAttachmentParams) error {
	ctx := r.Context()
	var a db.Attachment
	err := s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		a, err = core.AddAttachment(ctx, t, view.ws, seenVia, p)
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"attachment": core.ToAttachment(a)})
	return nil
}

// loadAttachment loads an attachment the caller can see, and who sees it (for events).
func (s *Server) loadAttachment(r *http.Request) (*wsView, db.Attachment, *uuid.UUID, error) {
	id, err := pathID(r, "attachmentID")
	if err != nil {
		return nil, db.Attachment{}, nil, err
	}
	ctx := r.Context()
	a, err := s.Store.GetAttachment(ctx, id)
	if store.IsNotFound(err) {
		return nil, a, nil, httpx.ErrNotFound
	} else if err != nil {
		return nil, a, nil, err
	}
	view, err := s.loadWSView(ctx, a.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, a, nil, err
	}
	var seenVia *uuid.UUID
	if a.GoalID != nil {
		g, err := s.Store.GetGoal(ctx, *a.GoalID)
		if err != nil {
			return nil, a, nil, httpx.ErrNotFound
		}
		seenVia = g.ManagerID
	} else if a.TaskID != nil {
		t, err := s.Store.GetTask(ctx, *a.TaskID)
		if err != nil {
			return nil, a, nil, httpx.ErrNotFound
		}
		seenVia = t.MemberID
	}
	if !view.seesMember(seenVia) {
		return nil, a, nil, httpx.ErrNotFound
	}
	return view, a, seenVia, nil
}

// handleAttachmentFile streams an attachment (inline for previews, ?download=1 to save).
func (s *Server) handleAttachmentFile(w http.ResponseWriter, r *http.Request) error {
	_, a, _, err := s.loadAttachment(r)
	if err != nil {
		return err
	}
	if s.Blobs == nil {
		return httpx.ErrNotFound
	}
	rc, size, err := s.Blobs.Get(r.Context(), blob.ContentKey(a.Sha256))
	if errors.Is(err, blob.ErrNotFound) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	defer rc.Close()
	w.Header().Set("Content-Type", a.Mime)
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Attached files are shown, never run, in the hub's origin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	disp := "inline"
	if r.URL.Query().Get("download") != "" {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disp, a.Name))
	_, _ = io.Copy(w, rc)
	return nil
}

// handleDeleteAttachment: the person who attached it, or an admin.
func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) error {
	view, a, seenVia, err := s.loadAttachment(r)
	if err != nil {
		return err
	}
	if (a.UserID == nil || *a.UserID != view.v.UserID) && !view.v.Admin() {
		return httpx.ErrForbidden
	}
	ctx := r.Context()
	if err := s.Core.Tx(ctx, func(t *core.Tx) error { return core.RemoveAttachment(ctx, t, view.ws, seenVia, a) }); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
