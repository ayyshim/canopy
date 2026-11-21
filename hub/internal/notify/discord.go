package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/discord"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

var imageTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

var kindColors = map[string]int{
	"questions":  0xF5A524,
	"progress":   0x3B82F6,
	"goal_ready": 0x22C55E,
	"errors":     0xEF4444,
	"jobs":       0x8B5CF6,
}

const maxImageRead = 8 << 20

// DiscordChannel sends outbox rows to each workspace's Discord webhook.
type DiscordChannel struct {
	Store     *store.Store
	Core      *core.Service
	Blobs     blob.Store
	PublicURL string
	Post      func(ctx context.Context, webhook string, m discord.Message) (discord.Result, error)
}

func NewDiscordChannel(s *store.Store, c *core.Service, b blob.Store, publicURL string) *DiscordChannel {
	return &DiscordChannel{Store: s, Core: c, Blobs: b, PublicURL: strings.TrimRight(publicURL, "/"), Post: discord.Send}
}

func (c *DiscordChannel) base() string { return c.PublicURL }

// reachable reports whether Discord could open links to the hub.
func (c *DiscordChannel) reachable() bool {
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	return h != "localhost" && !strings.HasSuffix(h, ".local") && !strings.HasSuffix(h, ".internal") && !strings.HasPrefix(h, "127.") && !strings.HasPrefix(h, "192.168.") && !strings.HasPrefix(h, "10.")
}

func (c *DiscordChannel) Send(ctx context.Context, row db.NotificationOutbox) error {
	q := c.Store.Queries
	ws, err := q.GetWorkspace(ctx, row.WorkspaceID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil
		}
		return err
	}
	var media Media
	_ = json.Unmarshal(row.Media, &media)
	if ws.DiscordWebhook == "" || !media.Test && !ParseDiscordKinds(ws.DiscordKinds)[row.Kind] {
		return nil
	}

	msg, goal := c.build(ctx, ws, row, media)
	if ws.DiscordForum && goal != nil {
		if goal.DiscordThreadID != "" {
			msg.ThreadID = goal.DiscordThreadID
		} else {
			msg.ThreadName = goal.Title
		}
	}
	res, err := c.Post(ctx, ws.DiscordWebhook, msg)
	if errors.Is(err, discord.ErrWebhookGone) && msg.ThreadID != "" {
		// The thread may have been deleted: start a new one.
		msg.ThreadID, msg.ThreadName = "", goal.Title
		_ = q.SetGoalDiscordThread(ctx, db.SetGoalDiscordThreadParams{ID: goal.ID, DiscordThreadID: ""})
		res, err = c.Post(ctx, ws.DiscordWebhook, msg)
	}
	switch {
	case err == nil:
		if msg.ThreadName != "" && goal != nil && res.ChannelID != "" {
			_ = q.SetGoalDiscordThread(ctx, db.SetGoalDiscordThreadParams{ID: goal.ID, DiscordThreadID: res.ChannelID})
		}
		return q.SetWorkspaceDiscordStatus(ctx, db.SetWorkspaceDiscordStatusParams{ID: ws.ID, Error: ""})
	case errors.Is(err, discord.ErrWebhookGone):
		return c.switchOff(ctx, ws)
	default:
		_ = q.SetWorkspaceDiscordStatus(ctx, db.SetWorkspaceDiscordStatusParams{ID: ws.ID, Error: err.Error()})
		if !discord.IsRetryable(err) {
			return nil // retrying a rejected message won't help
		}
		return err
	}
}

// switchOff turns Discord off for the workspace and tells its admins once.
func (c *DiscordChannel) switchOff(ctx context.Context, ws db.Workspace) error {
	const text = "Discord notifications are off: the webhook was deleted or is no longer valid. Add a new webhook in the workspace settings."
	return c.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DisableWorkspaceDiscord(ctx, db.DisableWorkspaceDiscordParams{ID: ws.ID, Error: text}); err != nil {
			return err
		}
		if _, err := t.Q.OpenWorkspaceDiscordSignal(ctx, ws.ID); err == nil {
			return nil
		} else if !store.IsNotFound(err) {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"source": "discord", "message": text})
		sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: ws.ID, Type: "error", Payload: payload})
		if err != nil {
			return err
		}
		e := core.WS(ws.OrgID, ws.ID, "signal.opened")
		if err := t.Emit(ctx, e, core.ToSignal(sig)); err != nil {
			return err
		}
		return EnqueueSignal(ctx, t, ws, sig)
	})
}

