package schedule

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PreviewCount is how many run times Preview returns.
const PreviewCount = 5

// Result is what the create dialog shows for a schedule.
type Result struct {
	Cron     string      `json:"cron"`
	Sentence string      `json:"sentence"`
	Zone     string      `json:"zone"`
	Next     []time.Time `json:"next"` // UTC
}

var cronStart = regexp.MustCompile(`^[0-9*/,?-]+$`)

// Preview reads a cron expression or plain text and returns the cron,
// a sentence and the next 5 run times after now.
func Preview(input, zone string, now time.Time) (Result, error) {
	loc, err := LoadZone(zone)
	if err != nil {
		return Result{}, err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return Result{}, fmt.Errorf("%w: schedule is empty", ErrInvalidText)
	}
	expr := input
	if f := strings.Fields(input); len(f) == 5 && cronStart.MatchString(f[0]) {
		// looks like cron: report cron errors as they are
	} else if expr, err = FromText(input); err != nil {
		return Result{}, err
	}
	c, err := ParseCron(expr)
	if err != nil {
		return Result{}, err
	}
	res := Result{Cron: c.String(), Sentence: describe(c, zone), Zone: zone}
	at := now
	for len(res.Next) < PreviewCount {
		n, ok := c.Next(at, loc)
		if !ok {
			break
		}
		res.Next = append(res.Next, n)
		at = n
	}
	if len(res.Next) == 0 {
		return Result{}, fmt.Errorf("%w: %q never runs (check the day and month)", ErrInvalidCron, c.String())
	}
	return res, nil
}
