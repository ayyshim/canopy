package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/skills"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// The workspace skill library: markdown that admins add (pasted, or fetched from a GitHub link)
// and attach to members. Anyone in the workspace reads the library; admins change it.

const maxSkillName = 80

var errSkillName = httpx.NewError(http.StatusConflict, "skill_name_taken", "A skill with that name already exists.")

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, _, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, identity(r).UserID); err != nil {
		return err
	}
	list, err := s.Store.ListWorkspaceSkills(ctx, wsID)
	if err != nil {
		return err
	}
	links, err := s.Store.ListWorkspaceMemberSkills(ctx, wsID)
	if err != nil {
		return err
	}
	byMember := map[uuid.UUID][]uuid.UUID{}
	for _, l := range links {
		byMember[l.MemberID] = append(byMember[l.MemberID], l.SkillID)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"skills": core.ToSkills(list), "member_skills": byMember})
	return nil
}

type skillInput struct {
	Name       *string `json:"name"`
	SourceKind *string `json:"source_kind"` // md or github
	SourceURL  *string `json:"source_url"`
	Content    *string `json:"content"`
	Refresh    bool    `json:"refresh"` // github skills: fetch the link again
}

// skillFields is a skill's checked, ready-to-store fields.
type skillFields struct {
	name, kind, url, content *string
}

// resolveSkill validates input against the skill it changes (zero value when creating) and
// fetches GitHub content when needed.
func (s *Server) resolveSkill(r *http.Request, in skillInput, cur *db.WorkspaceSkill) (skillFields, error) {
	val := httpx.Validation{}
	var f skillFields
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		switch {
		case n == "":
			val.Add("name", "Give the skill a name.")
		case utf8.RuneCountInString(n) > maxSkillName:
			val.Add("name", "Keep the name under 80 characters.")
		}
		f.name = &n
	} else if cur == nil {
		val.Add("name", "Give the skill a name.")
	}

	kind := ""
	if cur != nil {
		kind = cur.SourceKind
	}
	if in.SourceKind != nil {
		kind = *in.SourceKind
	}
	if kind != "md" && kind != "github" {
		val.Add("source_kind", "Choose a markdown file or a GitHub link.")
		return f, val.Err()
	}
	f.kind = &kind

	switch kind {
	case "md":
		if in.Content == nil && (cur == nil || cur.SourceKind != "md") {
			val.Add("content", "Add the skill’s markdown.")
		}
		if in.Content != nil {
			c, err := skills.Check(*in.Content)
			if err != nil {
				val.Add("content", err.Error())
			}
			f.content = &c
		}
		empty := ""
		f.url = &empty
	case "github":
		link := ""
		if cur != nil {
			link = cur.SourceUrl
		}
		if in.SourceURL != nil {
			link = strings.TrimSpace(*in.SourceURL)
		}
		if link == "" {
			val.Add("source_url", "Paste a link to a markdown file on GitHub.")
			break
		}
		changed := cur == nil || cur.SourceKind != "github" || link != cur.SourceUrl
		if changed || in.Refresh {
			body, err := s.skillFetcher().Fetch(r.Context(), link)
			var se skills.Error
			if errors.As(err, &se) {
				val.Add("source_url", se.Error())
			} else if err != nil {
				return f, err
			}
			f.content = &body
		}
		f.url = &link
	}
	return f, val.Err()
}

func (s *Server) skillFetcher() *skills.Fetcher {
	if s.Skills == nil {
		return &skills.Fetcher{}
	}
	return s.Skills
}

func (s *Server) handleCreateSkill(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	ws, v, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	var in skillInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	f, err := s.resolveSkill(r, in, nil)
	if err != nil {
		return err
	}
	var sk db.WorkspaceSkill
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		sk, err = t.Q.CreateWorkspaceSkill(ctx, db.CreateWorkspaceSkillParams{ID: newID(), WorkspaceID: wsID, Name: *f.name,
			SourceKind: *f.kind, SourceUrl: *f.url, Content: deref(f.content)})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "skill.saved"), core.ToSkill(sk))
	})
	if store.IsUniqueViolation(err, "") {
		return errSkillName
	} else if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"skill": core.ToSkill(sk)})
	return nil
}

