package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/realtime"
)

type jobEnv struct {
	e                 *env
	owner, bob, eve   *client
	carol             *client
	ws, riley, dev    string
	scribe, ivy, dir1 string
	dir2              string
}

func newJobEnv(t *testing.T) *jobEnv {
	e := newEnv(t)
	owner, me := e.signup("Jobs Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	m := members(d)
	j := &jobEnv{e: e, owner: owner, ws: ws, riley: str(m["Riley"]["id"]), dev: str(m["Dev"]["id"]), scribe: str(m["Scribe"]["id"]), ivy: str(m["Ivy"]["id"])}
	dirs := d["directories"].([]any)
	j.dir1, j.dir2 = str(dirs[0].(map[string]any)["id"]), str(dirs[1].(map[string]any)["id"])
	var bobID, eveID string
	j.bob, bobID = e.joinOrg(owner, org, "Bob")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff", "manager_ids": []string{j.riley}})
	j.eve, eveID = e.joinOrg(owner, org, "Eve") // in the workspace, but sees no manager
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+eveID, map[string]any{"role": "staff", "manager_ids": []string{}})
	j.carol, _ = e.joinOrg(owner, org, "Carol") // not in the workspace at all
	return j
}

func (j *jobEnv) body(over map[string]any) map[string]any {
	b := map[string]any{"title": "Daily logs", "kind": "single", "brief": "Check the logs and report anything odd.",
		"manager_id": j.riley, "schedule": "every weekday at 9", "timezone": "Asia/Kathmandu"}
	for k, v := range over {
		b[k] = v
	}
	return b
}

func (j *jobEnv) create(c *client, over map[string]any) map[string]any {
	j.e.t.Helper()
	r := c.do("POST", "/api/v1/workspaces/"+j.ws+"/jobs", j.body(over))
	if r.Status != http.StatusCreated {
		j.e.t.Fatalf("create job: %d %v", r.Status, r.Body)
	}
	return r.Body["job"].(map[string]any)
}

func TestCreateJobValidatesAndBuildsMemory(t *testing.T) {
	j := newJobEnv(t)
	job := j.create(j.owner, map[string]any{"kind": "continuous", "title": "Episodes", "assignee_member_id": j.dev,
		"required_dirs": []map[string]any{{"directory_id": j.dir1, "mode": "read_write"}}})
	if job["cron"] != "0 9 * * 1-5" || job["timezone"] != "Asia/Kathmandu" || job["next_run_at"] == nil || !strings.Contains(str(job["schedule"]), "weekday") {
		t.Errorf("schedule: %v", job)
	}
	if job["missed_policy"] != "run_once" || job["status"] != "active" {
		t.Errorf("defaults: %v", job)
	}
	// The memory note exists, is a job_memory note and links back to the job.
	var kind string
	var jobID *uuid.UUID
	err := j.e.store.Pool.QueryRow(context.Background(), `SELECT kind, job_id FROM notes WHERE id = $1`, job["memory_note_id"]).Scan(&kind, &jobID)
	if err != nil || kind != "job_memory" || jobID == nil || jobID.String() != str(job["id"]) {
		t.Errorf("memory note: %v %v %v", kind, jobID, err)
	}
	if single := j.create(j.owner, nil); single["memory_note_id"] != nil || single["missed_policy"] != "skip" {
		t.Errorf("single job: %v", single)
	}

	cases := []struct {
		name  string
		over  map[string]any
		field string
	}{
		{"no title", map[string]any{"title": ""}, "title"},
		{"bad kind", map[string]any{"kind": "weekly"}, "kind"},
		{"bad schedule", map[string]any{"schedule": "whenever"}, "schedule"},
		{"bad zone", map[string]any{"timezone": "Mars/Base"}, "timezone"},
		{"no manager", map[string]any{"manager_id": j.dev}, "manager_id"},
		{"staff of nobody", map[string]any{"assignee_member_id": j.ivy}, "assignee_member_id"},
		{"bad notify", map[string]any{"notify": "loud"}, "notify"},
		{"no grant", map[string]any{"assignee_member_id": j.dev, "required_dirs": []map[string]any{{"directory_id": j.dir2, "mode": "read"}}}, "required_dirs"},
		{"read grant only", map[string]any{"assignee_member_id": j.scribe, "required_dirs": []map[string]any{{"directory_id": j.dir2, "mode": "read_write"}}}, "required_dirs"},
		{"not their computer", map[string]any{"runner_id": uuid.NewString()}, "runner_id"},
	}
	for _, c := range cases {
		r := j.owner.do("POST", "/api/v1/workspaces/"+j.ws+"/jobs", j.body(c.over))
		if r.Status != http.StatusUnprocessableEntity || r.field(c.field) == "" {
			t.Errorf("%s: %d %v", c.name, r.Status, r.Body)
		}
	}
	// A read grant covers a read need.
	j.create(j.owner, map[string]any{"assignee_member_id": j.scribe, "required_dirs": []map[string]any{{"directory_id": j.dir2, "mode": "read"}}})
}

