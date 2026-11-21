package orch

import (
	"context"
	"io"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
)

// approveHandover records the person's approval and makes the brief the goal's summary.
func (o *Orch) approveHandover(ctx context.Context, t *core.Tx, ws db.Workspace, task db.Task, outID uuid.UUID, by *uuid.UUID) error {
	g, err := t.Q.GetGoal(ctx, *task.GoalID)
	if err != nil {
		return err
	}
	brief := o.readBrief(ctx, t, outID)
	params := db.UpdateGoalParams{ID: g.ID, SetHandover: true, HandoverApprovedBy: by}
	if brief != "" {
		params.Summary = &brief
	}
	if by == nil {
		by = g.CreatedBy
		params.HandoverApprovedBy = by
	}
	ng, err := t.Q.UpdateGoal(ctx, params)
	if err != nil {
		return err
	}
	if brief != "" {
		if _, err := o.saveNote(ctx, t, ws, &g.ID, "summary", "Summary: "+g.Title, brief, "shared", nil, g.ManagerID); err != nil {
			return err
		}
	}
	return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "goal.updated"), core.ToGoal(ng))
}

func (o *Orch) readBrief(ctx context.Context, t *core.Tx, outID uuid.UUID) string {
	versions, err := t.Q.ListOutputVersions(ctx, outID)
	if err != nil || len(versions) == 0 {
		return ""
	}
	latest := versions[0]
	for _, v := range versions {
		if v.Number > latest.Number {
			latest = v
		}
	}
	if o.Blobs == nil {
		return ""
	}
	rc, _, err := o.Blobs.Get(ctx, latest.BlobKey)
	if err != nil {
		return ""
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 256<<10))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
