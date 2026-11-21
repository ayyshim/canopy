package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/jobs"
	"canopy/hub/internal/jobs/schedule"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Jobs: scheduled single run and continuous work (docs/design/job-scheduler.md).

type jobInput struct {
	Title             *string         `json:"title"`
	Kind              *string         `json:"kind"`
	Brief             *string         `json:"brief"`
	ManagerID         *uuid.UUID      `json:"manager_id"`
	AssigneeMemberID  json.RawMessage `json:"assignee_member_id"` // null = the manager does the work
	RunnerID          json.RawMessage `json:"runner_id"`          // null = the owner's own computer
	OwnerUserID       *uuid.UUID      `json:"owner_user_id"`      // admins only
	Schedule          *string         `json:"schedule"`           // a cron expression or plain text such as "every weekday at 9"
	Timezone          *string         `json:"timezone"`
	MissedPolicy      *string         `json:"missed_policy"`
	CatchupWindowSecs *int32          `json:"catchup_window_secs"`
	RunTimeoutSecs    *int32          `json:"run_timeout_secs"`
	Caps              *struct {
		PerRun   *int64 `json:"per_run"`
		PerMonth *int64 `json:"per_month"`
	} `json:"token_caps"`
	Notify       *string         `json:"notify"`
	Ends         json.RawMessage `json:"ends"` // {"at": time|null, "max_runs": n|null}
	RequiredDirs *[]core.DirNeed `json:"required_dirs"`
}

type jobEnds struct {
	At      *time.Time `json:"at"`
	MaxRuns *int32     `json:"max_runs"`
}

// optUUID reads a field that may be absent (set=false), null, or a uuid.
func optUUID(raw json.RawMessage) (id *uuid.UUID, set, ok bool) {
	if len(raw) == 0 {
		return nil, false, true
	}
	if string(raw) == "null" {
		return nil, true, true
	}
	var u uuid.UUID
	if json.Unmarshal(raw, &u) != nil {
		return nil, true, false
	}
	return &u, true, true
}

// jobView loads a job the caller may see; anything else is not found.
func (s *Server) jobView(r *http.Request) (*wsView, db.Job, error) {
	id, err := pathID(r, "jobID")
	if err != nil {
		return nil, db.Job{}, err
	}
	ctx := r.Context()
	j, err := s.Store.GetJob(ctx, id)
	if err != nil {
		return nil, j, httpx.ErrNotFound
	}
	view, err := s.loadWSView(ctx, j.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, j, err
	}
	if !view.v.CanSeeJob(j, view.members) {
		return nil, j, httpx.ErrNotFound
	}
	return view, j, nil
}

// jobEditor is jobView plus the right to change: the owner or a workspace admin.
func (s *Server) jobEditor(r *http.Request) (*wsView, db.Job, error) {
	view, j, err := s.jobView(r)
	if err != nil {
		return nil, j, err
	}
	if !view.v.CanEditJob(j) {
		return nil, j, httpx.ErrForbidden
	}
	return view, j, nil
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	view, err := s.loadWSView(ctx, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	list, err := s.Store.ListJobs(ctx, wsID)
	if err != nil {
		return err
	}
	out := []core.JobView{}
	for _, j := range list {
		if !view.v.CanSeeJob(j, view.members) {
			continue
		}
		v := core.ToJob(j)
		if last, err := s.Store.LastJobRuns(ctx, db.LastJobRunsParams{JobID: j.ID, Limit: 1}); err == nil && len(last) > 0 {
			lr := core.ToJobRun(last[0])
			v.LastRun = &lr
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"jobs": out})
	return nil
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) error {
	_, j, err := s.jobView(r)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"job": core.ToJob(j)})
	return nil
}

// schedulePlan checks the schedule text and zone and returns the cron and its next run.
func schedulePlan(v httpx.Validation, input, zone string, now time.Time) (cron string, next *time.Time) {
	res, err := schedule.Preview(input, zone, now)
	switch {
	case errors.Is(err, schedule.ErrInvalidZone):
		v.Add("timezone", err.Error())
	case err != nil:
		v.Add("schedule", err.Error())
	default:
		cron = res.Cron
		if len(res.Next) > 0 {
			next = &res.Next[0]
		}
	}
	return cron, next
}

