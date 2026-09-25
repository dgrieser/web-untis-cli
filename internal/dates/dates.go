// Package dates contains helpers for the date and time formats used by
// WebUntis and for parsing human friendly date arguments.
package dates

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Day truncates t to midnight in its location.
func Day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Today returns today's date at midnight (local time).
func Today() time.Time { return Day(time.Now()) }

// Monday returns the Monday of t's week.
func Monday(t time.Time) time.Time {
	t = Day(t)
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t.AddDate(0, 0, 1-wd)
}

// ISO formats as 2006-01-02.
func ISO(t time.Time) string { return t.Format("2006-01-02") }

// Int formats as the WebUntis integer date 20060102.
func Int(t time.Time) int {
	y, m, d := t.Date()
	return y*10000 + int(m)*100 + d
}

// IntStr formats as "20060102".
func IntStr(t time.Time) string { return strconv.Itoa(Int(t)) }

// FromInt parses the WebUntis integer date 20060102.
func FromInt(v int) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Date(v/10000, time.Month(v/100%100), v%100, 0, 0, 0, 0, time.Local)
}

// FromIntTime combines an integer date and an integer time (e.g. 745 = 07:45).
func FromIntTime(d, hm int) time.Time {
	t := FromInt(d)
	if t.IsZero() {
		return t
	}
	return t.Add(time.Duration(hm/100)*time.Hour + time.Duration(hm%100)*time.Minute)
}

// HM formats an integer time such as 745 as "07:45".
func HM(v int) string {
	return fmt.Sprintf("%02d:%02d", v/100, v%100)
}

// Human formats a date as "Mo 21.09.2026" (German style like the web UI).
func Human(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return WeekdayShort(t) + " " + t.Format("02.01.2006")
}

// Short formats a date as "21.09.".
func Short(t time.Time) string { return t.Format("02.01.") }

var weekdaysDE = []string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}
var weekdaysLongDE = []string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}

// WeekdayShort returns the German two-letter weekday.
func WeekdayShort(t time.Time) string { return weekdaysDE[t.Weekday()] }

// WeekdayLong returns the German weekday name.
func WeekdayLong(t time.Time) string { return weekdaysLongDE[t.Weekday()] }

// ParseLocalDateTime parses "2026-09-21T07:45" / "2026-09-21T07:45:00(.sss)" in local time.
func ParseLocalDateTime(s string) (time.Time, error) {
	for _, l := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, nil
		}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local(), nil
	}
	return time.Time{}, fmt.Errorf("invalid date/time %q", s)
}

var relRe = regexp.MustCompile(`^([+-]?\d+)([dwmy])$`)

var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday, "so": time.Sunday, "sonntag": time.Sunday,
	"mon": time.Monday, "monday": time.Monday, "mo": time.Monday, "montag": time.Monday,
	"tue": time.Tuesday, "tuesday": time.Tuesday, "di": time.Tuesday, "dienstag": time.Tuesday,
	"wed": time.Wednesday, "wednesday": time.Wednesday, "mi": time.Wednesday, "mittwoch": time.Wednesday,
	"thu": time.Thursday, "thursday": time.Thursday, "do": time.Thursday, "donnerstag": time.Thursday,
	"fri": time.Friday, "friday": time.Friday, "fr": time.Friday, "freitag": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday, "sa": time.Saturday, "samstag": time.Saturday,
}

// Parse understands:
//
//	today, tomorrow, yesterday (also heute, morgen, gestern)
//	weekday names (next occurrence, today included): mon, friday, mo, freitag
//	next-week / last-week (Monday of that week)
//	relative offsets: +3d, -1w, 2m, +1y
//	2026-09-21, 21.09.2026, 21.09.26, 21.09. (current year), 20260921
func Parse(s string) (time.Time, error) {
	return ParseRelativeTo(s, Today())
}

// ParseRelativeTo is Parse with an explicit reference day.
func ParseRelativeTo(s string, ref time.Time) (time.Time, error) {
	in := strings.ToLower(strings.TrimSpace(s))
	ref = Day(ref)
	switch in {
	case "", "today", "heute", "now":
		return ref, nil
	case "tomorrow", "morgen":
		return ref.AddDate(0, 0, 1), nil
	case "yesterday", "gestern":
		return ref.AddDate(0, 0, -1), nil
	case "next-week", "nextweek", "nächste-woche":
		return Monday(ref).AddDate(0, 0, 7), nil
	case "last-week", "lastweek", "letzte-woche":
		return Monday(ref).AddDate(0, 0, -7), nil
	case "this-week", "week":
		return Monday(ref), nil
	}
	if wd, ok := weekdayNames[in]; ok {
		diff := (int(wd) - int(ref.Weekday()) + 7) % 7
		return ref.AddDate(0, 0, diff), nil
	}
	if m := relRe.FindStringSubmatch(in); m != nil {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "d":
			return ref.AddDate(0, 0, n), nil
		case "w":
			return ref.AddDate(0, 0, 7*n), nil
		case "m":
			return ref.AddDate(0, n, 0), nil
		case "y":
			return ref.AddDate(n, 0, 0), nil
		}
	}
	for _, l := range []string{"2006-01-02", "02.01.2006", "2.1.2006", "02.01.06", "2.1.06", "20060102"} {
		if t, err := time.ParseInLocation(l, in, time.Local); err == nil {
			return t, nil
		}
	}
	for _, l := range []string{"02.01.", "2.1.", "02.01", "2.1"} {
		if t, err := time.ParseInLocation(l, in, time.Local); err == nil {
			return time.Date(ref.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q (try 2026-09-21, 21.09., today, +1w, monday)", s)
}
