package schedule

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidText is wrapped when plain text is not understood.
var ErrInvalidText = errors.New("schedule text not understood")

const textHelp = `try "every day at 9", "weekdays at 9:30", "every Monday at 8" or "first of the month at 10"`

var (
	timeRe    = regexp.MustCompile(`\s+at\s+(noon|midnight|(\d{1,2})(?::(\d{2}))?\s*(am|pm)?)$`)
	monthDay  = regexp.MustCompile(`^(?:on\s+)?(?:the\s+)?(first|1st|(?:[2-9]|[12]\d|3[01])(?:st|nd|rd|th))\s+(?:day\s+)?of\s+(?:the|every|each)\s+month$`)
	dayWords  = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}
	dayLong   = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	listSplit = regexp.MustCompile(`\s*(?:,|\band\b)\s*`)
)

// FromText turns a plain sentence into a cron expression.
func FromText(text string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(text))
	s = strings.Join(strings.Fields(strings.TrimRight(s, ".")), " ")
	if s == "" {
		return "", fmt.Errorf("%w: it is empty; %s", ErrInvalidText, textHelp)
	}
	m := timeRe.FindStringSubmatch(s)
	if m == nil {
		return "", fmt.Errorf("%w: add a time such as \"at 9\" or \"at 14:30\"; %s", ErrInvalidText, textHelp)
	}
	day := strings.TrimSpace(s[:len(s)-len(m[0])])
	hour, min, err := parseClock(m)
	if err != nil {
		return "", err
	}
	dom, dow, err := parseDay(day)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d %d %s * %s", min, hour, dom, dow), nil
}

func parseClock(m []string) (hour, min int, err error) {
	switch m[1] {
	case "noon":
		return 12, 0, nil
	case "midnight":
		return 0, 0, nil
	}
	hour, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		min, _ = strconv.Atoi(m[3])
	}
	if m[4] != "" {
		if hour < 1 || hour > 12 {
			return 0, 0, fmt.Errorf("%w: %d%s is not a valid time", ErrInvalidText, hour, m[4])
		}
		hour %= 12
		if m[4] == "pm" {
			hour += 12
		}
	}
	if hour > 23 || min > 59 {
		return 0, 0, fmt.Errorf("%w: %q is not a valid time of day", ErrInvalidText, strings.TrimSpace(m[0]))
	}
	return hour, min, nil
}

func parseDay(day string) (dom, dow string, err error) {
	switch day {
	case "every day", "daily", "everyday", "each day":
		return "*", "*", nil
	case "weekdays", "every weekday", "each weekday", "on weekdays":
		return "*", "1-5", nil
	case "weekends", "every weekend", "on weekends":
		return "*", "0,6", nil
	}
	if m := monthDay.FindStringSubmatch(day); m != nil {
		if m[1] == "first" {
			return "1", "*", nil
		}
		return strings.TrimRight(m[1], "stndrh"), "*", nil
	}
	list := strings.TrimPrefix(strings.TrimPrefix(day, "every "), "on ")
	var days []string
	for _, w := range listSplit.Split(strings.TrimPrefix(list, "every "), -1) {
		w = strings.TrimSuffix(w, "s")
		if len(w) < 3 {
			days = nil
			break
		}
		n, ok := dayWords[w[:3]]
		if !ok || !isDayName(w, n) {
			days = nil
			break
		}
		days = append(days, strconv.Itoa(n))
	}
	if len(days) > 0 {
		return "*", strings.Join(days, ","), nil
	}
	return "", "", fmt.Errorf("%w: %q; %s", ErrInvalidText, day, textHelp)
}

func isDayName(w string, n int) bool {
	long := strings.ToLower(dayLong[n])
	return w == long[:3] || w == long || (n == 2 && w == "tues") || (n == 4 && (w == "thur" || w == "thurs")) || (n == 3 && w == "wednes")
}

// Describe turns a cron expression and zone into a plain sentence.
func Describe(expr, zone string) (string, error) {
	c, err := ParseCron(expr)
	if err != nil {
		return "", err
	}
	if _, err := LoadZone(zone); err != nil {
		return "", err
	}
	return describe(c, zone), nil
}

func describe(c *Cron, zone string) string {
	f := strings.Fields(c.expr)
	suffix := ", " + zoneLabel(zone)
	single := func(s string) (int, bool) { v, err := strconv.Atoi(s); return v, err == nil }
	min, okM := single(f[0])
	hour, okH := single(f[1])
	if !okM || !okH || f[3] != "*" {
		return "Custom schedule (" + c.expr + ")" + suffix
	}
	at := fmt.Sprintf("%d:%02d", hour, min)
	switch {
	case f[2] == "*" && f[4] == "*":
		return "Every day at " + at + suffix
	case f[2] == "*" && c.dow == 0b0111110:
		return "Every weekday at " + at + suffix
	case f[2] == "*" && c.dow == 0b1000001:
		return "Every weekend day at " + at + suffix
	case f[2] == "*":
		var names []string
		for d := 0; d < 7; d++ {
			if c.dow&(1<<uint(d)) != 0 {
				names = append(names, dayLong[d])
			}
		}
		return "Every " + joinAnd(names) + " at " + at + suffix
	case f[4] == "*":
		if d, ok := single(f[2]); ok {
			return "On the " + ordinal(d) + " of every month at " + at + suffix
		}
	}
	return "Custom schedule (" + c.expr + ")" + suffix
}

func joinAnd(s []string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

func ordinal(n int) string {
	suf := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suf = "st"
		case 2:
			suf = "nd"
		case 3:
			suf = "rd"
		}
	}
	return strconv.Itoa(n) + suf
}

func zoneLabel(zone string) string {
	zone = strings.TrimSpace(zone)
	if zone == "UTC" || zone == "Etc/UTC" {
		return "UTC"
	}
	name := zone[strings.LastIndex(zone, "/")+1:]
	return strings.ReplaceAll(name, "_", " ") + " time"
}
