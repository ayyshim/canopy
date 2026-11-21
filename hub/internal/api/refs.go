package api

import (
	"net/http"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/refs"
)

const pickerLimit = 20

var refTypes = map[refs.Kind]string{refs.Goal: "goal", refs.Task: "task", refs.Note: "note"}

type refItemView struct {
	Ref    string `json:"ref"`
	Type   string `json:"type"`
	ID     string `json:"id"`
	Number int32  `json:"number"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// handleSearchRefs feeds the # picker: visible items matching a number or title words.
func (s *Server) handleSearchRefs(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	ws, _, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	sc, err := refs.ViewerScope(ctx, s.Store.Queries, ws, identity(r).UserID)
	if err != nil {
		return err
	}
	items, err := refs.Resolver{Q: s.Store.Queries}.Search(ctx, wsID, sc, r.URL.Query().Get("q"), pickerLimit)
	if err != nil {
		return err
	}
	out := make([]refItemView, 0, len(items))
	for _, it := range items {
		out = append(out, refItemView{Ref: it.Ref.String(), Type: refTypes[it.Ref.Kind], ID: it.ID.String(), Number: it.Ref.Number,
			Title: it.Title, Status: it.Status()})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
	return nil
}

// handleGetRef turns T-34 into the item it names; hidden and unknown refs are both 404.
func (s *Server) handleGetRef(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ref, ok := refs.ParseOne(pathParam(r, "ref"))
	if !ok {
		return httpx.ErrNotFound
	}
	ctx := r.Context()
	ws, _, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	sc, err := refs.ViewerScope(ctx, s.Store.Queries, ws, identity(r).UserID)
	if err != nil {
		return err
	}
	it, ok, err := refs.Resolver{Q: s.Store.Queries}.One(ctx, wsID, sc, ref)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrNotFound
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"type": refTypes[ref.Kind], "id": it.ID})
	return nil
}
