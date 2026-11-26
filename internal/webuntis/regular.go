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
	plan := BuildRegularPlan(list, ks)
	plan.Source = source
	plan.Notes = append(notes, plan.Notes...)
	return plan, nil
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

func newPlanWeek(k int, tt *Timetable) *planWeek {
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
	return w
}

func (w *planWeek) known(wd int) bool { return w.days[wd] != nil && len(w.days[wd].slots) > 0 }

func (w *planWeek) hasData() bool {
	for wd := range w.days {
		if w.known(wd) {
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

// representative returns the week of ws (with lessons) for which ok returns
// true that overlays most of the other weeks exactly, so an anomalous week
// is never the template. The target week gets one extra point, remaining
// ties go to the nearest week.
func representative(ws []*planWeek, ok func(*planWeek) bool) *planWeek {
	var best *planWeek
	bestScore := -1
	for _, w := range ws {
		if !w.hasData() || !ok(w) {
			continue
		}
		score := 0
		for _, o := range ws {
			if o != w && o.hasData() && ok(o) && compare(w, o).matches() {
				score++
			}
		}
		if w.k == 0 {
			score++
		}
		if best == nil || score > bestScore || score == bestScore && nearer(w, best) {
			best, bestScore = w, score
		}
	}
	return best
}

func all(*planWeek) bool { return true }

func isEven(w *planWeek) bool { return !odd(w.k) }

func isOdd(w *planWeek) bool { return odd(w.k) }

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
func BuildRegularPlan(tts []*Timetable, ks []int) *RegularPlan {
	plan := &RegularPlan{}
	var weeks []*planWeek
	var target *planWeek
	for i, tt := range tts {
		w := newPlanWeek(ks[i], tt)
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
	// A/B rhythm, judged on all weeks first; it decides which weeks are
	// compared with each other when looking for plan changes.
	rhythm := alternates(weeks)
	class := func(w *planWeek) int {
		if rhythm && odd(w.k) {
			return 1
		}
		return 0
	}
	A := representative(weeks, isEven)
	B := representative(weeks, isOdd)
	if A == nil {
		A, B = B, nil
	}
	if A == nil {
		empty := *target.tt
		empty.Regular = true
		plan.Weeks = []*Timetable{&empty}
		plan.Notes = append(plan.Notes, "Kein Unterricht in den verglichenen Wochen gefunden")
		return plan
	}

	// Plan changes: going away from the target week, a week starts a
	// different timetable if (a) it and the next week fit neither the A
	// nor the B week, or (b) a slot holds something (a lesson or a free
	// period) that never occurred there in the weeks before, and the next
	// week of the same kind holds the same. (b) also catches lessons that
	// moved into a free period. The week and all weeks beyond it are
	// ignored. A single deviating week is no plan change (and no gap
	// filler, see below).
	fitsNeither := func(w *planWeek) bool {
		if w == A || w == B {
			return false
		}
		return compare(A, w).conflicts > 0 && (B == nil || compare(B, w).conflicts > 0)
	}
	var era, other []*planWeek
	for _, dir := range []int{-1, 1} {
		var side []*planWeek // ordered away from the target week
		for i := range weeks {
			w := weeks[i]
			if dir < 0 {
				w = weeks[len(weeks)-1-i]
			}
			if w.k*dir > 0 || w.k == 0 && dir < 0 {
				side = append(side, w)
			}
		}
		broken := false
		for i, w := range side {
			if !broken && w.k != 0 && w.hasData() {
				if fitsNeither(w) {
					for _, n := range side[i+1:] {
						if n.hasData() {
							broken = fitsNeither(n)
							break
						}
					}
				}
				if !broken {
					var before []*planWeek
					for _, o := range weeks {
						if !slices.Contains(side[i:], o) {
							before = append(before, o)
						}
					}
					broken = startsNewPlan(side, i, before, class)
				}
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
	sort.Slice(era, func(i, j int) bool { return era[i].k < era[j].k })
	sort.Slice(other, func(i, j int) bool { return other[i].k < other[j].k })
	if len(other) > 0 {
		plan.Notes = append(plan.Notes, "Anderer Stundenplan (Planwechsel), nicht verwendet: "+weekList(other))
	}

	type group struct {
		base    *planWeek
		members []*planWeek
	}
	var groups []group
	A, B = representative(era, isEven), representative(era, isOdd)
	plan.Alternating = A != nil && B != nil && alternates(era)
	if plan.Alternating {
		var even, oddW []*planWeek
		for _, w := range era {
			if odd(w.k) {
				oddW = append(oddW, w)
			} else {
				even = append(even, w)
			}
		}
		groups = []group{{A, even}, {B, oddW}}
		plan.Notes = append(plan.Notes, fmt.Sprintf("Zweiwöchentlicher Rhythmus erkannt: Woche A wie KW %d, Woche B wie KW %d", A.week(), B.week()))
	} else {
		groups = []group{{representative(era, all), era}}
	}
	if base := groups[0].base; base != target {
		why := "ohne Unterricht"
		if target.hasData() {
			why = "weicht von den anderen Wochen ab"
		}
		plan.Notes = append(plan.Notes, fmt.Sprintf("KW %d %s, Vorlage ist KW %d", target.week(), why, base.week()))
	}

	var used, mismatched, empty []*planWeek
	for gi, g := range groups {
		var fillers []*planWeek
		for _, w := range g.members {
			switch {
			case w == g.base:
			case !w.hasData():
				empty = append(empty, w)
			case compare(g.base, w).matches():
				fillers = append(fillers, w)
			default:
				mismatched = append(mismatched, w)
			}
		}
		sort.SliceStable(fillers, func(i, j int) bool {
			di, dj := abs(fillers[i].k-g.base.k), abs(fillers[j].k-g.base.k)
			return di < dj || di == dj && fillers[i].k > fillers[j].k
		})
		tt, src := fill(g.base, fillers)
		for _, w := range fillers {
			if src[w] > 0 {
				used = append(used, w)
			}
		}
		if plan.Alternating {
			tt.WeekLabel = "Woche " + string(rune('A'+gi))
		}
		plan.Weeks = append(plan.Weeks, tt)
	}
	sortWeeks := func(ws []*planWeek) {
		sort.Slice(ws, func(i, j int) bool { return ws[i].k < ws[j].k })
	}
	sortWeeks(used)
	sortWeeks(mismatched)
	sortWeeks(empty)
	if len(used) > 0 {
		plan.Notes = append(plan.Notes, "Lücken ergänzt aus "+weekList(used))
	}
	if len(mismatched) > 0 {
		plan.Notes = append(plan.Notes, "Nicht deckungsgleich, nicht verwendet: "+weekList(mismatched))
	}
	if len(empty) > 0 {
		plan.Notes = append(plan.Notes, "Ohne Unterricht (Ferien?): "+weekList(empty))
	}
	return plan
}

// startsNewPlan reports whether side[i] starts a different timetable: on a
// day of side[i], some slot holds something that no week of the same kind
// in before had (at least two of them have lessons that day), and the next
// week of the same kind in side with lessons that day holds the same.
func startsNewPlan(side []*planWeek, i int, before []*planWeek, class func(*planWeek) int) bool {
	w := side[i]
	for wd := range 7 {
		if !w.known(wd) {
			continue
		}
		var prev []*planWeek
		for _, b := range before {
			if class(b) == class(w) && b.known(wd) {
				prev = append(prev, b)
			}
		}
		var next *planWeek
		for _, n := range side[i+1:] {
			if class(n) == class(w) && n.known(wd) {
				next = n
				break
			}
		}
		if len(prev) < 2 || next == nil {
			continue
		}
		keys := append(w.keys(wd), next.keys(wd)...)
		for _, p := range prev {
			keys = append(keys, p.keys(wd)...)
		}
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			v := w.sig(wd, key)
			if next.sig(wd, key) != v {
				continue
			}
			if !slices.ContainsFunc(prev, func(p *planWeek) bool { return p.sig(wd, key) == v }) {
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

// fill builds the regular week from base, filling free slots from the
// fillers (nearest first): a slot is taken if at least as many weeks have
// the lesson as have the slot free. It returns how many slots each filler
// contributed.
func fill(base *planWeek, fillers []*planWeek) (*Timetable, map[*planWeek]int) {
	out := *base.tt
	out.Regular = true
	out.Days = nil
	src := map[*planWeek]int{}
	// Every weekday Monday to Friday (and the weekend if any week has
	// lessons then), even if the API left a day out.
	monday := dates.Monday(base.tt.Start)
	for wd := range 7 {
		day := monday.AddDate(0, 0, wd)
		nd := TimetableDay{Date: day}
		if d := base.dayOf(wd); d != nil {
			nd.Date, nd.Resource = d.Date, d.Resource
		}
		if wd >= 5 && !base.known(wd) && !slices.ContainsFunc(fillers, func(f *planWeek) bool { return f.known(wd) }) {
			continue
		}
		keys := base.keys(wd)
		for _, f := range fillers {
			if f.known(wd) {
				keys = append(keys, f.keys(wd)...)
			}
		}
		slices.Sort(keys)
		var pieces []piece
		for _, key := range slices.Compact(keys) {
			if base.sig(wd, key) != "" {
				pieces = append(pieces, base.days[wd].slots[key]...)
				continue
			}
			free := 0
			if base.known(wd) {
				free = 1
			}
			count := map[string]int{}
			first := map[string]*planWeek{} // nearest week with that content
			var order []string
			for _, f := range fillers {
				if !f.known(wd) {
					continue
				}
				s := f.sig(wd, key)
				if s == "" {
					free++
					continue
				}
				if count[s] == 0 {
					first[s] = f
					order = append(order, s)
				}
				count[s]++
			}
			best := ""
			for _, s := range order {
				if best == "" || count[s] > count[best] {
					best = s
				}
			}
			if best == "" || count[best] < free {
				continue
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
	return &out, src
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