// checkJobFields validates what create and patch share. dirs and assignee are only checked when given.
func (s *Server) checkJobFields(ctx context.Context, v httpx.Validation, view *wsView, uid uuid.UUID, in *jobInput, manager *db.Member, assignee *uuid.UUID, assigneeSet bool, runner *uuid.UUID, runnerSet bool) error {
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
		if t == "" || utf8.RuneCountInString(t) > 200 {
			v.Add("title", "Give the job a title of up to 200 characters.")
		}
	}
	if in.Brief != nil {
		b := strings.TrimSpace(*in.Brief)
		in.Brief = &b
		if b == "" || utf8.RuneCountInString(b) > 20000 {
			v.Add("brief", "Describe the job in up to 20,000 characters.")
		}
	}
	if in.MissedPolicy != nil && *in.MissedPolicy != "skip" && *in.MissedPolicy != "run_once" {
		v.Add("missed_policy", "Choose skip or run_once.")
	}
	if in.Notify != nil && *in.Notify != "attention" && *in.Notify != "always" && *in.Notify != "failures" {
		v.Add("notify", "Choose attention, always or failures.")
	}
	if w := in.CatchupWindowSecs; w != nil && (*w < 0 || *w > 7*86400) {
		v.Add("catchup_window_secs", "Choose up to 7 days.")
	}
	if w := in.RunTimeoutSecs; w != nil && (*w < 60 || *w > 7*86400) {
		v.Add("run_timeout_secs", "Choose between 1 minute and 7 days.")
	}
	if c := in.Caps; c != nil {
		if (c.PerRun != nil && *c.PerRun <= 0) || (c.PerMonth != nil && *c.PerMonth <= 0) {
			v.Add("token_caps", "A token cap must be more than zero, or empty for none.")
		}
	}
	if assigneeSet && assignee != nil {
		m, ok := view.members[*assignee]
		if !ok || !view.v.CanSeeMember(m) || (m.Kind == "staff" && manager != nil && (m.ReportsTo == nil || *m.ReportsTo != manager.ID)) {
			v.Add("assignee_member_id", "Choose the manager or one of their staff.")
		}
	}
	if runnerSet && runner != nil {
		rn, err := s.Store.GetRunner(ctx, *runner)
		if err != nil || rn.OrgID != view.ws.OrgID || rn.RevokedAt != nil || (rn.OwnerUserID != uid && !rn.Shared) {
			v.Add("runner_id", "Choose your own computer or a shared one.")
		}
	}
	return nil
}