func TestJobPermissions(t *testing.T) {
	j := newJobEnv(t)
	mine := j.create(j.owner, nil)
	id := str(mine["id"])
	path := "/api/v1/jobs/" + id

	// Bob sees Riley's team: he can see and create jobs, but not change the owner's.
	if r := j.bob.do("GET", path, nil); r.Status != 200 {
		t.Errorf("bob get: %d", r.Status)
	}
	bobs := j.create(j.bob, map[string]any{"title": "Bob's job"})
	for _, c := range []struct{ method, p string }{{"PATCH", path}, {"POST", path + "/pause"}, {"POST", path + "/resume"}, {"POST", path + "/run"}, {"PUT", path + "/memory"}, {"DELETE", path}} {
		if r := j.bob.do(c.method, c.p, map[string]any{"title": "x", "memory": "x"}); r.Status != http.StatusForbidden {
			t.Errorf("bob %s %s: %d", c.method, c.p, r.Status)
		}
	}
	// An admin may edit and pause someone else's job; its owner stays Bob.
	bp := "/api/v1/jobs/" + str(bobs["id"])
	if r := j.owner.do("PATCH", bp, map[string]any{"title": "Renamed"}); r.Status != 200 || r.Body["job"].(map[string]any)["title"] != "Renamed" {
		t.Errorf("admin patch: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("POST", bp+"/pause", nil); r.Status != 200 || r.Body["job"].(map[string]any)["status"] != "paused" {
		t.Errorf("admin pause: %d %v", r.Status, r.Body)
	}
	// Only admins hand a job to someone else.
	if r := j.bob.do("PATCH", bp, map[string]any{"owner_user_id": uuid.NewString()}); r.Status != http.StatusForbidden {
		t.Errorf("bob changed owner: %d", r.Status)
	}

	// Eve is in the workspace but sees no manager; Carol is not in it. Both get 404.
	for name, c := range map[string]*client{"eve": j.eve, "carol": j.carol} {
		for _, call := range []struct{ method, p string }{{"GET", path}, {"PATCH", path}, {"POST", path + "/pause"}, {"POST", path + "/run"}, {"GET", path + "/runs"}, {"GET", path + "/memory"}, {"DELETE", path}} {
			if r := c.do(call.method, call.p, map[string]any{}); r.Status != http.StatusNotFound {
				t.Errorf("%s %s %s: %d", name, call.method, call.p, r.Status)
			}
		}
		if r := c.do("POST", "/api/v1/workspaces/"+j.ws+"/jobs", j.body(nil)); r.Status != http.StatusNotFound && r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s create: %d", name, r.Status)
		}
	}
	if r := j.eve.do("GET", "/api/v1/workspaces/"+j.ws+"/jobs", nil); r.Status != 200 || len(r.Body["jobs"].([]any)) != 0 {
		t.Errorf("eve list: %d %v", r.Status, r.Body)
	}
	if r := j.carol.do("GET", "/api/v1/workspaces/"+j.ws+"/jobs", nil); r.Status != http.StatusNotFound {
		t.Errorf("carol list: %d", r.Status)
	}
	if r := j.bob.do("GET", "/api/v1/workspaces/"+j.ws+"/jobs", nil); len(r.Body["jobs"].([]any)) != 2 {
		t.Errorf("bob list: %v", r.Body)
	}
}

