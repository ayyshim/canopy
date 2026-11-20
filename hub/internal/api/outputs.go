package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

func (s *Server) loadOutput(r *http.Request) (*wsView, db.Output, error) {
	id, err := pathID(r, "outputID")
	if err != nil {
		return nil, db.Output{}, err
	}
	o, err := s.Store.GetOutput(r.Context(), id)
	if store.IsNotFound(err) {
		return nil, o, httpx.ErrNotFound
	} else if err != nil {
		return nil, o, err
	}
	view, err := s.loadWSView(r.Context(), o.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, o, err
	}
	if !view.seesMember(o.MemberID) {
		return nil, o, httpx.ErrNotFound
	}
	return view, o, nil
}

func (s *Server) handleGetOutput(w http.ResponseWriter, r *http.Request) error {
	_, o, err := s.loadOutput(r)
	if err != nil {
		return err
	}
	versions, err := s.Store.ListOutputVersions(r.Context(), o.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"output": core.ToOutput(o, versions)})
	return nil
}

// handleOutputFile streams one version of an output (inline for previews, ?download=1 to save).
func (s *Server) handleOutputFile(w http.ResponseWriter, r *http.Request) error {
	_, o, err := s.loadOutput(r)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(pathParam(r, "n"))
	if err != nil {
		return httpx.ErrNotFound
	}
	ctx := r.Context()
	v, err := s.Store.GetOutputVersion(ctx, db.GetOutputVersionParams{OutputID: o.ID, Number: int32(n)})
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	if s.Blobs == nil {
		return httpx.ErrNotFound
	}
	rc, size, err := s.Blobs.Get(ctx, v.BlobKey)
	if errors.Is(err, blob.ErrNotFound) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	defer rc.Close()
	w.Header().Set("Content-Type", v.Mime)
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Files agents made are shown, never run, in the hub's origin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	disp := "inline"
	if r.URL.Query().Get("download") != "" {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disp, o.Name))
	_, _ = io.Copy(w, rc)
	return nil
}

// handleOutputSeen clears the "unseen" dot for the viewer.
func (s *Server) handleOutputSeen(w http.ResponseWriter, r *http.Request) error {
	_, o, err := s.loadOutput(r)
	if err != nil {
		return err
	}
	versions, err := s.Store.ListOutputVersions(r.Context(), o.ID)
	if err != nil {
		return err
	}
	if len(versions) > 0 {
		if err := s.Store.MarkOutputSeen(r.Context(), db.MarkOutputSeenParams{UserID: identity(r).UserID, OutputID: o.ID, Version: versions[len(versions)-1].Number}); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
