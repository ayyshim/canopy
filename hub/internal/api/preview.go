package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

const (
	previewTTL     = time.Hour
	previewMaxHTML = 20 << 20
)

// Agent pages run scripts on an opaque origin: no allow-same-origin, so they cannot reach hub cookies or the API.
const previewCSP = "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads; " +
	"default-src 'none'; script-src 'self' 'unsafe-inline' https:; style-src 'self' 'unsafe-inline' https:; " +
	"font-src 'self' https: data:; img-src 'self' https: data: blob:; media-src 'self' https: data: blob:; " +
	"connect-src https: data: blob:"

// handleCreatePreview issues a token that serves an output, and the outputs next to it, as a page.
func (s *Server) handleCreatePreview(w http.ResponseWriter, r *http.Request) error {
	_, o, err := s.loadOutput(r)
	if err != nil {
		return err
	}
	g := previewGrant{UserID: identity(r).UserID, WorkspaceID: o.WorkspaceID, OutputID: o.ID, TaskID: o.TaskID}
	if o.TaskID != nil {
		t, err := s.Store.GetTask(r.Context(), *o.TaskID)
		if err != nil {
			return err
		}
		g.GoalID = t.GoalID
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	g.ExpiresAt = s.Now().Add(previewTTL)
	if err := s.previews().Put(r.Context(), token, g, previewTTL); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":        "/api/v1/preview/" + token + "/" + escapePath(o.Name),
		"expires_at": g.ExpiresAt.UTC().Format(time.RFC3339),
	})
	return nil
}

func (s *Server) previews() PreviewStore {
	if s.Previews == nil {
		s.Previews = &MemoryPreviews{}
	}
	return s.Previews
}

func escapePath(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "%", "%25"), "?", "%3F")
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(parts[i], "#", "%23"), " ", "%20")
	}
	return strings.Join(parts, "/")
}

// handlePreviewFile serves one named output to a token holder; the token is the only credential.
func (s *Server) handlePreviewFile(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	g, ok, err := s.previews().Get(ctx, pathParam(r, "token"))
	if err != nil {
		return err
	}
	if !ok || !s.Now().Before(g.ExpiresAt) {
		return httpx.ErrNotFound
	}
	name := pathParam(r, "*")
	if name == "" || strings.Contains(name, "\\") || strings.Contains(name, "\x00") {
		return httpx.ErrNotFound
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." || seg == "" {
			return httpx.ErrNotFound
		}
	}
	// The person must still see the workspace.
	view, err := s.loadWSView(ctx, g.WorkspaceID, g.UserID)
	if err != nil {
		return httpx.ErrNotFound
	}
	o, err := s.findPreviewOutput(r, g, name)
	if err != nil {
		return err
	}
	if !view.seesMember(o.MemberID) {
		return httpx.ErrNotFound
	}
	v, err := s.Store.GetLatestOutputVersion(ctx, o.ID)
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

	h := w.Header()
	h.Set("Content-Type", v.Mime)
	h.Set("Content-Security-Policy", previewCSP)
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	// Pages on the opaque origin need CORS for fonts, modules and fetch; the token is not a cookie.
	h.Set("Access-Control-Allow-Origin", "*")

	if !strings.HasPrefix(strings.ToLower(v.Mime), "text/html") {
		h.Set("Content-Length", fmt.Sprint(size))
		_, _ = io.Copy(w, rc)
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(rc, previewMaxHTML))
	if err != nil {
		return err
	}
	out := injectPreviewScript(body)
	h.Set("Content-Length", fmt.Sprint(len(out)))
	_, _ = w.Write(out)
	return nil
}

// findPreviewOutput looks in the token's task first, then in the other tasks of its goal.
func (s *Server) findPreviewOutput(r *http.Request, g previewGrant, name string) (db.Output, error) {
	ctx := r.Context()
	if g.TaskID == nil {
		o, err := s.Store.GetOutput(ctx, g.OutputID)
		if err != nil || o.Name != name {
			return o, httpx.ErrNotFound
		}
		return o, nil
	}
	o, err := s.Store.GetTaskOutputByName(ctx, db.GetTaskOutputByNameParams{TaskID: g.TaskID, Name: name})
	if err == nil {
		return o, nil
	}
	if !store.IsNotFound(err) {
		return o, err
	}
	if g.GoalID == nil {
		return o, httpx.ErrNotFound
	}
	o, err = s.Store.GetGoalOutputByName(ctx, db.GetGoalOutputByNameParams{GoalID: g.GoalID, Name: name})
	if store.IsNotFound(err) {
		return o, httpx.ErrNotFound
	}
	return o, err
}

const previewMarker = "<!--canopy-preview-->"

const previewScript = previewMarker + `<script>(function(){
function mem(){var d={};return{getItem:function(k){return k in d?d[k]:null},setItem:function(k,v){d[k]=String(v)},removeItem:function(k){delete d[k]},clear:function(){d={}},key:function(i){return Object.keys(d)[i]||null},get length(){return Object.keys(d).length}}}
['localStorage','sessionStorage'].forEach(function(n){var ok=false;try{ok=!!window[n];window[n].getItem('x')}catch(e){ok=false}
if(!ok){try{Object.defineProperty(window,n,{value:mem(),configurable:true})}catch(e){}}});
var m=location.pathname.match(/\/preview\/[^\/]+\/(.*)$/);
function name(){try{return m?decodeURIComponent(m[1]):''}catch(e){return m?m[1]:''}}
window.addEventListener('load',function(){parent.postMessage({type:'canopy-preview',name:name()},'*')});
document.addEventListener('click',function(e){var a=e.target&&e.target.closest&&e.target.closest('a[href]');if(!a||e.defaultPrevented)return;
try{var u=new URL(a.href,location.href);if((u.protocol==='http:'||u.protocol==='https:')&&u.host!==location.host){a.target='_blank';a.rel='noopener noreferrer'}}catch(x){}},true);
})();</script>`

var (
	headOpen = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
	htmlOpen = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)
	doctype  = regexp.MustCompile(`(?i)^\s*<!doctype[^>]*>`)
)

// injectPreviewScript puts the helper script at the top of <head>, once.
func injectPreviewScript(b []byte) []byte {
	if bytes.Contains(b, []byte(previewMarker)) {
		return b
	}
	at, wrap := 0, true
	if loc := headOpen.FindIndex(b); loc != nil {
		at, wrap = loc[1], false
	} else if loc := htmlOpen.FindIndex(b); loc != nil {
		at = loc[1]
	} else if loc := doctype.FindIndex(b); loc != nil {
		at = loc[1]
	}
	ins := previewScript
	if wrap {
		ins = "<head>" + ins + "</head>"
		if at == 0 {
			ins = previewScript
		}
	}
	out := make([]byte, 0, len(b)+len(ins))
	out = append(out, b[:at]...)
	out = append(out, ins...)
	return append(out, b[at:]...)
}