// build makes the Discord message for a row and returns the goal it belongs to, if any.
func (c *DiscordChannel) build(ctx context.Context, ws db.Workspace, row db.NotificationOutbox, media Media) (discord.Message, *db.Goal) {
	q := c.Store.Queries
	emb := discord.Embed{
		Title: row.Title, Description: row.Body, Color: kindColors[row.Kind],
		Footer: "Canopy", Timestamp: row.CreatedAt,
	}
	msg := discord.Message{Username: row.Title}
	if c.reachable() {
		if ws.LogoKey != nil {
			msg.AvatarURL = c.base() + "/api/v1/public/workspaces/" + ws.ID.String() + "/logo"
		}
	}
	link := ""
	if row.Link != "" {
		link = c.base() + row.Link
		emb.URL = link
	}
	if row.Kind == "questions" && ws.DiscordRoleID != "" {
		msg.RoleID = ws.DiscordRoleID
	}

	var goal *db.Goal
	if row.GoalID != nil {
		if g, err := q.GetGoal(ctx, *row.GoalID); err == nil {
			goal = &g
			emb.Fields = append(emb.Fields, discord.Field{Name: "Goal", Value: g.Title, Inline: true})
		}
	}
	if row.TaskID != nil {
		if t, err := q.GetTask(ctx, *row.TaskID); err == nil {
			emb.Fields = append(emb.Fields, discord.Field{Name: "Task", Value: t.Title, Inline: true})
		}
	}

	var outLinks []string
	if media.Test {
		if data := sampleImage(); data != nil {
			msg.Files = append(msg.Files, discord.File{Name: "sample.png", ContentType: "image/png", Data: data})
			emb.Image = "attachment://sample.png"
		}
	}
	for _, id := range media.OutputIDs {
		o, err := q.GetOutput(ctx, id)
		if err != nil {
			continue
		}
		label := o.Title
		if label == "" {
			label = o.Name
		}
		viewer := c.base() + outputLink(ws.ID, row.GoalID, row.TaskID, o.ID)
		v, err := q.GetLatestOutputVersion(ctx, o.ID)
		ext, isImage := imageTypes[v.Mime]
		if err != nil || !isImage || v.Size > maxImageRead || c.Blobs == nil {
			outLinks = append(outLinks, fmt.Sprintf("[%s](%s)", label, viewer))
			continue
		}
		data, err := c.readBlob(ctx, v.BlobKey)
		if err != nil {
			outLinks = append(outLinks, fmt.Sprintf("[%s](%s)", label, viewer))
			continue
		}
		name := fileName(o.Name, ext)
		msg.Files = append(msg.Files, discord.File{Name: name, ContentType: v.Mime, Data: data, URL: viewer})
		if emb.Image == "" {
			emb.Image = "attachment://" + name
		}
	}
	if len(outLinks) > 0 {
		emb.Fields = append(emb.Fields, discord.Field{Name: "Outputs", Value: strings.Join(outLinks, "\n")})
	}

	if link != "" {
		msg.Buttons = append(msg.Buttons, discord.Button{Label: "Open in Canopy", URL: link})
	}
	for _, l := range media.Links {
		msg.Buttons = append(msg.Buttons, discord.Button{Label: l.Label, URL: l.URL})
	}
	msg.Embeds = []discord.Embed{emb}
	return msg, goal
}

// outputLink opens the output in the viewer on the board.
func outputLink(wsID uuid.UUID, goal, task *uuid.UUID, output uuid.UUID) string {
	q := url.Values{}
	if goal != nil {
		q.Set("goal", goal.String())
	}
	if task != nil {
		q.Set("task", task.String())
	}
	q.Set("output", output.String())
	return "/w/" + wsID.String() + "?" + q.Encode()
}

func (c *DiscordChannel) readBlob(ctx context.Context, key string) ([]byte, error) {
	rc, _, err := c.Blobs.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxImageRead+1))
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// fileName gives a safe attachment name that ends in the right extension.
func fileName(name, ext string) string {
	base := strings.Trim(unsafeName.ReplaceAllString(path.Base(name), "-"), "-.")
	if base == "" {
		base = "image"
	}
	if strings.ToLower(path.Ext(base)) != ext && !(ext == ".jpg" && strings.EqualFold(path.Ext(base), ".jpeg")) {
		base += ext
	}
	return base
}

// sampleImage is the picture sent with the test message.
func sampleImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 480, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 480; x++ {
			img.Set(x, y, color.RGBA{R: uint8(40 + x/4), G: uint8(120 + y/3), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return nil
	}
	return buf.Bytes()
}