// checkJobDirs makes sure the member who does the work already has a grant for every directory.
func (s *Server) checkJobDirs(ctx context.Context, v httpx.Validation, view *wsView, worker db.Member, dirs []core.DirNeed) error {
	if len(dirs) == 0 {
		return nil
	}
	grants, err := s.Store.ListMemberGrants(ctx, worker.ID)
	if err != nil {
		return err
	}
	have := map[uuid.UUID]string{}
	for _, g := range grants {
		have[g.DirectoryID] = g.Mode
	}
	known, err := s.Store.ListDirectories(ctx, view.ws.ID)
	if err != nil {
		return err
	}
	inWS := map[uuid.UUID]bool{}
	for _, d := range known {
		inWS[d.ID] = true
	}
	for _, d := range dirs {
		switch {
		case d.Mode != "read" && d.Mode != "read_write":
			v.Add("required_dirs", "Choose read or read_write for each folder.")
		case !inWS[d.DirectoryID]:
			v.Add("required_dirs", "One of the folders isn’t in this workspace.")
		case !(worker.Kind == "manager" && worker.AllDirectories) && (have[d.DirectoryID] == "" || (d.Mode == "read_write" && have[d.DirectoryID] != "read_write")):
			v.Add("required_dirs", worker.Name+" doesn’t have access to one of these folders yet. Grant it first.")
		}
	}
	return nil
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	view, err := s.loadWSView(ctx, wsID, uid)
	if err != nil {
		return err
	}
	var in jobInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	v := httpx.Validation{}
	if in.Title == nil {
		v.Add("title", "Give the job a title.")
	}
	if in.Brief == nil {
		v.Add("brief", "Describe what the job should do.")
	}
	if in.Kind == nil || (*in.Kind != "single" && *in.Kind != "continuous") {
		v.Add("kind", "Choose single or continuous.")
	}
	var manager *db.Member
	if in.ManagerID != nil {
		if m, ok := view.members[*in.ManagerID]; ok && m.Kind == "manager" && view.v.CanSeeMember(m) {
			manager = &m
		}
	}
	if manager == nil {
		v.Add("manager_id", "Choose a manager.")
	}
	assignee, assigneeSet, ok1 := optUUID(in.AssigneeMemberID)
	runner, runnerSet, ok2 := optUUID(in.RunnerID)
	if !ok1 {
		v.Add("assignee_member_id", "That isn’t a member id.")
	}
	if !ok2 {
		v.Add("runner_id", "That isn’t a computer id.")
	}
	if in.OwnerUserID != nil && *in.OwnerUserID != uid {
		v.Add("owner_user_id", "A new job is yours. An admin can hand it over later.")
	}
	if err := s.checkJobFields(ctx, v, view, uid, &in, manager, assignee, assigneeSet, runner, runnerSet); err != nil {
		return err
	}
	org, err := s.Store.GetOrganization(ctx, view.ws.OrgID)
	if err != nil {
		return err
	}
	zone := org.Timezone
	if in.Timezone != nil && strings.TrimSpace(*in.Timezone) != "" {
		zone = strings.TrimSpace(*in.Timezone)
	}
	var cron string
	var next *time.Time
	if in.Schedule == nil {
		v.Add("schedule", "Say when the job runs, for example “every weekday at 9”.")
	} else {
		cron, next = schedulePlan(v, *in.Schedule, zone, s.Now())
	}
	var dirs []core.DirNeed
	if in.RequiredDirs != nil {
		dirs = *in.RequiredDirs
	}
	if manager != nil {
		worker := *manager
		if assignee != nil {
			worker = view.members[*assignee]
		}
		if err := s.checkJobDirs(ctx, v, view, worker, dirs); err != nil {
			return err
		}
	}
	var ends jobEnds
	if len(in.Ends) > 0 && json.Unmarshal(in.Ends, &ends) != nil {
		v.Add("ends", "Give an end time and or a maximum number of runs.")
	}
	if ends.MaxRuns != nil && *ends.MaxRuns <= 0 {
		v.Add("ends", "The maximum number of runs must be more than zero.")
	}
	if err := v.Err(); err != nil {
		return err
	}

	p := db.CreateJobParams{ID: newID(), WorkspaceID: wsID, Title: *in.Title, Kind: *in.Kind, Brief: *in.Brief, OwnerUserID: &uid,
		ManagerID: &manager.ID, AssigneeMemberID: assignee, RunnerID: runner, Cron: cron, Timezone: zone, NextRunAt: next,
		MissedPolicy: "skip", CatchupWindowSecs: 3600, RunTimeoutSecs: 7200, Notify: "attention", EndsAt: ends.At, MaxRuns: ends.MaxRuns,
		CreatedBy: &uid}
	if p.Kind == "continuous" { // continuous jobs catch up once, within 6 hours
		p.MissedPolicy, p.CatchupWindowSecs = "run_once", 6*3600
	}
	if in.MissedPolicy != nil {
		p.MissedPolicy = *in.MissedPolicy
	}
	if in.CatchupWindowSecs != nil {
		p.CatchupWindowSecs = *in.CatchupWindowSecs
	}
	if in.RunTimeoutSecs != nil {
		p.RunTimeoutSecs = *in.RunTimeoutSecs
	}
	if in.Notify != nil {
		p.Notify = *in.Notify
	}
	if in.Caps != nil {
		p.TokenCapPerRun, p.TokenCapPerMonth = in.Caps.PerRun, in.Caps.PerMonth
	}
	if p.RequiredDirs, err = json.Marshal(nonNilDirs(dirs)); err != nil {
		return err
	}
	var job db.Job
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		if job, err = t.Q.CreateJob(ctx, p); err != nil {
			return err
		}
		if job.Kind == "continuous" {
			// The memory note gives revisions, an N- number and read_notes for free.
			note, err := t.Q.CreateNote(ctx, db.CreateNoteParams{ID: newID(), WorkspaceID: wsID, Kind: "job_memory",
				Title: "Memory: " + job.Title, AuthorUserID: &uid, JobID: &job.ID})
			if err != nil {
				return err
			}
			if job, err = t.Q.UpdateJob(ctx, db.UpdateJobParams{ID: job.ID, MemoryNoteID: &note.ID}); err != nil {
				return err
			}
			if err := t.EmitNoteSaved(ctx, view.ws, nil, note); err != nil {
				return err
			}
		}
		if err := t.Emit(ctx, jobEvent(view.ws, job), core.ToJob(job)); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &view.ws.OrgID, &uid, "jobs.created", "Created the job “"+job.Title+"”")
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"job": core.ToJob(job)})
	return nil
}

