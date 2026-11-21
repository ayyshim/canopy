package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"regexp"
	"strings"
	"time"
)

type payload struct {
	Username   string          `json:"username,omitempty"`
	AvatarURL  string          `json:"avatar_url,omitempty"`
	Content    string          `json:"content,omitempty"`
	Embeds     []embedJSON     `json:"embeds,omitempty"`
	Components []row           `json:"components,omitempty"`
	Mentions   allowedMentions `json:"allowed_mentions"`
	ThreadName string          `json:"thread_name,omitempty"`
	Attach     []attachment    `json:"attachments,omitempty"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
	Roles []string `json:"roles,omitempty"`
}

type attachment struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
}

type embedJSON struct {
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	URL         string      `json:"url,omitempty"`
	Color       int         `json:"color,omitempty"`
	Fields      []fieldJSON `json:"fields,omitempty"`
	Image       *imageJSON  `json:"image,omitempty"`
	Footer      *footerJSON `json:"footer,omitempty"`
	Timestamp   string      `json:"timestamp,omitempty"`
}

type fieldJSON struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type imageJSON struct {
	URL string `json:"url"`
}

type footerJSON struct {
	Text string `json:"text"`
}

type row struct {
	Type       int      `json:"type"`
	Components []button `json:"components"`
}

type button struct {
	Type  int    `json:"type"`
	Style int    `json:"style"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

var roleIDRe = regexp.MustCompile(`^[0-9]{1,25}$`)

// prepared is a message cut down to Discord limits.
type prepared struct {
	payload payload
	files   []File
}

// prepare trims the message to Discord limits. With buttons false, the
// link buttons are written into the embeds instead.
func prepare(m Message, buttons bool) prepared {
	var kept []File
	var links []string
	skipped := map[string]string{}
	for _, f := range m.Files {
		if f.Name == "" || len(f.Data) == 0 || len(f.Data) > maxFileSize || len(kept) >= maxFiles {
			skipped[f.Name] = f.URL
			switch {
			case f.URL != "":
				links = append(links, f.URL)
			case f.Name != "":
				links = append(links, f.Name+" (too large to attach)")
			}
			continue
		}
		kept = append(kept, f)
	}

	content := m.Content
	var roles []string
	if roleIDRe.MatchString(m.RoleID) {
		roles = []string{m.RoleID}
		if !strings.Contains(content, "<@&"+m.RoleID+">") {
			content = strings.TrimSpace("<@&" + m.RoleID + "> " + content)
		}
	}
	if len(links) > 0 {
		content = strings.TrimSpace(content + "\n" + strings.Join(links, "\n"))
	}

	embeds := append([]Embed(nil), m.Embeds...)
	for i, e := range embeds {
		if name, ok := strings.CutPrefix(e.Image, "attachment://"); ok {
			if url, gone := skipped[name]; gone {
				embeds[i].Image = url
			}
		}
	}

	var rows []row
	if len(m.Buttons) > 0 {
		if buttons {
			rows = buttonRows(m.Buttons)
		} else {
			embeds = withLinks(embeds, m.Buttons)
		}
	}

	p := payload{
		Username:   cleanUsername(m.Username),
		AvatarURL:  m.AvatarURL,
		Content:    truncate(content, maxContent),
		Components: rows,
		Mentions:   allowedMentions{Parse: []string{}, Roles: roles},
	}
	if m.ThreadID == "" {
		p.ThreadName = truncate(m.ThreadName, 100)
	}
	for _, e := range fitEmbeds(embeds) {
		ej := embedJSON{
			Title: e.Title, Description: e.Description, URL: e.URL,
			Color: e.Color, Fields: toFields(e.Fields),
		}
		if e.Image != "" {
			ej.Image = &imageJSON{URL: e.Image}
		}
		if e.Footer != "" {
			ej.Footer = &footerJSON{Text: e.Footer}
		}
		if !e.Timestamp.IsZero() {
			ej.Timestamp = e.Timestamp.UTC().Format(time.RFC3339)
		}
		p.Embeds = append(p.Embeds, ej)
	}
	for i, f := range kept {
		p.Attach = append(p.Attach, attachment{ID: i, Filename: f.Name})
	}
	return prepared{payload: p, files: kept}
}

func toFields(in []Field) []fieldJSON {
	out := make([]fieldJSON, len(in))
	for i, f := range in {
		out[i] = fieldJSON{Name: f.Name, Value: f.Value, Inline: f.Inline}
	}
	return out
}

func buttonRows(bs []Button) []row {
	var rows []row
	for _, b := range bs {
		if b.URL == "" || b.Label == "" {
			continue
		}
		if len(rows) == 0 || len(rows[len(rows)-1].Components) == 5 {
			if len(rows) == 5 {
				break
			}
			rows = append(rows, row{Type: 1})
		}
		r := &rows[len(rows)-1]
		r.Components = append(r.Components, button{Type: 2, Style: 5, Label: truncate(b.Label, 80), URL: b.URL})
	}
	return rows
}

func withLinks(embeds []Embed, bs []Button) []Embed {
	var parts []string
	for _, b := range bs {
		if b.URL != "" && b.Label != "" {
			parts = append(parts, fmt.Sprintf("[%s](%s)", b.Label, b.URL))
		}
	}
	if len(parts) == 0 {
		return embeds
	}
	text := strings.Join(parts, " · ")
	if len(embeds) == 0 {
		return []Embed{{Description: text}}
	}
	embeds[0].Fields = append(append([]Field(nil), embeds[0].Fields...), Field{Name: "Links", Value: text})
	return embeds
}

// encode returns the request body and its content type.
func (p prepared) encode() ([]byte, string, error) {
	js, err := json.Marshal(p.payload)
	if err != nil {
		return nil, "", err
	}
	if len(p.files) == 0 {
		return js, "application/json", nil
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="payload_json"`)
	h.Set("Content-Type", "application/json")
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	part.Write(js)
	for i, f := range p.files {
		ct := f.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files[%d]"; filename="%s"`, i, quoteName(f.Name)))
		h.Set("Content-Type", ct)
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		part.Write(f.Data)
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func quoteName(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "").Replace(s)
}