func TestJobLifecycleRunsAndMemory(t *testing.T) {
	j := newJobEnv(t)
	job := j.create(j.owner, map[string]any{"kind": "continuous", "title": "Episodes"})
	id := str(job["id"])
	p := "/api/v1/jobs/" + id
	events, cancel := j.e.srv.Bus.Subscribe(realtime.WorkspaceChannel(uuid.MustParse(j.ws)))
	defer cancel()

	// Pause and resume; resuming an active job is a conflict.
	if r := j.owner.do("POST", p+"/pause", nil); r.Status != 200 {
		t.Fatalf("pause: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("POST", p+"/resume", nil); r.Status != 200 || r.Body["job"].(map[string]any)["status"] != "active" {
		t.Fatalf("resume: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("POST", p+"/resume", nil); r.Status != http.StatusConflict || r.errCode() != "not_paused" {
		t.Errorf("resume active: %d %v", r.Status, r.Body)
	}

	// A test run starts a run and a goal; a second run is refused while it goes.
	before := j.owner.do("GET", p, nil).Body["job"].(map[string]any)["next_run_at"]
	r := j.owner.do("POST", p+"/run", map[string]any{"test": true})
	if r.Status != http.StatusAccepted || r.Body["run"].(map[string]any)["trigger"] != "test" {
		t.Fatalf("test run: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("POST", p+"/run", nil); r.Status != http.StatusConflict || r.errCode() != "run_in_progress" {
		t.Errorf("second run: %d %v", r.Status, r.Body)
	}
	if after := j.owner.do("GET", p, nil).Body["job"].(map[string]any)["next_run_at"]; after != before {
		t.Errorf("run moved next_run_at: %v -> %v", before, after)
	}
	runs := j.owner.do("GET", p+"/runs", nil).Body["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["status"] != "running" || runs[0].(map[string]any)["goal_id"] == nil {
		t.Fatalf("runs: %v", runs)
	}

	// Memory: readable any time, but not editable or deletable while a run is going.
	if r := j.owner.do("GET", p+"/memory", nil); r.Status != 200 || r.Body["memory"] != "" {
		t.Errorf("get memory: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("PUT", p+"/memory", map[string]any{"memory": "Keeper is Ada."}); r.Status != http.StatusConflict || r.errCode() != "run_in_progress" {
		t.Errorf("put during run: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("DELETE", p, nil); r.Status != http.StatusConflict {
		t.Errorf("delete during run: %d", r.Status)
	}
	note := str(job["memory_note_id"])
	if r := j.owner.do("PATCH", "/api/v1/notes/"+note, map[string]any{"body": "sneaky"}); r.Status != http.StatusConflict {
		t.Errorf("notes route during run: %d", r.Status)
	}

	// The run ends; now the memory can be saved, with a revision, and is length-checked.
	j.e.store.Pool.Exec(context.Background(), `UPDATE job_runs SET status = 'succeeded', finished_at = now() WHERE job_id = $1`, id)
	if r := j.owner.do("PUT", p+"/memory", map[string]any{"memory": "Keeper is Ada."}); r.Status != 200 || r.Body["memory"] != "Keeper is Ada." {
		t.Fatalf("put memory: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("PUT", p+"/memory", map[string]any{"memory": strings.Repeat("x", 5001)}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("long memory: %d", r.Status)
	}
	if r := j.owner.do("DELETE", "/api/v1/notes/"+note, nil); r.Status != http.StatusConflict {
		t.Errorf("deleting the memory note: %d", r.Status)
	}
	var revs int
	_ = j.e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM note_revisions WHERE note_id = $1`, note).Scan(&revs)
	if revs != 1 {
		t.Errorf("revisions: %d", revs)
	}

	// A single run job has no memory.
	single := j.create(j.owner, nil)
	if r := j.owner.do("GET", "/api/v1/jobs/"+str(single["id"])+"/memory", nil); r.Status != http.StatusNotFound {
		t.Errorf("single memory: %d", r.Status)
	}

	// Events were published for the job and its run.
	got := map[string]bool{}
	for done := false; !done; {
		select {
		case ev := <-events:
			got[ev.Type] = true
		case <-time.After(100 * time.Millisecond):
			done = true
		}
	}
	if !got["job.updated"] || !got["job.run.updated"] {
		t.Errorf("events: %v", got)
	}

	// Delete works once nothing runs; the job and its memory note go.
	if r := j.owner.do("DELETE", p, nil); r.Status != http.StatusNoContent {
		t.Fatalf("delete: %d", r.Status)
	}
	if r := j.owner.do("GET", p, nil); r.Status != http.StatusNotFound {
		t.Errorf("get deleted: %d", r.Status)
	}
	var left int
	_ = j.e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM notes WHERE id = $1`, note).Scan(&left)
	if left != 0 {
		t.Error("memory note survived the job")
	}
}

func TestPatchJobSchedule(t *testing.T) {
	j := newJobEnv(t)
	job := j.create(j.owner, nil)
	p := "/api/v1/jobs/" + str(job["id"])
	r := j.owner.do("PATCH", p, map[string]any{"schedule": "every day at 18:30", "timezone": "UTC", "token_caps": map[string]any{"per_run": 5000}, "notify": "always"})
	got := map[string]any{}
	if r.Status == 200 {
		got = r.Body["job"].(map[string]any)
	}
	if r.Status != 200 || got["cron"] != "30 18 * * *" || got["timezone"] != "UTC" || got["token_cap_per_run"].(float64) != 5000 || got["notify"] != "always" {
		t.Fatalf("patch: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("PATCH", p, map[string]any{"schedule": "nonsense"}); r.Status != http.StatusUnprocessableEntity || r.field("schedule") == "" {
		t.Errorf("bad schedule: %d %v", r.Status, r.Body)
	}
	if r := j.owner.do("PATCH", p, map[string]any{"kind": "continuous"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("kind change: %d", r.Status)
	}
}

func TestSchedulePreview(t *testing.T) {
	j := newJobEnv(t)
	r := j.bob.do("POST", "/api/v1/schedules/preview", map[string]any{"schedule": "every weekday at 9:30", "timezone": "Asia/Kathmandu"})
	if r.Status != 200 || r.Body["cron"] != "30 9 * * 1-5" || len(r.Body["next"].([]any)) != 5 || !strings.Contains(str(r.Body["sentence"]), "Kathmandu") {
		t.Fatalf("preview: %d %v", r.Status, r.Body)
	}
	if r := j.bob.do("POST", "/api/v1/schedules/preview", map[string]any{"schedule": "*/5 * * * *", "workspace_id": j.ws}); r.Status != 200 || r.Body["zone"] == "" {
		t.Errorf("cron with workspace zone: %d %v", r.Status, r.Body)
	}
	if r := j.bob.do("POST", "/api/v1/schedules/preview", map[string]any{"schedule": "someday"}); r.Status != http.StatusUnprocessableEntity || r.field("schedule") == "" {
		t.Errorf("bad text: %d %v", r.Status, r.Body)
	}
	if r := j.bob.do("POST", "/api/v1/schedules/preview", map[string]any{"schedule": "0 9 * * *", "timezone": "Nowhere/Land"}); r.Status != http.StatusUnprocessableEntity || r.field("timezone") == "" {
		t.Errorf("bad zone: %d %v", r.Status, r.Body)
	}
	if r := j.carol.do("POST", "/api/v1/schedules/preview", map[string]any{"schedule": "0 9 * * *", "workspace_id": j.ws}); r.Status != http.StatusNotFound {
		t.Errorf("other workspace: %d", r.Status)
	}
}
