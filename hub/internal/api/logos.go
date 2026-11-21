package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
)

const maxLogoBytes = 1 << 20

// logoTypes maps the detected content type to the stored file extension.
var logoTypes = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/svg+xml": ".svg",
}

var errBadLogo = httpx.FieldError(http.StatusUnsupportedMediaType, "unsupported_logo", "logo", "Use a PNG, JPEG, SVG or WebP image.")

// readLogo reads the request body and works out its type from the bytes, not the headers.
func readLogo(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLogoBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, "", httpx.FieldError(http.StatusRequestEntityTooLarge, "logo_too_large", "logo", "The logo must be 1 MB or smaller.")
		}
		return nil, "", err
	}
	mime := http.DetectContentType(data)
	if mime == "text/xml; charset=utf-8" || mime == "text/plain; charset=utf-8" {
		if isSafeSVG(data) {
			mime = "image/svg+xml"
		}
	}
	if _, ok := logoTypes[mime]; !ok {
		return nil, "", errBadLogo
	}
	return data, mime, nil
}

var svgHandler = regexp.MustCompile(`\won[a-z]+\s*=`)

func isSafeSVG(data []byte) bool {
	s := strings.ToLower(string(data))
	return strings.Contains(s, "<svg") && !strings.Contains(s, "<script") && !strings.Contains(s, "javascript:") && !svgHandler.MatchString(s)
}

func (s *Server) saveLogo(r *http.Request, kind string, id interface{ String() string }, data []byte, mime string) (string, error) {
	if s.Blobs == nil {
		return "", errors.New("file storage is not configured")
	}
	key := fmt.Sprintf("logos/%s/%s/%s%s", kind, id, newID(), logoTypes[mime])
	if err := s.Blobs.Put(r.Context(), key, bytes.NewReader(data), int64(len(data)), mime); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Server) serveLogo(w http.ResponseWriter, r *http.Request, key *string) error {
	if key == nil || s.Blobs == nil {
		return httpx.ErrNotFound
	}
	rc, size, err := s.Blobs.Get(r.Context(), *key)
	if errors.Is(err, blob.ErrNotFound) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	defer rc.Close()
	for mime, ext := range logoTypes {
		if ext == path.Ext(*key) {
			w.Header().Set("Content-Type", mime)
		}
	}
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = io.Copy(w, rc)
	return nil
}

func (s *Server) handlePutOrgLogo(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	if _, err := authz.OrgAdmin(r.Context(), s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	data, mime, err := readLogo(w, r)
	if err != nil {
		return err
	}
	key, err := s.saveLogo(r, "orgs", orgID, data, mime)
	if err != nil {
		return err
	}
	org, err := s.Store.SetOrganizationLogo(r.Context(), db.SetOrganizationLogoParams{ID: orgID, LogoKey: &key})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"organization": toOrgView(org)})
	return nil
}

func (s *Server) handleGetOrgLogo(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	if _, err := authz.OrgMember(r.Context(), s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	org, err := s.Store.GetOrganization(r.Context(), orgID)
	if err != nil {
		return err
	}
	return s.serveLogo(w, r, org.LogoKey)
}

func (s *Server) handleDeleteOrgLogo(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	if _, err := authz.OrgAdmin(r.Context(), s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	if _, err := s.Store.SetOrganizationLogo(r.Context(), db.SetOrganizationLogoParams{ID: orgID}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handlePutWorkspaceLogo(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	_, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	data, mime, err := readLogo(w, r)
	if err != nil {
		return err
	}
	key, err := s.saveLogo(r, "workspaces", wsID, data, mime)
	if err != nil {
		return err
	}
	ws, err := s.Store.SetWorkspaceLogo(r.Context(), db.SetWorkspaceLogoParams{ID: wsID, LogoKey: &key})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"workspace": core.ToWorkspace(ws)})
	return nil
}

func (s *Server) handleGetWorkspaceLogo(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ws, _, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	return s.serveLogo(w, r, ws.LogoKey)
}

// handlePublicWorkspaceLogo lets Discord load the avatar. It only answers for workspaces with Discord on.
func (s *Server) handlePublicWorkspaceLogo(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ws, err := s.Store.Queries.GetWorkspace(r.Context(), wsID)
	if err != nil || ws.DiscordWebhook == "" {
		return httpx.ErrNotFound
	}
	return s.serveLogo(w, r, ws.LogoKey)
}

func (s *Server) handleDeleteWorkspaceLogo(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	_, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	if _, err := s.Store.SetWorkspaceLogo(r.Context(), db.SetWorkspaceLogoParams{ID: wsID}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
