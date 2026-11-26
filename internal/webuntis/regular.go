package webuntis

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/dates"
)

// RegularPlan is the regular timetable (Regelstundenplan) reconstructed from
// several weeks around a target week. Weeks holds one week, or two (A and B
// week) if the timetable alternates weekly.
type RegularPlan struct {
	// Source is "base" (base timetable of the JSON-RPC API, without any
	// changes) or "derived" (derived from the timetable with changes).
	Source      string       `json:"source" yaml:"source"`
	Alternating bool         `json:"alternating" yaml:"alternating"`
	Weeks       []*Timetable `json:"weeks" yaml:"weeks"`
	Notes       []string     `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// Regular plan sources.
const (
	SourceAuto    = "auto"
	SourceBase    = "base"
	SourceDerived = "derived"
)

// RegularOptions controls RegularPlan.
type RegularOptions struct {
	// WeeksBefore / WeeksAfter: how many weeks around the target week are
	// compared (clipped to the school year).
	WeeksBefore, WeeksAfter int
	// Source: SourceAuto (base timetable if available, else derived),
	// SourceBase or SourceDerived.
	Source string
}

// RegularPlan reconstructs the regular timetable of the week of q.Start.
//
// The week of q.Start is the baseline. Weeks before and after it are used to
// detect an A/B rhythm and to fill gaps (holidays, lessons missing in the
// baseline, afternoon lessons): a week only serves as gap filler if all
// lessons it shares with the baseline are identical, so weeks of a different
// timetable (Stundenplanwechsel) are ignored. The weeks come from the base
// timetable of the JSON-RPC API (getTimetable with onlyBaseTimetable), which
// contains no substitutions and cancellations, or, if that is not available,
// from the regular timetable view with the changes removed (WithoutChanges).
func (c *Client) RegularPlan(ctx context.Context, q TimetableQuery, opt RegularOptions) (*RegularPlan, error) {
	monday := dates.Monday(q.Start)
	weekQuery := func(k int) TimetableQuery {
		wq := q
		wq.Start = monday.AddDate(0, 0, 7*k)
		wq.End = wq.Start.AddDate(0, 0, 4)
		return wq
	}
	target, err := c.Timetable(ctx, weekQuery(0))
	if err != nil {
		return nil, err
	}
	from, to := -max(0, opt.WeeksBefore), max(0, opt.WeeksAfter)
	if sy, err := c.SchoolYearFor(ctx, monday); err == nil {
		s, e := sy.DateRange.StartTime(), sy.DateRange.EndTime()
		for from < 0 && monday.AddDate(0, 0, 7*from+4).Before(s) {
			from++
		}
		for to > 0 && !e.IsZero() && monday.AddDate(0, 0, 7*to).After(e) {
			to--
		}
	}
	var notes []string
	weeks := map[int]*Timetable{}
	source := ""
	src := strings.ToLower(opt.Source)
	if src == "" {
		src = SourceAuto
	}
	if src == SourceAuto || src == SourceBase {
		base, err := c.BaseTimetable(ctx, TimetableQuery{ResourceType: q.ResourceType, ResourceID: q.ResourceID,
			Start: monday.AddDate(0, 0, 7*from), End: monday.AddDate(0, 0, 7*to+4)})
		switch {
		case err == nil && base.hasLessons():
			source = SourceBase
			colors := subjectsOf(target)
			for k := from; k <= to; k++ {
				wk := base.slice(monday.AddDate(0, 0, 7*k), monday.AddDate(0, 0, 7*k+4))
				wk.ResourceType, wk.Resource, wk.TimeGrid = target.ResourceType, target.Resource, target.TimeGrid
				colors.apply(wk)
				weeks[k] = wk
			}
		case src == SourceBase && err != nil:
			return nil, fmt.Errorf("base timetable: %w", err)
		case src == SourceBase:
			return nil, fmt.Errorf("base timetable: no lessons between %s and %s", dates.Human(monday.AddDate(0, 0, 7*from)), dates.Human(monday.AddDate(0, 0, 7*to+4)))
		case err != nil:
			c.debugf("base timetable unavailable: %v", err)
			notes = append(notes, "Basisstundenplan (JSON-RPC) nicht verfügbar, Regelplan aus dem Stundenplan mit Änderungen abgeleitet")
		default:
			notes = append(notes, "Basisstundenplan (JSON-RPC) ist leer, Regelplan aus dem Stundenplan mit Änderungen abgeleitet")
		}
	}
	if source == "" {
		source = SourceDerived
		for k := from; k <= to; k++ {
			wk := target
			if k != 0 {
				if wk, err = c.Timetable(ctx, weekQuery(k)); err != nil {
					notes = append(notes, fmt.Sprintf("KW %d nicht abrufbar: %v", isoWeek(monday.AddDate(0, 0, 7*k)), err))
					continue
				}
			}
			weeks[k] = wk.WithoutChanges()
		}
	}
	var list []*Timetable
	var ks []int
	for k := from; k <= to; k++ {
		if w := weeks[k]; w != nil {
			list = append(list, w)
			ks = append(ks, k)
		}
	}
	plan := BuildRegularPlan(list, ks, c.schoolDays(ctx))
	plan.Source = source
	plan.Notes = append(notes, plan.Notes...)
	return plan, nil
}

// schoolDays returns a function telling school days (Monday to Friday, not
// in the holidays of the school year), or nil if the holiday calendar is
// not available.
func (c *Client) schoolDays(ctx context.Context) func(time.Time) bool {
	ad, err := c.AppData(ctx)
	if err != nil || len(ad.Holidays) == 0 {
		return nil
	}
	type span struct{ from, to time.Time }
	var hs []span
	for _, h := range ad.Holidays {
		from, err1 := dates.ParseLocalDateTime(firstNonEmpty(h.StartDate, h.Start))
		to, err2 := dates.ParseLocalDateTime(firstNonEmpty(h.EndDate, h.End))
		if err1 != nil || err2 != nil {
			continue
		}
		hs = append(hs, span{dates.Day(from), dates.Day(to)})
	}
	if len(hs) == 0 {
		return nil
	}
	return func(d time.Time) bool {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			return false
		}
		d = dates.Day(d)
		for _, h := range hs {
			if !d.Before(h.from) && !d.After(h.to) {
				return false
			}
		}
		return true
	}
}

// BaseTimetable fetches the base timetable (Basisstundenplan, without
// substitutions, cancellations and additional lessons) via the JSON-RPC
// method getTimetable with onlyBaseTimetable. Any remaining change
// information is kept, so WithoutChanges can be applied on top.
func (c *Client) BaseTimetable(ctx context.Context, q TimetableQuery) (*Timetable, error) {
	et, ok := elementTypes[q.ResourceType]
	if !ok {
		return nil, fmt.Errorf("no base timetable for resource type %s", q.ResourceType)
	}
	fields := []string{"id", "name", "longname"}
	params := map[string]any{"options": map[string]any{
		"element":           map[string]any{"id": q.ResourceID, "type": et},
		"startDate":         rpcDate(q.Start),
		"endDate":           rpcDate(q.End),
		"onlyBaseTimetable": true,
		"showLsText":        true,
		"showStudentgroup":  true,
		"klasseFields":      fields,
		"teacherFields":     fields,
		"subjectFields":     fields,
		"roomFields":        fields,
	}}
	var periods []rpcPeriod
	if err := c.RPC(ctx, "getTimetable", params, &periods); err != nil {
		return nil, err
	}
	return baseTimetable(q, periods), nil
}

type rpcElement struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	LongName    string `json:"longname"`
	OrgID       int    `json:"orgid"`
	OrgName     string `json:"orgname"`
	OrgLongName string `json:"orglongname"`
}

type rpcPeriod struct {
	ID        int          `json:"id"`
	Date      int          `json:"date"`
	StartTime int          `json:"startTime"`
	EndTime   int          `json:"endTime"`
	Kl        []rpcElement `json:"kl"`
	Te        []rpcElement `json:"te"`
	Su        []rpcElement `json:"su"`
	Ro        []rpcElement `json:"ro"`
	LsType    string       `json:"lstype"`
	Code      string       `json:"code"`
	LsText    string       `json:"lstext"`
}

func rpcDate(t time.Time) int {
	n, _ := strconv.Atoi(t.Format("20060102"))
	return n
}

func hhmm(day time.Time, v int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), v/100, v%100, 0, 0, day.Location())
}

// baseTimetable converts JSON-RPC periods into a Timetable.
func baseTimetable(q TimetableQuery, periods []rpcPeriod) *Timetable {
	tt := &Timetable{ResourceType: q.ResourceType, Resource: Resource{ID: q.ResourceID}, Start: q.Start, End: q.End}
	// Replaced elements may only come with their id.
	names := map[int]rpcElement{}
	for _, p := range periods {
		for _, el := range [][]rpcElement{p.Kl, p.Te, p.Su, p.Ro} {
			for _, e := range el {
				if e.ID != 0 && e.Name != "" {
					names[e.ID] = e
				}
			}
		}
	}
	elements := func(in []rpcElement) []Element {
		var out []Element
		for _, e := range in {
			el := Element{Name: e.Name, LongName: e.LongName}
			if e.ID == 0 && e.Name == "" && e.OrgID == 0 && e.OrgName == "" {
				continue
			}
			if e.Name == "" && e.ID != 0 {
				el.Name, el.LongName = names[e.ID].Name, names[e.ID].LongName
			}
			if e.OrgID != 0 || e.OrgName != "" {
				el.Removed = firstNonEmpty(e.OrgName, names[e.OrgID].Name, strconv.Itoa(e.OrgID))
				el.Status = "CHANGED"
				if el.Removed == el.Name {
					el.Status = "REGULAR"
				}
			}
			out = append(out, el)
		}
		return out
	}
	byDay := map[int][]Lesson{}
	for _, p := range periods {
		lt := strings.ToLower(p.LsType)
		if lt != "" && lt != "ls" && lt != "ex" { // office hours, standby, break supervision
			continue
		}
		day, err := time.ParseInLocation("20060102", strconv.Itoa(p.Date), time.Local)
		if err != nil {
			continue
		}
		l := Lesson{IDs: []int{p.ID}, Start: hhmm(day, p.StartTime), End: hhmm(day, p.EndTime), Type: "NORMAL_TEACHING_PERIOD", Status: "REGULAR",
			Subjects: elements(p.Su), Teachers: elements(p.Te), Rooms: elements(p.Ro), Classes: elements(p.Kl), LessonText: p.LsText}
		if lt == "ex" {
			l.Type = "EXAM"
		}
		switch strings.ToLower(p.Code) {
		case "cancelled":
			l.Status = "CANCELLED"
		case "irregular":
			l.Status = "ADDITIONAL"
		}
		byDay[p.Date] = append(byDay[p.Date], l)
	}
	for d := dates.Day(q.Start); !d.After(q.End); d = d.AddDate(0, 0, 1) {
		ls := byDay[rpcDate(d)]
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].Start.Before(ls[j].Start) })
		tt.Days = append(tt.Days, TimetableDay{Date: d, Lessons: ls})
	}
	return tt.WithoutChanges()
}

func (tt *Timetable) hasLessons() bool {
	for _, d := range tt.Days {
		for _, l := range d.Lessons {
			if !l.AllDay {
				return true
			}
		}
	}
	return false
}

// slice returns the days start..end of tt as a new timetable.
func (tt *Timetable) slice(start, end time.Time) *Timetable {
	out := *tt
	out.Start, out.End, out.Days = start, end, nil
	for _, d := range tt.Days {
		if !dates.Day(d.Date).Before(dates.Day(start)) && !dates.Day(d.Date).After(dates.Day(end)) {
			out.Days = append(out.Days, d)
		}
	}
	return &out
}

// subjectInfo holds the color and long name of a subject, taken from the
// timetable view (the JSON-RPC timetable has no colors).
type subjectInfo struct{ color, long string }

type subjects map[string]subjectInfo

func subjectsOf(tt *Timetable) subjects {
	s := subjects{}
	for _, d := range tt.Days {
		for _, l := range d.Lessons {
			for _, e := range l.Subjects {
				for _, n := range []string{e.Name, e.Removed} {
					if n != "" {
						s[n] = subjectInfo{color: firstNonEmpty(s[n].color, l.Color), long: firstNonEmpty(s[n].long, e.LongName)}
					}
				}
			}
		}
	}
	return s
}

func (s subjects) apply(tt *Timetable) {
	for di := range tt.Days {
		for li := range tt.Days[di].Lessons {
			l := &tt.Days[di].Lessons[li]
			for ei := range l.Subjects {
				info, ok := s[l.Subjects[ei].Name]
				if !ok {
					continue
				}
				if l.Color == "" {
					l.Color = info.color
				}
				if l.Subjects[ei].LongName == "" {
					l.Subjects[ei].LongName = info.long
				}
			}
		}
	}
}

func isoWeek(t time.Time) int { _, w := t.ISOWeek(); return w }

// ------------------------------------------------------------ reconstruction

// piece is the part of a lesson within one slot (period of the time grid).
type piece struct {
	l   Lesson
	idx int // index of the time grid unit, -1 if outside the grid
}

type planDay struct {
	date  time.Time
	slots map[string][]piece
	// free: a school day without lessons (known to be free, unlike a day
	// without data or a holiday)
	free bool
}

type planWeek struct {
	k    int
	tt   *Timetable
	days [7]*planDay // Monday = 0
}

func weekdayIndex(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

func clock(t time.Time) int { return t.Hour()*60 + t.Minute() }

func at(day time.Time, minutes int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, day.Location())
}

// newPlanWeek splits the lessons of tt into slots. schoolDay (may be nil)
// tells school days from holidays: a weekday without lessons is a known free
// day only if it is a school day by the holiday calendar, its status is
// regular, and the week has lessons on another day (a week without any
// lessons is no data, e.g. not published yet).
func newPlanWeek(k int, tt *Timetable, schoolDay func(time.Time) bool) *planWeek {
	w := &planWeek{k: k, tt: tt}
	for _, d := range tt.Days {
		pd := &planDay{date: dates.Day(d.Date), slots: map[string][]piece{}}
		for _, l := range d.Lessons {
			if l.AllDay || l.Start.IsZero() {
				continue
			}
			ls, le := clock(l.Start), clock(l.End)
			add := func(from, to, idx int, key string) {
				p := l
				p.Start, p.End = at(pd.date, from), at(pd.date, to)
				pd.slots[key] = append(pd.slots[key], piece{p, idx})
			}
			outside := func(from, to int) string { return fmt.Sprintf("t%04d-%04d", from/60*100+from%60, to/60*100+to%60) }
			// Split at the periods of the time grid; parts before the first
			// and after the last period it touches are kept as pieces of
			// their own, so no teaching time is lost. Breaks between periods
			// come back when the pieces are merged again.
			first, last := -1, -1
			for i, u := range tt.TimeGrid {
				us, ue := u.StartTime/100*60+u.StartTime%100, u.EndTime/100*60+u.EndTime%100
				if ls < ue && le > us {
					add(max(ls, us), min(le, ue), i, fmt.Sprintf("%04d-%04d", u.StartTime, u.EndTime))
					if first < 0 || us < first {
						first = us
					}
					last = max(last, ue)
				}
			}
			switch {
			case first < 0:
				add(ls, le, -1, outside(ls, le))
			default:
				if ls < first {
					add(ls, first, -1, outside(ls, first))
				}
				if le > last {
					add(last, le, -1, outside(last, le))
				}
			}
		}
		w.days[weekdayIndex(d.Date)] = pd
	}
	if schoolDay != nil && w.hasData() {
		for _, d := range tt.Days {
			wd := weekdayIndex(d.Date)
			pd := w.days[wd]
			if wd < 5 && len(pd.slots) == 0 && (d.Status == "" || d.Status == "REGULAR") && schoolDay(dates.Day(d.Date)) {
				pd.free = true
			}
		}
	}
	return w
}

// known reports whether the content of the day is known: it has lessons
// or is a known free day.
func (w *planWeek) known(wd int) bool { return w.lessons(wd) || w.days[wd] != nil && w.days[wd].free }

// lessons reports whether the day has lessons.
func (w *planWeek) lessons(wd int) bool { return w.days[wd] != nil && len(w.days[wd].slots) > 0 }

func (w *planWeek) hasData() bool {
	for wd := range w.days {
		if w.lessons(wd) {
			return true
		}
	}
	return false
}

func (w *planWeek) week() int { return isoWeek(w.tt.Start) }

// dayOf returns the day of the timetable with weekday wd, if any.
func (w *planWeek) dayOf(wd int) *TimetableDay {
	for i, d := range w.tt.Days {
		if weekdayIndex(d.Date) == wd {
			return &w.tt.Days[i]
		}
	}
	return nil
}

// lessonSig identifies a lesson for comparisons: subjects and teachers.
// Rooms are ignored, they change too often.
func lessonSig(l Lesson) string {
	subj := strings.Split(strings.ToUpper(l.SubjectLabel()), ", ")
	teach := strings.Split(l.TeacherLabel(), ", ")
	slices.Sort(subj)
	slices.Sort(teach)
	return strings.Join(subj, ",") + "/" + strings.Join(teach, ",")
}

// sig is the content of a slot ("" = free).
func (w *planWeek) sig(wd int, key string) string {
	if w.days[wd] == nil {
		return ""
	}
	var s []string
	for _, p := range w.days[wd].slots[key] {
		s = append(s, lessonSig(p.l))
	}
	slices.Sort(s)
	return strings.Join(slices.Compact(s), "|")
}

func (w *planWeek) keys(wd int) []string {
	if w.days[wd] == nil {
		return nil
	}
	var out []string
	for k := range w.days[wd].slots {
		out = append(out, k)
	}
	return out
}

type comparison struct {
	conflicts int // slot used in both weeks with different lessons
	overlap   int // slot used in both weeks with the same lessons
	presence  int // slot used in only one of the weeks
}

// compare compares the days that have lessons in both weeks.
func compare(a, b *planWeek) comparison {
	var c comparison
	for wd := range 7 {
		if !a.known(wd) || !b.known(wd) {
			continue
		}
		keys := append(a.keys(wd), b.keys(wd)...)
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			sa, sb := a.sig(wd, key), b.sig(wd, key)
			switch {
			case sa == "" && sb == "":
			case sa == "" || sb == "":
				c.presence++
			case sa == sb:
				c.overlap++
			default:
				c.conflicts++
			}
		}
	}
	return c
}

// matches reports whether b may fill gaps of a: every slot both weeks use is
// identical and there is enough overlap to tell.
func (c comparison) matches() bool { return c.conflicts == 0 && c.overlap >= 2 }

// nearer reports whether a is nearer to the target week than b, preferring
// the future on ties.
func nearer(a, b *planWeek) bool {
	return abs(a.k) < abs(b.k) || abs(a.k) == abs(b.k) && a.k > b.k
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func odd(k int) bool { return k%2 != 0 }

func weekList(ws []*planWeek) string {
	var s []string
	for _, w := range ws {
		s = append(s, strconv.Itoa(w.week()))
	}
	if len(s) == 1 {
		return "KW " + s[0]
	}
	return "KW " + strings.Join(s, ", ")
}

// BuildRegularPlan reconstructs the regular plan from regularized weeks
// (see WithoutChanges). ks are the offsets of the weeks from the target week
// (0 = target week, the baseline).
//
//  1. Anchor: the target week, or the nearest week with lessons if it has
//     none (holidays).
//  2. Era: going away from the anchor, the first week that persistently
//     differs from the weeks accepted so far starts a different timetable
//     (Stundenplanwechsel); it and all weeks beyond it are ignored. So the
//     timetable of the anchor is never outvoted by an older or newer one.
//  3. A/B rhythm: some slot consistently differs between even and odd
//     weeks of the era.
//  4. Each week (A, or A and B) is composed: the target week is
//     authoritative for its lessons and its free periods are filled from
//     weeks that overlay it exactly; without target week (B week, holiday)
//     each slot gets what the majority of the weeks has there.
//
// schoolDay (may be nil) tells school days from holidays, so that a
// regularly free weekday (e.g. of a part-time teacher) counts as free and
// not as missing data.
func BuildRegularPlan(tts []*Timetable, ks []int, schoolDay func(time.Time) bool) *RegularPlan {
	plan := &RegularPlan{}
	var weeks []*planWeek
	var target *planWeek
	for i, tt := range tts {
		w := newPlanWeek(ks[i], tt, schoolDay)
		weeks = append(weeks, w)
		if w.k == 0 {
			target = w
		}
	}
	if target == nil && len(weeks) > 0 {
		target = weeks[0]
	}
	if target == nil {
		return plan
	}
	var anchor *planWeek
	for _, w := range weeks {
		if w.hasData() && (anchor == nil || nearer(w, anchor)) {
			anchor = w
		}
	}
	if anchor == nil {
		empty := *target.tt
		empty.Regular = true
		plan.Weeks = []*Timetable{&empty}
		plan.Notes = append(plan.Notes, "Kein Unterricht in den verglichenen Wochen gefunden")
		return plan
	}

	// The rhythm decides which weeks are compared when looking for plan
	// changes, and weeks of another timetable may hide the rhythm. So:
	// rhythm of all weeks, era, rhythm of the era, and if that differs,
	// the era once more with the rhythm of the era.
	findEra := func(rhythm bool) (era, other []*planWeek) {
		class := func(w *planWeek) int {
			if rhythm && odd(w.k) {
				return 1
			}
			return 0
		}
		era = []*planWeek{anchor}
		for _, dir := range []int{-1, 1} {
			var side []*planWeek // ordered away from the anchor
			for i := range weeks {
				w := weeks[i]
				if dir < 0 {
					w = weeks[len(weeks)-1-i]
				}
				if (w.k-anchor.k)*dir > 0 {
					side = append(side, w)
				}
			}
			broken := false
			for i, w := range side {
				if !broken && w.hasData() && startsNewPlan(w, side[i+1:], era, class) {
					broken = true
				}
				if broken {
					if w.hasData() {
						other = append(other, w)
					}
					continue
				}
				era = append(era, w)
			}
		}
		return era, other
	}
	rhythm := alternates(weeks)
	era, other := findEra(rhythm)
	if r := alternates(era); r != rhythm {
		era, other = findEra(r)
	}
	sortWeeks(era)
	sortWeeks(other)
	if len(other) > 0 {
		plan.Notes = append(plan.Notes, "Anderer Stundenplan (Planwechsel), nicht verwendet: "+weekList(other))
	}

	plan.Alternating = alternates(era)
	groups := [][]*planWeek{era}
	if plan.Alternating {
		var even, oddW []*planWeek
		for _, w := range era {
			if odd(w.k) {
				oddW = append(oddW, w)
			} else {
				even = append(even, w)
			}
		}
		groups = [][]*planWeek{even, oddW}
		plan.Notes = append(plan.Notes, fmt.Sprintf("Zweiwöchentlicher Rhythmus erkannt: Woche A wie KW %d, Woche B wie KW %d",
			isoWeek(target.tt.Start), isoWeek(target.tt.Start.AddDate(0, 0, 7))))
	}
	if !target.hasData() {
		plan.Notes = append(plan.Notes, fmt.Sprintf("KW %d ohne Unterricht, Regelplan aus den Nachbarwochen", target.week()))
	}

	var used, voted, mismatched, empty []*planWeek
	for gi, members := range groups {
		var base *planWeek
		if slices.Contains(members, target) && target.hasData() {
			base = target
		}
		var voters []*planWeek
		for _, w := range members {
			switch {
			case w == base:
			case !w.hasData():
				if w != target {
					empty = append(empty, w)
				}
			case base == nil || compare(base, w).matches():
				voters = append(voters, w)
			default:
				mismatched = append(mismatched, w)
			}
		}
		sort.SliceStable(voters, func(i, j int) bool { return nearer(voters[i], voters[j]) })
		tt, src, kept := compose(base, voters)
		for _, kf := range kept {
			when := "früheren"
			if kf.later {
				when = "späteren"
			}
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s frei wie in KW %d, Unterricht nur in %s Wochen (Planwechsel?)",
				dates.WeekdayShort(dates.Monday(base.tt.Start).AddDate(0, 0, kf.wd)), base.week(), when))
		}
		if base == nil {
			// Majority weeks: report the ones that were outvoted somewhere.
			res := newPlanWeek(0, tt, schoolDay)
			var kept []*planWeek
			for _, w := range voters {
				if c := compare(res, w); c.conflicts > 0 || c.presence > 0 {
					mismatched = append(mismatched, w)
				} else {
					kept = append(kept, w)
				}
			}
			voted = append(voted, kept...)
		} else {
			for _, w := range voters {
				if src[w] > 0 {
					used = append(used, w)
				}
			}
		}
		if plan.Alternating {
			tt.WeekLabel = "Woche " + string(rune('A'+gi))
		}
		plan.Weeks = append(plan.Weeks, tt)
	}
	// Free weekdays in the result can't always be told from holidays.
	var freeDays []string
	for _, tt := range plan.Weeks {
		for _, d := range tt.Days {
			if wd := weekdayIndex(d.Date); wd < 5 && len(d.Lessons) == 0 {
				label := dates.WeekdayShort(d.Date)
				if tt.WeekLabel != "" {
					label += " (" + tt.WeekLabel + ")"
				}
				freeDays = append(freeDays, label)
			}
		}
	}
	if len(freeDays) > 0 {
		plan.Notes = append(plan.Notes, "Ohne Unterricht im Regelplan: "+strings.Join(freeDays, ", "))
	}
	if schoolDay == nil {
		var unsure []string
		for _, w := range era {
			for wd := range 5 {
				if w.hasData() && w.days[wd] != nil && !w.lessons(wd) {
					unsure = append(unsure, fmt.Sprintf("%s KW %d", dates.WeekdayShort(w.days[wd].date), w.week()))
				}
			}
		}
		if len(unsure) > 0 {
			plan.Notes = append(plan.Notes, "Ferienkalender nicht verfügbar, Tage ohne Unterricht als Ferien gewertet (könnten regulär frei sein): "+strings.Join(unsure, ", "))
		}
	}
	sortWeeks(used)
	sortWeeks(voted)
	sortWeeks(mismatched)
	sortWeeks(empty)
	if len(used) > 0 {
		plan.Notes = append(plan.Notes, "Lücken ergänzt aus "+weekList(used))
	}
	if len(voted) > 0 {
		plan.Notes = append(plan.Notes, "Mehrheit je Stunde aus "+weekList(voted))
	}
	if len(mismatched) > 0 {
		plan.Notes = append(plan.Notes, "Nicht deckungsgleich, nicht verwendet: "+weekList(mismatched))
	}
	if len(empty) > 0 {
		plan.Notes = append(plan.Notes, "Ohne Unterricht (Ferien?): "+weekList(empty))
	}
	return plan
}

func sortWeeks(ws []*planWeek) {
	sort.Slice(ws, func(i, j int) bool { return ws[i].k < ws[j].k })
}

// startsNewPlan reports whether w starts a different timetable than the
// weeks of era: on a day of w, a slot holds something no era week of the
// same kind (A/B) had there, and the next week of that kind (in next) with
// lessons that day holds the same, so it is no one-off. Something new is
//   - another lesson or a free period where the era always had a lesson
//     (lesson replaced, moved away or dropped): one era week suffices;
//   - a lesson where the era always had a free period: only with at least
//     two era weeks, as a single week (the target week) may just lack the
//     lesson; then it is a gap to fill, not a plan change.
func startsNewPlan(w *planWeek, next []*planWeek, era []*planWeek, class func(*planWeek) int) bool {
	// Only days with lessons count: a whole free day may as well be an
	// unlisted holiday.
	for wd := range 7 {
		if !w.lessons(wd) {
			continue
		}
		var prev []*planWeek
		for _, e := range era {
			if class(e) == class(w) && e.lessons(wd) {
				prev = append(prev, e)
			}
		}
		var confirm *planWeek
		for _, n := range next {
			if class(n) == class(w) && n.lessons(wd) {
				confirm = n
				break
			}
		}
		if len(prev) == 0 || confirm == nil {
			continue
		}
		keys := append(w.keys(wd), confirm.keys(wd)...)
		for _, p := range prev {
			keys = append(keys, p.keys(wd)...)
		}
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			v := w.sig(wd, key)
			if confirm.sig(wd, key) != v || slices.ContainsFunc(prev, func(p *planWeek) bool { return p.sig(wd, key) == v }) {
				continue
			}
			addition := v != "" && !slices.ContainsFunc(prev, func(p *planWeek) bool { return p.sig(wd, key) != "" })
			if !addition || len(prev) >= 2 {
				return true
			}
		}
	}
	return false
}

// alternates reports whether the weeks follow an A/B rhythm: some slot
// consistently differs between even and odd weeks (relative to the target
// week), with at least two weeks of each kind showing it.
func alternates(weeks []*planWeek) bool {
	type slotID struct {
		wd  int
		key string
	}
	slotsSeen := map[slotID]bool{}
	for _, w := range weeks {
		for wd := range 7 {
			for _, key := range w.keys(wd) {
				slotsSeen[slotID{wd, key}] = true
			}
		}
	}
	for s := range slotsSeen {
		counts := [2]map[string]int{{}, {}}
		n := [2]int{}
		for _, w := range weeks {
			if !w.known(s.wd) {
				continue
			}
			p := 0
			if odd(w.k) {
				p = 1
			}
			counts[p][w.sig(s.wd, s.key)]++
			n[p]++
		}
		if n[0] < 2 || n[1] < 2 {
			continue
		}
		var maj [2]string
		consistent := true
		for p := range 2 {
			best := -1
			for v, c := range counts[p] {
				if c > best || c == best && v < maj[p] {
					maj[p], best = v, c
				}
			}
			if best*4 < n[p]*3 {
				consistent = false
			}
		}
		if consistent && maj[0] != maj[1] {
			return true
		}
	}
	return false
}

// keptFree is a free weekday of the base week kept free although other
// weeks (only earlier or only later ones) have lessons then.
type keptFree struct {
	wd    int
	later bool // the other weeks with lessons are later ones
}

// compose builds a regular week from voters (sorted nearest first).
// With a base week, base is authoritative for its lessons, and a free
// period of it gets a lesson if at least as many voters have that lesson
// as have the period free (base counting as free). Without base, each
// slot gets what most voters have there (lesson or free period), ties go to
// the nearest week. Dates are those of base or the nearest voter. It
// returns how many slots each voter contributed.
func compose(base *planWeek, voters []*planWeek) (out *Timetable, src map[*planWeek]int, kept []keptFree) {
	ref := base
	if ref == nil {
		ref = voters[0]
	}
	tt := *ref.tt
	out = &tt
	out.Regular = true
	out.Days = nil
	src = map[*planWeek]int{}
	knows := func(wd int) bool {
		return base != nil && base.lessons(wd) || slices.ContainsFunc(voters, func(f *planWeek) bool { return f.lessons(wd) })
	}
	// Every weekday Monday to Friday (and the weekend if any week has
	// lessons then), even if the API left a day out.
	monday := dates.Monday(ref.tt.Start)
	for wd := range 7 {
		if wd >= 5 && !knows(wd) {
			continue
		}
		day := monday.AddDate(0, 0, wd)
		nd := TimetableDay{Date: day}
		if d := ref.dayOf(wd); d != nil {
			nd.Date, nd.Resource = d.Date, d.Resource
		}
		// A known free day of the base week stays free unless weeks on both
		// sides of it have lessons that day: a plan change shows on one
		// side only, an unlisted holiday has lessons before and after it.
		if base != nil && base.days[wd] != nil && base.days[wd].free {
			earlier := slices.ContainsFunc(voters, func(f *planWeek) bool { return f.k < base.k && f.lessons(wd) })
			later := slices.ContainsFunc(voters, func(f *planWeek) bool { return f.k > base.k && f.lessons(wd) })
			if !earlier || !later {
				if earlier || later {
					kept = append(kept, keptFree{wd, later})
				}
				out.Days = append(out.Days, nd)
				continue
			}
		}
		var keys []string
		if base != nil {
			keys = base.keys(wd)
		}
		for _, f := range voters {
			if f.known(wd) {
				keys = append(keys, f.keys(wd)...)
			}
		}
		slices.Sort(keys)
		var pieces []piece
		for _, key := range slices.Compact(keys) {
			if base != nil && base.sig(wd, key) != "" {
				pieces = append(pieces, base.days[wd].slots[key]...)
				continue
			}
			count := map[string]int{}
			first := map[string]*planWeek{} // nearest week with that content
			var order []string              // contents, nearest first
			vote := func(s string, f *planWeek) {
				if count[s] == 0 {
					first[s] = f
					order = append(order, s)
				}
				count[s]++
			}
			if base != nil && base.known(wd) {
				vote("", base)
			}
			for _, f := range voters {
				if f.known(wd) {
					vote(f.sig(wd, key), f)
				}
			}
			best := ""
			if base != nil {
				// Lessons only compete with each other, then with "free".
				for _, s := range order {
					if s != "" && (best == "" || count[s] > count[best]) {
						best = s
					}
				}
				if best == "" || count[best] < count[""] {
					continue
				}
			} else {
				bestN := -1
				for _, s := range order {
					if count[s] > bestN {
						best, bestN = s, count[s]
					}
				}
				if best == "" {
					continue
				}
			}
			from := first[best]
			src[from]++
			for _, p := range from.days[wd].slots[key] {
				p.l.Start, p.l.End = at(day, clock(p.l.Start)), at(day, clock(p.l.End))
				pieces = append(pieces, p)
			}
		}
		nd.Lessons = mergePieces(pieces)
		out.Days = append(out.Days, nd)
	}
	return out, src, kept
}

// mergePieces joins the pieces of consecutive periods with the same lesson
// into one lesson again (double periods).
func mergePieces(pieces []piece) []Lesson {
	sort.SliceStable(pieces, func(i, j int) bool {
		if !pieces[i].l.Start.Equal(pieces[j].l.Start) {
			return pieces[i].l.Start.Before(pieces[j].l.Start)
		}
		return lessonSig(pieces[i].l) < lessonSig(pieces[j].l)
	})
	full := func(l Lesson) string { return lessonSig(l) + "#" + l.RoomLabel() + "#" + l.ClassLabel() }
	var out []Lesson
	var lastIdx []int
	for _, p := range pieces {
		merged := false
		for i := range out {
			o := &out[i]
			if full(*o) != full(p.l) {
				continue
			}
			if o.Start.Equal(p.l.Start) && o.End.Equal(p.l.End) {
				merged = true // the same lesson again
				break
			}
			if o.End.Equal(p.l.Start) || p.idx >= 0 && lastIdx[i] >= 0 && p.idx == lastIdx[i]+1 && !p.l.Start.Before(o.End) {
				o.End = p.l.End
				lastIdx[i] = p.idx
				for _, id := range p.l.IDs {
					if !slices.Contains(o.IDs, id) {
						o.IDs = append(o.IDs, id)
					}
				}
				merged = true
				break
			}
		}
		if !merged {
			l := p.l
			l.IDs = slices.Clone(l.IDs)
			out = append(out, l)
			lastIdx = append(lastIdx, p.idx)
		}
	}
	return out
}

// Pages returns the weeks of the plan as the week of monday and, for an
// A/B rhythm, the week after it, so the dates are those of the requested
// week even if another week served as template.
func (p *RegularPlan) Pages(monday time.Time) []*Timetable {
	monday = dates.Monday(monday)
	var out []*Timetable
	for i, w := range p.Weeks {
		start := monday.AddDate(0, 0, 7*i)
		pg := p.Project(start, start.AddDate(0, 0, 4))
		for _, d := range w.Days {
			if wd := weekdayIndex(d.Date); wd >= 5 {
				pg.End = start.AddDate(0, 0, wd)
			}
		}
		if pg.End.After(start.AddDate(0, 0, 4)) {
			pg = p.Project(start, pg.End)
		}
		pg.WeekLabel = w.WeekLabel
		out = append(out, pg)
	}
	return out
}

// Project maps the plan onto the days start..end: every day gets the
// lessons of its weekday in the A or B week.
func (p *RegularPlan) Project(start, end time.Time) *Timetable {
	if len(p.Weeks) == 0 {
		return &Timetable{Start: start, End: end, Regular: true}
	}
	w0 := p.Weeks[0]
	out := *w0
	out.Start, out.End, out.Days, out.WeekLabel = start, end, nil, ""
	m0 := dates.Monday(w0.Start)
	for d := dates.Day(start); !d.After(dates.Day(end)); d = d.AddDate(0, 0, 1) {
		w := w0
		if len(p.Weeks) > 1 {
			diff := int(math.Round(dates.Monday(d).Sub(m0).Hours() / 24 / 7))
			if odd(diff) {
				w = p.Weeks[1]
			}
		}
		nd := TimetableDay{Date: d}
		for _, src := range w.Days {
			if weekdayIndex(src.Date) != weekdayIndex(d) {
				continue
			}
			nd.Resource = src.Resource
			for _, l := range src.Lessons {
				l.Start, l.End = at(d, clock(l.Start)), at(d, clock(l.End))
				nd.Lessons = append(nd.Lessons, l)
			}
		}
		out.Days = append(out.Days, nd)
	}
	return &out
}
