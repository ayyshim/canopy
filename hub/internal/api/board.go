package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
)

// boardSnapshot is everything the board draws, filtered to what the viewer may see
// (SPEC "Board WebSocket": a snapshot filtered to the viewer's access, then deltas).
type boardSnapshot struct {
	Seq          int64                `json:"seq"`
	Workspace    core.WorkspaceView   `json:"workspace"`
	Viewer       viewerView           `json:"viewer"`
	Members      []any                `json:"members"`
	Grants       []core.GrantView     `json:"grants"`
	Directories  []directoryDetail    `json:"directories"`
	Goals        []core.GoalView      `json:"goals"`
	GoalEvents   []core.GoalEventView `json:"goal_events"`
	Tasks        []core.TaskView      `json:"tasks"`
	Signals      []core.SignalView    `json:"signals"`
	Sessions     []core.SessionView   `json:"sessions"`
	Outputs      []core.OutputView    `json:"outputs"`
	Seen         map[string]int32     `json:"seen"`      // output id → newest version this viewer opened
	Unread       map[string]int64     `json:"unread"`    // member id → unread chat messages
	Accounts     []core.AccountView   `json:"accounts"`  // sign-ins of the viewer's computers and shared ones (admins: all)
	Runners      []core.RunnerView    `json:"runners"`   // the organization's computers
	Computers    map[string]any       `json:"computers"` // person id → the computer their goals run on
	Clones       []core.CloneView     `json:"clones"`
	GoalRepos    []core.GoalRepoView  `json:"goal_repos"`
	Notes        []core.NoteView      `json:"notes"`
	Skills       []core.SkillView     `json:"skills"`        // the workspace skill library
	MemberSkills map[string][]string  `json:"member_skills"` // member id → ids of library skills they have
	Mirrors      map[string]any       `json:"mirrors"`       // directory id → sync state
	People       []map[string]any     `json:"people"`        // names for avatars, cursors and "given by"
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	snap, err := s.BoardSnapshot(r.Context(), wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, snap)
	return nil
}

