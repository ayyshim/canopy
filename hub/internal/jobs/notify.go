package jobs

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/store/db"
)

// Notify tells the job's owner how a run ended, following the job's notify setting.
// Skipped runs and test runs are silent, and so is an ok or nothing_to_do run of a single job.
func Notify(ctx context.Context, t *core.Tx, job db.Job, run db.JobRun, paused bool) error {
	if job.OwnerUserID == nil || run.Trigger == "test" {
		return nil
	}
	ws, err := t.Q.GetWorkspace(ctx, job.WorkspaceID)
	if err != nil {
		return err
	}
	send := func(body string) error {
		n := notify.Note{Kind: "jobs", MemberID: job.ManagerID, GoalID: run.GoalID, Body: body, Recipients: []uuid.UUID{*job.OwnerUserID}}
		n.Media.Links = notify.ArtifactLinks(run.Summary)
		return notify.Enqueue(ctx, t, ws, n)
	}
	what := fmt.Sprintf("%s, run %d", job.Title, run.Number)
	switch run.Status {
	case "failed", "capped":
		if err := send(what+" failed: "+firstNonEmpty(run.Error, "it stopped before it finished")+"."); err != nil {
			return err
		}
	case "succeeded":
		switch {
		case run.Result == "attention" && job.Notify != "failures":
			if err := send(what+" needs a look: "+firstNonEmpty(run.Summary, run.Title)); err != nil {
				return err
			}
		case job.Notify == "always" && job.Kind == "continuous" && run.Result != "attention":
			if err := send(what+" is done: "+firstNonEmpty(run.Title, run.Summary)); err != nil {
				return err
			}
		}
	}
	if paused {
		return send(job.Title+" is paused after "+fmt.Sprint(FailuresToPause)+" failed runs in a row. Open the job to fix it and resume.")
	}
	return nil
}
