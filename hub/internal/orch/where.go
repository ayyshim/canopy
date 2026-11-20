package orch

import (
	"fmt"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// Where work runs (DECISIONS): on the computer of the person the work is for, with that
// computer's own CLI sign-in. A member has one session per goal it works on, plus one per
// person for chat and for work outside a goal.

// sessKey names one session: a member working on a goal, or a member working for a person.
type sessKey struct {
	member uuid.UUID
	goal   uuid.UUID // set for goal work
	person uuid.UUID // set for chat, owner-approved changes and work outside a goal
}

func goalKey(member, goal uuid.UUID) sessKey     { return sessKey{member: member, goal: goal} }
func personKey(member, person uuid.UUID) sessKey { return sessKey{member: member, person: person} }

// taskKey is the session a task runs in. Owner-approved changes run for the owner, on the
// owner's computer, so they never share the requester's goal session.
func taskKey(t db.Task) sessKey {
	var member uuid.UUID
	if t.MemberID != nil {
		member = *t.MemberID
	}
	if t.GoalID != nil && t.Kind != "owner_change" {
		return goalKey(member, *t.GoalID)
	}
	return personKey(member, deref2(firstID(t.AttributedUserID, t.CreatedBy)))
}

func keyOfSession(s db.Session) sessKey {
	return sessKey{member: s.MemberID, goal: deref2(s.GoalID), person: deref2(s.PersonID)}
}

func (k sessKey) goalID() *uuid.UUID   { return optID(k.goal) }
func (k sessKey) personID() *uuid.UUID { return optID(k.person) }

// work is the session's folder name on its computer. It stays the same for the session's
// whole life, so the CLI can resume its conversation there.
func (k sessKey) work() string {
	if k.goal != uuid.Nil {
		return fmt.Sprintf("goal-%s/%s", k.goal, short(k.member))
	}
	return fmt.Sprintf("person-%s/%s", k.person, short(k.member))
}

// short is the random end of an id: ids are time-ordered, so their starts repeat.
func short(id uuid.UUID) string {
	s := id.String()
	return s[len(s)-8:]
}

func optID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func firstID(ids ...*uuid.UUID) *uuid.UUID {
	for _, id := range ids {
		if id != nil {
			return id
		}
	}
	return nil
}

// computerFor is the computer a person's work runs on: their own, else a shared one.
func (st *wsState) computerFor(person uuid.UUID) *uuid.UUID {
	if id, ok := st.defaults[person]; ok {
		if r, ok := st.runners[id]; ok && r.RevokedAt == nil {
			return &id
		}
	}
	for _, r := range st.runners {
		if r.OwnerUserID == person && !r.Shared {
			id := r.ID
			return &id
		}
	}
	if len(st.shared) > 0 {
		id := st.shared[0]
		return &id
	}
	return nil
}

// runnerFor is the computer a session runs on.
func (st *wsState) runnerFor(k sessKey, task *db.Task) *uuid.UUID {
	if task != nil && task.Kind == "owner_change" {
		for _, need := range taskDirs(*task) {
			if d, ok := st.dir[need.DirectoryID]; ok && d.MachineID != nil {
				return d.MachineID
			}
		}
	}
	if g, ok := st.goal[k.goal]; ok {
		if g.RunnerID != nil {
			return g.RunnerID
		}
		if g.CreatedBy != nil {
			return st.computerFor(*g.CreatedBy)
		}
		return nil
	}
	if k.person != uuid.Nil {
		return st.computerFor(k.person)
	}
	return nil
}

// whyWaiting says why work for a session can't start now ("" when it can).
func (st *wsState) whyWaiting(runner *uuid.UUID, cli string, now func() bool) string {
	if runner == nil {
		return "No computer to run on"
	}
	r, ok := st.runners[*runner]
	if !ok || r.RevokedAt != nil {
		return "No computer to run on"
	}
	if !st.online(*runner) {
		return "Waiting for " + r.Name
	}
	switch st.accountBlocked(*runner, cli, now) {
	case "cap":
		return "Paused at the daily cap on " + r.Name
	case "limit":
		return "Usage limit on " + r.Name
	}
	return ""
}

// accountBlocked reports why a computer's sign-in can't run work now, if it can't.
func (st *wsState) accountBlocked(runner uuid.UUID, cli string, now func() bool) string {
	a, ok := st.accounts[acctKey{runner, cli}]
	if !ok {
		return ""
	}
	if a.Paused {
		return "cap"
	}
	if a.LimitedUntil != nil && now() && (a.UseKeyUntil == nil || a.UseKeyUntil.Before(*a.LimitedUntil)) {
		return "limit"
	}
	return ""
}