func nonNilDirs(d []core.DirNeed) []core.DirNeed {
	if d == nil {
		return []core.DirNeed{}
	}
	return d
}

func (s *Server) handlePatchJob(w http.ResponseWriter, r *http.Request) error {
	view, j, err := s.jobEditor(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := view.v.UserID
	var in jobInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Kind != nil && *in.Kind != j.Kind {
		return httpx.Validation{"kind": "A job can’t change between single and continuous."}.Err()
	}
	v := httpx.Validation{}
	assignee, assigneeSet, ok1 := optUUID(in.AssigneeMemberID)
	runner, runnerSet, ok2 := optUUID(in.RunnerID)
	if !ok1 {
		v.Add("assignee_member_id", "That isn’t a member id.")
	}
	if !ok2 {
		v.Add("runner_id", "That isn’t a computer id.")
	}
	manager := view.members[idOrNil(j.ManagerID)]
	if in.ManagerID != nil {
		m, ok := view.members[*in.ManagerID]
		if !ok || m.Kind != "manager" || !view.v.CanSeeMember(m) {
			v.Add("manager_id", "Choose a manager.")
		}
		manager = m
	}
	if in.OwnerUserID != nil && *in.OwnerUserID != idOrNil(j.OwnerUserID) {
		if !view.v.Admin() {
			return httpx.ErrForbidden
		}
		if _, err := s.Store.GetMembership(ctx, db.GetMembershipParams{OrgID: view.ws.OrgID, UserID: *in.OwnerUserID}); err != nil {
			v.Add("owner_user_id", "Choose someone in this organization.")
		}
	}
	if err := s.checkJobFields(ctx, v, view, idOrNil(j.OwnerUserID), &in, &manager, assignee, assigneeSet, runner, runnerSet); err != nil {
		return err
	}
	p := db.UpdateJobParams{ID: j.ID, Title: in.Title, Brief: in.Brief, MissedPolicy: in.MissedPolicy, CatchupWindowSecs: in.CatchupWindowSecs,
		RunTimeoutSecs: in.RunTimeoutSecs, Notify: in.Notify, OwnerUserID: in.OwnerUserID, ManagerID: in.ManagerID,
		SetAssignee: assigneeSet, AssigneeMemberID: assignee, SetRunner: runnerSet, RunnerID: runner}
	if in.Caps != nil {
		p.SetCaps, p.TokenCapPerRun, p.TokenCapPerMonth = true, in.Caps.PerRun, in.Caps.PerMonth
	}
	if in.Schedule != nil || in.Timezone != nil {
		input, zone := j.Cron, j.Timezone
		if in.Schedule != nil {
			input = *in.Schedule
		}
		if in.Timezone != nil {
			zone = strings.TrimSpace(*in.Timezone)
		}
		cron, next := schedulePlan(v, input, zone, s.Now())
		if cron != "" {
			p.Cron, p.Timezone = &cron, &zone
			if j.Status == "active" {
				p.SetNext, p.NextRunAt = true, next
			}
		}
	}
	if len(in.Ends) > 0 {
		var e jobEnds
		if json.Unmarshal(in.Ends, &e) != nil || (e.MaxRuns != nil && *e.MaxRuns <= 0) {
			v.Add("ends", "Give an end time and or a maximum number of runs above zero.")
		}
		p.SetEnds, p.EndsAt, p.MaxRuns = true, e.At, e.MaxRuns
	}
	if in.RequiredDirs != nil || assigneeSet {
		dirs := []core.DirNeed{}
		if in.RequiredDirs != nil {
			dirs = *in.RequiredDirs
		} else {
			_ = json.Unmarshal(j.RequiredDirs, &dirs)
		}
		worker := manager
		who := j.AssigneeMemberID
		if assigneeSet {
			who = assignee
		}
		if who != nil {
			worker = view.members[*who]
		}
		if err := s.checkJobDirs(ctx, v, view, worker, dirs); err != nil {
			return err
		}
		if in.RequiredDirs != nil {
			raw, _ := json.Marshal(nonNilDirs(dirs))
			p.RequiredDirs = raw
		}
	}
	if err := v.Err(); err != nil {
		return err
	}
	var job db.Job
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		if job, err = t.Q.UpdateJob(ctx, p); err != nil {
			return err
		}
		if err := t.Emit(ctx, jobEvent(view.ws, job), core.ToJob(job)); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &view.ws.OrgID, &uid, "jobs.updated", "Changed the job “"+job.Title+"”")
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"job": core.ToJob(job)})
	return nil
}

func idOrNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) error {
	view, j, err := s.jobEditor(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := s.Store.RunningJobRun(ctx, j.ID); err == nil {
		return httpx.NewError(http.StatusConflict, "run_in_progress", "A run is going. Pause the job and wait for it to finish.")
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteJob(ctx, j.ID); err != nil {
			return err
		}
		if err := t.Emit(ctx, jobEvent(view.ws, j), map[string]any{"id": j.ID, "deleted": true}); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &view.ws.OrgID, &view.v.UserID, "jobs.deleted", "Deleted the job “"+j.Title+"”")
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// jobError turns engine errors into plain 409s.
func jobError(err error) error {
	switch {
	case errors.Is(err, jobs.ErrRunning):
		return httpx.NewError(http.StatusConflict, "run_in_progress", "A run of this job is still going.")
	case errors.Is(err, jobs.ErrEnded):
		return httpx.NewError(http.StatusConflict, "job_ended", "This job has ended.")
	case errors.Is(err, jobs.ErrNotPaused):
		return httpx.NewError(http.StatusConflict, "not_paused", "This job isn’t paused.")
	case errors.Is(err, jobs.ErrNoWorker):
		return httpx.NewError(http.StatusConflict, "no_worker", "This job has no one to do the work.")
	}
	return err
}

func (s *Server) handlePauseJob(w http.ResponseWriter, r *http.Request) error {
	view, j, err := s.jobEditor(r)
	if err != nil {
		return err
	}
	job, err := s.Jobs.Pause(r.Context(), j.ID, "owner")
	if err != nil {
		return jobError(err)
	}
	_ = s.audit(r.Context(), s.Store.Queries, &view.ws.OrgID, &view.v.UserID, "jobs.paused", "Paused the job “"+job.Title+"”")
	httpx.JSON(w, http.StatusOK, map[string]any{"job": core.ToJob(job)})
	return nil
}

func (s *Server) handleResumeJob(w http.ResponseWriter, r *http.Request) error {
	view, j, err := s.jobEditor(r)
	if err != nil {
		return err
	}
	job, err := s.Jobs.Resume(r.Context(), j.ID)
	if err != nil {
		return jobError(err)
	}
	_ = s.audit(r.Context(), s.Store.Queries, &view.ws.OrgID, &view.v.UserID, "jobs.resumed", "Resumed the job “"+job.Title+"”")
	httpx.JSON(w, http.StatusOK, map[string]any{"job": core.ToJob(job)})
	return nil
}

// handleRunJob starts a run now; with test=true it is a test run that never writes memory.
func (s *Server) handleRunJob(w http.ResponseWriter, r *http.Request) error {
	view, j, err := s.jobEditor(r)
	if err != nil {
		return err
	}
	var in struct {
		Test bool `json:"test"`
	}
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
	}
	// A run is attributed to the owner, so only the owner's limits matter; admins starting it count against them too.
	var run db.JobRun
	if in.Test {
		run, err = s.Jobs.TestRun(r.Context(), j.ID)
	} else {
		run, err = s.Jobs.RunNow(r.Context(), j.ID)
	}
	if err != nil {
		return jobError(err)
	}
	_ = s.audit(r.Context(), s.Store.Queries, &view.ws.OrgID, &view.v.UserID, "jobs.run", "Started a "+run.Trigger+" run of “"+j.Title+"”")
	httpx.JSON(w, http.StatusAccepted, map[string]any{"run": core.ToJobRun(run)})
	return nil
}

func (s *Server) handleListJobRuns(w http.ResponseWriter, r *http.Request) error {
	_, j, err := s.jobView(r)
	if err != nil {
		return err
	}
	limit := int32(50)
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
		limit = int32(n)
	}
	runs, err := s.Store.LastJobRuns(r.Context(), db.LastJobRunsParams{JobID: j.ID, Limit: limit})
	if err != nil {
		return err
	}
	out := make([]core.JobRunView, 0, len(runs))
	for _, run := range runs {
		out = append(out, core.ToJobRun(run))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"runs": out})
	return nil
}