// BoardSnapshot builds the board for one viewer.
func (s *Server) BoardSnapshot(ctx context.Context, wsID, userID uuid.UUID) (*boardSnapshot, error) {
	q := s.Store.Queries
	view, err := s.loadWSView(ctx, wsID, userID)
	if err != nil {
		return nil, err
	}
	seq, err := q.MaxWorkspaceSeq(ctx, db.MaxWorkspaceSeqParams{WorkspaceID: &wsID, OrgID: view.ws.OrgID})
	if err != nil {
		return nil, err
	}
	detail, err := s.workspaceDetail(ctx, view.ws, view.v)
	if err != nil {
		return nil, err
	}
	snap := &boardSnapshot{Seq: seq, Workspace: detail.Workspace, Viewer: detail.Viewer, Members: detail.Members, Grants: detail.Grants,
		Directories: detail.Directories, Clones: detail.Clones, Goals: []core.GoalView{}, GoalEvents: []core.GoalEventView{},
		Tasks: []core.TaskView{}, Signals: []core.SignalView{}, Sessions: []core.SessionView{}, Outputs: []core.OutputView{},
		Seen: map[string]int32{}, Unread: map[string]int64{}, Accounts: []core.AccountView{}, Mirrors: map[string]any{}, People: []map[string]any{},
		Runners: []core.RunnerView{}, Computers: map[string]any{}, GoalRepos: []core.GoalRepoView{}, Notes: []core.NoteView{}}
	runners, err := q.ListOrgRunners(ctx, view.ws.OrgID)
	if err != nil {
		return nil, err
	}
	mine := map[uuid.UUID]bool{}
	for _, rn := range runners {
		snap.Runners = append(snap.Runners, s.liveRunnerView(rn))
		mine[rn.ID] = rn.OwnerUserID == userID || rn.Shared
	}
	defaults, err := q.ListDefaultRunners(ctx, view.ws.OrgID)
	if err != nil {
		return nil, err
	}
	for _, d := range defaults {
		snap.Computers[d.UserID.String()] = d.DefaultRunnerID
	}
	goals, err := q.ListGoals(ctx, wsID)
	if err != nil {
		return nil, err
	}
	visibleGoals := map[uuid.UUID]bool{}
	for _, g := range goals {
		if view.seesMember(g.ManagerID) {
			visibleGoals[g.ID] = true
			snap.Goals = append(snap.Goals, core.ToGoal(g))
		}
	}
	repos, err := q.ListWorkspaceGoalRepos(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, r := range repos {
		if visibleGoals[r.GoalID] {
			snap.GoalRepos = append(snap.GoalRepos, core.ToGoalRepo(r))
		}
	}
	notes, err := q.ListNotes(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		if n.GoalID == nil || visibleGoals[*n.GoalID] {
			snap.Notes = append(snap.Notes, core.ToNote(n))
		}
	}
	skillList, err := q.ListWorkspaceSkills(ctx, wsID)
	if err != nil {
		return nil, err
	}
	snap.Skills = core.ToSkills(skillList)
	snap.MemberSkills = map[string][]string{}
	links, err := q.ListWorkspaceMemberSkills(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if view.seesMember(&l.MemberID) {
			snap.MemberSkills[l.MemberID.String()] = append(snap.MemberSkills[l.MemberID.String()], l.SkillID.String())
		}
	}
	events, err := q.ListWorkspaceGoalEvents(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if visibleGoals[e.GoalID] {
			snap.GoalEvents = append(snap.GoalEvents, core.ToGoalEvent(e))
		}
	}
	tasks, err := q.ListTasks(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if view.seesMember(t.MemberID) {
			snap.Tasks = append(snap.Tasks, core.ToTask(t))
		}
	}
	signals, err := q.ListOpenSignals(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, sig := range signals {
		if view.seesSignal(sig) {
			snap.Signals = append(snap.Signals, core.ToSignal(sig))
		}
	}
	sessions, err := q.ListLatestSessions(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, x := range sessions {
		if view.seesMember(&x.MemberID) {
			snap.Sessions = append(snap.Sessions, core.ToSession(x))
		}
	}
	outs, err := q.ListOutputs(ctx, wsID)
	if err != nil {
		return nil, err
	}
	versions, err := q.ListWorkspaceOutputVersions(ctx, wsID)
	if err != nil {
		return nil, err
	}
	byOut := map[uuid.UUID][]db.OutputVersion{}
	for _, v := range versions {
		byOut[v.OutputID] = append(byOut[v.OutputID], v)
	}
	for _, o := range outs {
		if view.seesMember(o.MemberID) {
			snap.Outputs = append(snap.Outputs, core.ToOutput(o, byOut[o.ID]))
		}
	}
	seen, err := q.ListOutputViews(ctx, db.ListOutputViewsParams{WorkspaceID: wsID, UserID: userID})
	if err != nil {
		return nil, err
	}
	for _, sv := range seen {
		snap.Seen[sv.OutputID.String()] = sv.Version
	}
	unread, err := q.UnreadCounts(ctx, db.UnreadCountsParams{WorkspaceID: wsID, UserID: userID})
	if err != nil {
		return nil, err
	}
	for _, u := range unread {
		if view.seesMember(&u.MemberID) {
			snap.Unread[u.MemberID.String()] = u.Unread
		}
	}
	accounts, err := s.connectionsView(ctx, view.ws.OrgID)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if view.v.Admin() || mine[a.RunnerID] {
			snap.Accounts = append(snap.Accounts, a)
		}
	}
	for _, d := range detail.Directories {
		if st, err := q.GetMirrorState(ctx, d.ID); err == nil {
			n, _ := q.CountQueuedWrites(ctx, d.ID)
			snap.Mirrors[d.ID.String()] = map[string]any{"revision": st.Revision, "complete": st.Complete, "missing": st.Missing,
				"last_synced_at": st.LastSyncedAt, "queued_writes": n}
		}
	}
	people, err := q.ListOrgPeople(ctx, view.ws.OrgID)
	if err != nil {
		return nil, err
	}
	for _, p := range people {
		snap.People = append(snap.People, map[string]any{"id": p.ID, "name": p.Name, "avatar_color": p.AvatarColor})
	}
	return snap, nil
}

var _ = json.Marshal
