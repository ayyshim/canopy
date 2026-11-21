package domain

import (
	"embed"
	"strings"
)

//go:embed guides/*.md
var guideFiles embed.FS

// guides maps a skill key to its working guide. It is not part of the catalog sent to the web app.
var guides = loadGuides()

func loadGuides() map[string]string {
	out := map[string]string{}
	files, _ := guideFiles.ReadDir("guides")
	for _, f := range files {
		data, err := guideFiles.ReadFile("guides/" + f.Name())
		if err != nil {
			continue
		}
		for k, v := range parseGuides(string(data)) {
			out[k] = v
		}
	}
	return out
}

// parseGuides reads "## <key>" sections into a map of key to trimmed body.
func parseGuides(src string) map[string]string {
	out := map[string]string{}
	key := ""
	var body []string
	flush := func() {
		if key != "" {
			out[key] = strings.TrimSpace(strings.Join(body, "\n"))
		}
		body = nil
	}
	for _, line := range strings.Split(src, "\n") {
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			key = strings.TrimSpace(rest)
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}

// SkillGuide is the working guide for a skill key, or "" when there is none.
func SkillGuide(key string) string {
	return guides[key]
}

// GuideKeys lists every key that has a guide.
func GuideKeys() []string {
	keys := make([]string, 0, len(guides))
	for k := range guides {
		keys = append(keys, k)
	}
	return keys
}