// memoryNote loads the memory note of a continuous job.
func (s *Server) memoryNote(ctx context.Context, j db.Job) (db.Note, error) {
	if j.Kind != "continuous" || j.MemoryNoteID == nil {
		return db.Note{}, httpx.NewError(http.StatusNotFound, "no_memory", "Only continuous jobs have memory.")
	}
	n, err := s.Store.GetNote(ctx, *j.MemoryNoteID)
	if err != nil {
		return n, httpx.ErrNotFound
	}
	return n, nil
}

func (s *Server) handleGetJobMemory(w http.ResponseWriter, r *http.Request) error {
	_, j, err := s.jobView(r)
	if err != nil {
		return err
	}
	n, err := s.memoryNote(r.Context(), j)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"memory": n.Body, "note_id": n.ID, "number": n.Number, "updated_at": n.UpdatedAt, "max_chars": jobs.MaxMemoryChars})
	return nil
}

// handlePutJobMemory replaces the summary. A run that is going would overwrite it, so it is refused then.
func (s *Server) handlePutJobMemory(w http.ResponseWriter, r *http.Request) error {
	view, j, err := s.jobEditor(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var in struct {
		Memory *string `json:"memory"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Memory == nil {
		return httpx.Validation{"memory": "Send the new summary text."}.Err()
	}
	if utf8.RuneCountInString(*in.Memory) > jobs.MaxMemoryChars {
		return httpx.Validation{"memory": "Keep the summary under " + strconv.Itoa(jobs.MaxMemoryChars) + " characters."}.Err()
	}
	note, err := s.memoryNote(ctx, j)
	if err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if _, err := t.Q.LockJob(ctx, j.ID); err != nil {
			return err
		}
		if _, err := t.Q.RunningJobRun(ctx, j.ID); err == nil {
			return httpx.NewError(http.StatusConflict, "run_in_progress", "A run is going. Edit the memory when it has finished.")
		} else if !store.IsNotFound(err) {
			return err
		}
		prev := note
		if *in.Memory != note.Body {
			if err := t.SaveNoteRevision(ctx, note.ID); err != nil {
				return err
			}
		}
		var err error
		note, err = t.Q.UpdateNote(ctx, db.UpdateNoteParams{ID: note.ID, Body: in.Memory, SetEditor: true, EditedByUserID: &view.v.UserID})
		if err != nil {
			return err
		}
		return t.EmitNoteSaved(ctx, view.ws, &prev, note)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"memory": note.Body, "note_id": note.ID, "number": note.Number, "updated_at": note.UpdatedAt, "max_chars": jobs.MaxMemoryChars})
	return nil
}

// handlePreviewSchedule turns a cron expression or plain text into the cron, a sentence and the next 5 runs.
func (s *Server) handlePreviewSchedule(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Schedule    string     `json:"schedule"`
		Timezone    string     `json:"timezone"`
		WorkspaceID *uuid.UUID `json:"workspace_id"` // its organization's zone is the default
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	zone := strings.TrimSpace(in.Timezone)
	if zone == "" {
		zone = "UTC"
		if in.WorkspaceID != nil {
			ws, _, err := authz.LoadWorkspace(ctx, s.Store.Queries, *in.WorkspaceID, identity(r).UserID)
			if err != nil {
				return err
			}
			if org, err := s.Store.GetOrganization(ctx, ws.OrgID); err == nil {
				zone = org.Timezone
			}
		}
	}
	v := httpx.Validation{}
	res, err := schedule.Preview(in.Schedule, zone, s.Now())
	switch {
	case errors.Is(err, schedule.ErrInvalidZone):
		v.Add("timezone", err.Error())
	case err != nil:
		v.Add("schedule", err.Error())
	}
	if err := v.Err(); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func jobEvent(ws db.Workspace, j db.Job) realtime.Event {
	if j.ManagerID != nil {
		return core.WSMember(ws.OrgID, ws.ID, *j.ManagerID, "job.updated")
	}
	e := core.WS(ws.OrgID, ws.ID, "job.updated")
	e.AdminOnly = true
	return e
}
