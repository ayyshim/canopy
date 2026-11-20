package orch

import (
	"context"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// wsState is everything a scheduling pass needs about one workspace.
type wsState struct {
	ws        db.Workspace
	members   []db.Member
	member    map[uuid.UUID]db.Member
	tasks     []db.Task
	task      map[uuid.UUID]db.Task
	goals     []db.Goal
	goal      map[uuid.UUID]db.Goal
	session   map[sessKey]db.Session // newest session per member and goal (or person)
	grants    map[uuid.UUID][]db.DirectoryGrant
	dirs      []db.Directory
	dir       map[uuid.UUID]db.Directory
	dirAccess map[uuid.UUID]map[uuid.UUID]string
	clones    []db.DirectoryClone
	goalRepos map[uuid.UUID][]db.GoalRepo
	runners   map[uuid.UUID]db.Runner
	defaults  map[uuid.UUID]uuid.UUID // person → their computer for goals
	shared    []uuid.UUID             // shared computers, oldest first
	accounts  map[acctKey]db.Account
	conns     map[acctKey]db.CliConnection
	signals   []db.Signal
	online    func(uuid.UUID) bool

	mirrorCache map[uuid.UUID]map[uuid.UUID]string
}

// acctKey is one CLI sign-in on one computer.
type acctKey struct {
	runner uuid.UUID
	cli    string
}

func (o *Orch) load(ctx context.Context, q *db.Queries, wsID uuid.UUID) (*wsState, error) {
	ws, err := q.GetWorkspace(ctx, wsID)
	if err != nil {
		return nil, err
	}
	st := &wsState{ws: ws, member: map[uuid.UUID]db.Member{}, task: map[uuid.UUID]db.Task{}, goal: map[uuid.UUID]db.Goal{},
		session: map[sessKey]db.Session{}, grants: map[uuid.UUID][]db.DirectoryGrant{}, dir: map[uuid.UUID]db.Directory{},
		dirAccess: map[uuid.UUID]map[uuid.UUID]string{}, goalRepos: map[uuid.UUID][]db.GoalRepo{}, runners: map[uuid.UUID]db.Runner{},
		defaults: map[uuid.UUID]uuid.UUID{}, accounts: map[acctKey]db.Account{}, conns: map[acctKey]db.CliConnection{}, online: o.R.Online}

	if st.members, err = q.ListMembers(ctx, wsID); err != nil {
		return nil, err
	}
	for _, m := range st.members {
		st.member[m.ID] = m
	}
	if st.tasks, err = q.ListTasks(ctx, wsID); err != nil {
		return nil, err
	}
	for _, t := range st.tasks {
		st.task[t.ID] = t
	}
	if st.goals, err = q.ListGoals(ctx, wsID); err != nil {
		return nil, err
	}
	for _, g := range st.goals {
		st.goal[g.ID] = g
	}
	sessions, err := q.ListLatestSessions(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		st.session[keyOfSession(s)] = s
	}
	grants, err := q.ListGrants(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, g := range grants {
		st.grants[g.MemberID] = append(st.grants[g.MemberID], g)
	}
	if st.dirs, err = q.ListDirectories(ctx, wsID); err != nil {
		return nil, err
	}
	for _, d := range st.dirs {
		st.dir[d.ID] = d
	}
	access, err := q.ListDirectoryAccess(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, a := range access {
		if st.dirAccess[a.DirectoryID] == nil {
			st.dirAccess[a.DirectoryID] = map[uuid.UUID]string{}
		}
		st.dirAccess[a.DirectoryID][a.UserID] = a.Mode
	}
	if st.clones, err = q.ListWorkspaceClones(ctx, wsID); err != nil {
		return nil, err
	}
	repos, err := q.ListWorkspaceGoalRepos(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, r := range repos {
		st.goalRepos[r.GoalID] = append(st.goalRepos[r.GoalID], r)
	}
	runners, err := q.ListOrgRunners(ctx, ws.OrgID)
	if err != nil {
		return nil, err
	}
	for _, r := range runners {
		st.runners[r.ID] = r
		if r.Shared {
			st.shared = append(st.shared, r.ID)
		}
	}
	defaults, err := q.ListDefaultRunners(ctx, ws.OrgID)
	if err != nil {
		return nil, err
	}
	for _, d := range defaults {
		if d.DefaultRunnerID != nil {
			st.defaults[d.UserID] = *d.DefaultRunnerID
		}
	}
	accounts, err := q.ListAccounts(ctx, ws.OrgID)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		st.accounts[acctKey{a.RunnerID, a.Cli}] = a
	}
	conns, err := q.ListOrgConnections(ctx, ws.OrgID)
	if err != nil {
		return nil, err
	}
	for _, c := range conns {
		st.conns[acctKey{c.RunnerID, c.Cli}] = c
	}
	if st.signals, err = q.ListOpenSignals(ctx, wsID); err != nil {
		return nil, err
	}
	return st, nil
}

func (st *wsState) grantMode(memberID, dirID uuid.UUID) string {
	for _, g := range st.grants[memberID] {
		if g.DirectoryID == dirID {
			return g.Mode
		}
	}
	return ""
}

// busy: the member already has a task going for this goal (or person).
func (st *wsState) busy(k sessKey) bool {
	for _, t := range st.tasks {
		if t.MemberID != nil && (t.Status == "doing" || t.Status == "review") && taskKey(t) == k {
			return true
		}
	}
	return false
}

// busyAnywhere: the member has a task going for anyone.
func (st *wsState) busyAnywhere(memberID uuid.UUID) bool {
	for _, t := range st.tasks {
		if t.MemberID != nil && *t.MemberID == memberID && (t.Status == "doing" || t.Status == "review") {
			return true
		}
	}
	return false
}

func (st *wsState) openSignal(match func(db.Signal) bool) *db.Signal {
	for i := range st.signals {
		if match(st.signals[i]) {
			return &st.signals[i]
		}
	}
	return nil
}

func (st *wsState) putTask(t db.Task) {
	for i := range st.tasks {
		if st.tasks[i].ID == t.ID {
			st.tasks[i] = t
		}
	}
	st.task[t.ID] = t
}

func (st *wsState) putGoal(g db.Goal) {
	for i := range st.goals {
		if st.goals[i].ID == g.ID {
			st.goals[i] = g
		}
	}
	st.goal[g.ID] = g
}