// loadSkill fetches a skill the caller can see; admin says whether they administer its workspace.
func (s *Server) loadSkill(r *http.Request, needAdmin bool) (db.Workspace, db.WorkspaceSkill, error) {
	id, err := pathID(r, "skillID")
	if err != nil {
		return db.Workspace{}, db.WorkspaceSkill{}, err
	}
	sk, err := s.Store.GetWorkspaceSkill(r.Context(), id)
	if store.IsNotFound(err) {
		return db.Workspace{}, sk, httpx.ErrNotFound
	} else if err != nil {
		return db.Workspace{}, sk, err
	}
	ws, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, sk.WorkspaceID, identity(r).UserID)
	if err != nil {
		return ws, sk, err
	}
	if needAdmin {
		err = v.RequireAdmin()
	}
	return ws, sk, err
}

func (s *Server) handlePatchSkill(w http.ResponseWriter, r *http.Request) error {
	ws, sk, err := s.loadSkill(r, true)
	if err != nil {
		return err
	}
	var in skillInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	f, err := s.resolveSkill(r, in, &sk)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		sk, err = t.Q.UpdateWorkspaceSkill(ctx, db.UpdateWorkspaceSkillParams{ID: sk.ID, Name: f.name, SourceKind: f.kind,
			SourceUrl: f.url, Content: f.content})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "skill.saved"), core.ToSkill(sk))
	})
	if store.IsUniqueViolation(err, "") {
		return errSkillName
	} else if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"skill": core.ToSkill(sk)})
	return nil
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) error {
	ws, sk, err := s.loadSkill(r, true)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteWorkspaceSkill(ctx, sk.ID); err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "skill.deleted"), map[string]any{"id": sk.ID})
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- skills on a member ----

func (s *Server) handleListMemberSkills(w http.ResponseWriter, r *http.Request) error {
	_, _, m, err := s.loadMember(r)
	if err != nil {
		return err
	}
	list, err := s.Store.ListMemberSkills(r.Context(), m.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"skills": core.ToSkills(list)})
	return nil
}

// memberSkillTarget loads the member and skill of an attach or detach call (admins only).
func (s *Server) memberSkillTarget(r *http.Request) (db.Workspace, db.Member, db.WorkspaceSkill, error) {
	ws, v, m, err := s.loadMember(r)
	if err != nil {
		return ws, m, db.WorkspaceSkill{}, err
	}
	if err := v.RequireAdmin(); err != nil {
		return ws, m, db.WorkspaceSkill{}, err
	}
	id, err := pathID(r, "skillID")
	if err != nil {
		return ws, m, db.WorkspaceSkill{}, err
	}
	sk, err := s.Store.GetWorkspaceSkill(r.Context(), id)
	if store.IsNotFound(err) || (err == nil && sk.WorkspaceID != ws.ID) {
		return ws, m, sk, httpx.ErrNotFound
	}
	return ws, m, sk, err
}

func (s *Server) handleAttachSkill(w http.ResponseWriter, r *http.Request) error {
	ws, m, sk, err := s.memberSkillTarget(r)
	if err != nil {
		return err
	}
	return s.changeMemberSkill(w, r, ws, m, sk, true)
}

func (s *Server) handleDetachSkill(w http.ResponseWriter, r *http.Request) error {
	ws, m, sk, err := s.memberSkillTarget(r)
	if err != nil {
		return err
	}
	return s.changeMemberSkill(w, r, ws, m, sk, false)
}

func (s *Server) changeMemberSkill(w http.ResponseWriter, r *http.Request, ws db.Workspace, m db.Member, sk db.WorkspaceSkill, attach bool) error {
	ctx := r.Context()
	var list []db.WorkspaceSkill
	err := s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		if attach {
			err = t.Q.AttachMemberSkill(ctx, db.AttachMemberSkillParams{MemberID: m.ID, SkillID: sk.ID})
		} else {
			err = t.Q.DetachMemberSkill(ctx, db.DetachMemberSkillParams{MemberID: m.ID, SkillID: sk.ID})
		}
		if err != nil {
			return err
		}
		if list, err = t.Q.ListMemberSkills(ctx, m.ID); err != nil {
			return err
		}
		return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, m.ID, "member.skills"), map[string]any{"member_id": m.ID, "skills": core.ToSkills(list)})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"skills": core.ToSkills(list)})
	return nil
}
