// Package schedule turns a human schedule phrase ("every 6 hours",
// "Wednesday at 1pm", "weekly") OR a raw cron expression into a single
// canonical spec string, and computes the next fire time from it.
//
// The canonical form is whatever github.com/robfig/cron/v3 ParseStandard
// accepts: standard 5-field cron (minute hour dom month dow) plus the
// descriptors @hourly / @daily / @weekly and @every <duration>. English
// phrases are normalized to one of those; anything that isn't recognized
// English is tried as a raw cron expression so power users can still pass
// "0 13 * * 1-5" directly.
//
// All next-fire math runs in the machine's local timezone — flow is a
// single-host personal tool, and robfig's Next(t) honors the location of
// the time it's handed.
package schedule

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"

	"time"
)

// Kind classifies how a spec was produced, for display/grouping only.
const (
	KindPreset  = "preset"  // @hourly / @daily / @weekly
	KindEvery   = "every"   // @every Nh / @every Nm
	KindDaytime = "daytime" // day-and-time, e.g. "Wednesday at 1pm"
	KindCron    = "cron"    // raw cron passthrough
)

// Spec is a normalized, storable schedule.
type Spec struct {
	Input string // the operator's original phrase, verbatim, for display/edit
	Cron  string // canonical cron / descriptor robfig/cron parses
	Kind  string // one of the Kind* constants
}

// Parse normalizes an English phrase or raw cron expression into a Spec.
// Returns an error the caller can surface verbatim when the input is neither
// a recognized phrase nor a valid cron expression.
func Parse(input string) (Spec, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Spec{}, fmt.Errorf("schedule is empty")
	}

	// 1. Recognized English → canonical cron.
	if c, kind, ok := normalizeEnglish(strings.ToLower(raw)); ok {
		if _, err := cron.ParseStandard(c); err != nil {
			// A normalizer bug, not user error — surface loudly.
			return Spec{}, fmt.Errorf("internal: normalized %q to invalid spec %q: %w", raw, c, err)
		}
		return Spec{Input: raw, Cron: c, Kind: kind}, nil
	}

	// 2. Raw cron / descriptor passthrough.
	if _, err := cron.ParseStandard(raw); err == nil {
		return Spec{Input: raw, Cron: raw, Kind: KindCron}, nil
	}

	return Spec{}, fmt.Errorf(
		"could not understand schedule %q (try \"every hour\", \"every 6 hours\", \"weekly\", \"Wednesday at 1pm\", or a cron expression like \"0 13 * * 3\")",
		raw,
	)
}

// Validate reports whether a stored canonical spec still parses.
func Validate(canonicalCron string) error {
	_, err := cron.ParseStandard(canonicalCron)
	return err
}

// Next returns the next fire time strictly after `after`, computed in
// after's timezone (pass a local time for local-clock schedules).
func Next(canonicalCron string, after time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(canonicalCron)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse schedule %q: %w", canonicalCron, err)
	}
	return sched.Next(after), nil
}

// Describe returns a human label for a spec, preferring the operator's own
// phrasing and falling back to the canonical cron when none was recorded
// (e.g. a schedule set directly from raw cron).
func Describe(s Spec) string {
	if strings.TrimSpace(s.Input) != "" {
		return s.Input
	}
	return s.Cron
}

// ---- English normalization ----

var weekdays = map[string]int{
	"sunday": 0, "sun": 0,
	"monday": 1, "mon": 1,
	"tuesday": 2, "tue": 2, "tues": 2,
	"wednesday": 3, "wed": 3,
	"thursday": 4, "thu": 4, "thur": 4, "thurs": 4,
	"friday": 5, "fri": 5,
	"saturday": 6, "sat": 6,
}

var (
	everyNRe  = regexp.MustCompile(`^every\s+(\d+)\s+([a-z]+)$`)
	dailyAtRe = regexp.MustCompile(`^(?:every day|daily|each day) at (.+)$`)
	dayAtRe   = regexp.MustCompile(`^(?:every |on )?([a-z]+?)s? at (.+)$`)
	clockRe   = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
	hourUnits = map[string]bool{"hours": true, "hour": true, "hrs": true, "hr": true, "h": true}
	minUnits  = map[string]bool{"minutes": true, "minute": true, "mins": true, "min": true, "m": true}
)

// normalizeEnglish maps a lowercased phrase to a canonical cron string.
// Returns ok=false when the phrase is not recognized English.
func normalizeEnglish(s string) (canonical, kind string, ok bool) {
	s = strings.TrimSuffix(strings.Join(strings.Fields(s), " "), ".")

	switch s {
	case "every hour", "hourly", "once an hour", "every 1 hour":
		return "@hourly", KindPreset, true
	case "every minute", "every 1 minute":
		return "@every 1m", KindEvery, true
	case "every day", "daily", "once a day", "every 1 day":
		return "@daily", KindPreset, true
	case "every week", "weekly", "weekly once", "once a week", "once weekly", "every 1 week":
		return "@weekly", KindPreset, true
	}

	if m := everyNRe.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 {
			switch {
			case hourUnits[m[2]]:
				return fmt.Sprintf("@every %dh", n), KindEvery, true
			case minUnits[m[2]]:
				return fmt.Sprintf("@every %dm", n), KindEvery, true
			}
		}
		return "", "", false
	}

	// "every day at 9am" / "daily at 09:30"
	if m := dailyAtRe.FindStringSubmatch(s); m != nil {
		if h, min, ok := parseClock(m[1]); ok {
			return fmt.Sprintf("%d %d * * *", min, h), KindDaytime, true
		}
		return "", "", false
	}

	// "[every|on] <weekday>[s] at <time>"
	if m := dayAtRe.FindStringSubmatch(s); m != nil {
		if dow, found := weekdays[m[1]]; found {
			if h, min, ok := parseClock(m[2]); ok {
				return fmt.Sprintf("%d %d * * %d", min, h, dow), KindDaytime, true
			}
		}
		return "", "", false
	}

	return "", "", false
}

// parseClock parses a clock phrase ("1pm", "1:30pm", "13:00", "9am", "noon",
// "midnight") into 24-hour (hour, minute). ok=false on anything unparseable.
func parseClock(s string) (hour, min int, ok bool) {
	s = strings.TrimSpace(s)
	switch s {
	case "noon":
		return 12, 0, true
	case "midnight":
		return 0, 0, true
	}
	m := clockRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	h, _ := strconv.Atoi(m[1])
	if m[2] != "" {
		min, _ = strconv.Atoi(m[2])
	}
	switch m[3] {
	case "am":
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		if h == 12 {
			h = 0
		}
	case "pm":
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		if h != 12 {
			h += 12
		}
	default: // 24-hour
		if h > 23 {
			return 0, 0, false
		}
	}
	if min > 59 {
		return 0, 0, false
	}
	return h, min, true
}
