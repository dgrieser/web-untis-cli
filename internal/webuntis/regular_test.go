package webuntis

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

var testGrid = []TimeUnit{
	{1, 745, 830}, {2, 835, 920}, {3, 940, 1025}, {4, 1030, 1115},
	{5, 1135, 1220}, {6, 1225, 1310}, {7, 1400, 1445}, {8, 1445, 1530},
}

// week builds a regularized week k weeks after 2026-11-23. spec maps the
// weekday (0 = Monday) to "unit:SUBJECT" entries; a missing weekday is a day
// without lessons (holiday).
func week(k int, spec map[int][]string) *Timetable {
	mon := time.Date(2026, 11, 23, 0, 0, 0, 0, time.Local).AddDate(0, 0, 7*k)
	tt := &Timetable{ResourceType: "STUDENT", Start: mon, End: mon.AddDate(0, 0, 4), TimeGrid: testGrid, Regular: true}
	for wd := range 5 {
		d := mon.AddDate(0, 0, wd)
		day := TimetableDay{Date: d}
		for _, e := range spec[wd] {
			var u int
			var subj string
			_, _ = fmt.Sscanf(e, "%d:%s", &u, &subj)
			g := testGrid[u-1]
			day.Lessons = append(day.Lessons, Lesson{IDs: []int{k*1000 + wd*10 + u}, Status: "REGULAR",
				Start: hhmm(d, g.StartTime), End: hhmm(d, g.EndTime),
				Subjects: []Element{{Name: subj}}, Teachers: []Element{{Name: "T" + subj}}, Rooms: []Element{{Name: "R1"}}})
		}
		tt.Days = append(tt.Days, day)
	}
	return tt
}

func full(extra map[int][]string) map[int][]string {
	m := map[int][]string{
		0: {"1:D", "2:D", "3:M", "4:E"},
		1: {"1:M", "2:BI", "3:E", "4:SP", "5:SP"},
		2: {"1:E", "2:D", "3:GE", "4:M"},
		3: {"1:KU", "2:KU", "3:M", "4:D"},
		4: {"1:MU", "2:E", "3:D", "4:PH"},
	}
	for wd, ls := range extra {
		m[wd] = append(append([]string{}, m[wd]...), ls...)
	}
	return m
}

func without(m map[int][]string, wd int, drop ...string) map[int][]string {
	out := map[int][]string{}
	maps.Copy(out, m)
	if drop == nil {
		delete(out, wd)
		return out
	}
	var keep []string
	for _, e := range m[wd] {
		if !slices.Contains(drop, e) {
			keep = append(keep, e)
		}
	}
	out[wd] = keep
	return out
}

// dayString renders a day as "07:45-09:20 D, 09:40-10:25 M".
func dayString(tt *Timetable, wd int) string {
	var s []string
	for _, l := range tt.Days[wd].Lessons {
		s = append(s, l.Start.Format("15:04")+"-"+l.End.Format("15:04")+" "+l.SubjectLabel())
	}
	return strings.Join(s, ", ")
}

func build(weeks map[int]*Timetable) *RegularPlan {
	var tts []*Timetable
	var ks []int
	for k := -4; k <= 4; k++ {
		if w, ok := weeks[k]; ok {
			tts = append(tts, w)
			ks = append(ks, k)
		}
	}
	return BuildRegularPlan(tts, ks, nil)
}

func TestRegularPlanFillsGaps(t *testing.T) {
	afternoon := full(map[int][]string{3: {"7:AG", "8:AG"}})
	// Baseline: Tuesday is a holiday, Thursday's AG is missing, Monday 3rd
	// period has no entry.
	base := without(without(afternoon, 1), 3, "7:AG", "8:AG")
	base = without(base, 0, "3:M")
	plan := build(map[int]*Timetable{
		-2: week(-2, afternoon),
		-1: week(-1, without(afternoon, 4)), // Friday off
		0:  week(0, base),
		1:  week(1, afternoon),
		2:  week(2, map[int][]string{}), // holidays
	})
	if plan.Alternating || len(plan.Weeks) != 1 {
		t.Fatalf("unexpected A/B rhythm: %+v", plan.Notes)
	}
	w := plan.Weeks[0]
	if !w.Regular || w.Start.Day() != 23 {
		t.Errorf("baseline week not kept: %v", w.Start)
	}
	if got := dayString(w, 0); got != "07:45-09:20 D, 09:40-10:25 M, 10:30-11:15 E" {
		t.Errorf("Monday gap not filled: %s", got)
	}
	if got := dayString(w, 1); got != "07:45-08:30 M, 08:35-09:20 BI, 09:40-10:25 E, 10:30-12:20 SP" {
		t.Errorf("holiday not filled: %s", got)
	}
	if got := dayString(w, 3); !strings.HasSuffix(got, "14:00-15:30 AG") {
		t.Errorf("afternoon lesson not filled: %s", got)
	}
	if d := w.Days[1].Date; d.Day() != 24 || w.Days[1].Lessons[0].Start.Day() != 24 {
		t.Errorf("filled lessons not moved to the baseline week: %v", w.Days[1].Lessons[0].Start)
	}
	notes := strings.Join(plan.Notes, "\n")
	if !strings.Contains(notes, "Lücken ergänzt") || !strings.Contains(notes, "Ohne Unterricht (Ferien?): KW 50") {
		t.Errorf("notes: %s", notes)
	}
}

func TestRegularPlanAlternating(t *testing.T) {
	a := full(map[int][]string{1: {"7:INF", "8:INF"}})
	b := without(full(nil), 2, "3:GE")
	b[2] = append(b[2], "3:EK")
	plan := build(map[int]*Timetable{
		-3: week(-3, b),
		-2: week(-2, a),
		-1: week(-1, b),
		0:  week(0, without(a, 0)), // Monday holiday
		1:  week(1, without(b, 4, "4:PH")),
		2:  week(2, a),
	})
	if !plan.Alternating || len(plan.Weeks) != 2 {
		t.Fatalf("A/B rhythm not detected: %+v", plan.Notes)
	}
	A, B := plan.Weeks[0], plan.Weeks[1]
	if A.WeekLabel != "Woche A" || B.WeekLabel != "Woche B" || B.Start.Day() != 30 {
		t.Errorf("labels/weeks: %q %q %v", A.WeekLabel, B.WeekLabel, B.Start)
	}
	if got := dayString(A, 1); !strings.HasSuffix(got, "14:00-15:30 INF") {
		t.Errorf("A week: %s", got)
	}
	if got := dayString(B, 1); strings.Contains(got, "INF") {
		t.Errorf("B lesson leaked into A: %s", got)
	}
	if got := dayString(A, 0); got != "07:45-09:20 D, 09:40-10:25 M, 10:30-11:15 E" {
		t.Errorf("A Monday not filled from A weeks: %s", got)
	}
	if got := dayString(B, 2); !strings.Contains(got, "EK") || strings.Contains(got, "GE") {
		t.Errorf("B Wednesday: %s", got)
	}
	if got := dayString(B, 4); !strings.Contains(got, "PH") {
		t.Errorf("B Friday gap not filled: %s", got)
	}
	// Projection follows the rhythm.
	p := plan.Project(time.Date(2026, 12, 7, 0, 0, 0, 0, time.Local), time.Date(2026, 12, 18, 0, 0, 0, 0, time.Local))
	if len(p.Days) != 12 || dayString(p, 1) != dayString(A, 1) || !strings.Contains(dayString(p, 9), "EK") {
		t.Errorf("projection: %s / %s", dayString(p, 1), dayString(p, 9))
	}
}

func TestRegularPlanIgnoresOtherPlans(t *testing.T) {
	old := full(nil)
	// Different timetable from week +2 on (Stundenplanwechsel); week -1 has
	// a lesson that doesn't overlay the baseline.
	changed := without(full(map[int][]string{0: {"3:CH"}}), 0, "3:M")
	changed = without(changed, 4, "1:MU")
	changed[4] = append(changed[4], "1:REL")
	odd := without(old, 2, "1:E")
	odd[2] = append(odd[2], "1:KU")
	base := without(old, 4, "1:MU") // gap the other plans must not fill
	plan := build(map[int]*Timetable{
		-2: week(-2, without(old, 4, "1:MU")),
		-1: week(-1, odd),
		0:  week(0, base),
		1:  week(1, without(old, 4, "1:MU")),
		2:  week(2, changed),
		3:  week(3, changed),
		4:  week(4, changed),
	})
	if plan.Alternating {
		t.Fatalf("plan change taken as A/B rhythm: %v", plan.Notes)
	}
	w := plan.Weeks[0]
	if got := dayString(w, 0); strings.Contains(got, "CH") {
		t.Errorf("lesson of the new plan used: %s", got)
	}
	if got := dayString(w, 4); strings.Contains(got, "REL") || strings.Contains(got, "MU") {
		t.Errorf("Friday filled from a non-matching week: %s", got)
	}
	notes := strings.Join(plan.Notes, "\n")
	if !strings.Contains(notes, "Planwechsel") || !strings.Contains(notes, "KW 50, 51, 52") {
		t.Errorf("plan change not reported: %s", notes)
	}
}

func TestMergePieces(t *testing.T) {
	plan := build(map[int]*Timetable{0: week(0, map[int][]string{0: {"1:D", "2:D", "4:D", "5:D"}})})
	if got := dayString(plan.Weeks[0], 0); got != "07:45-09:20 D, 10:30-12:20 D" {
		t.Errorf("got %s", got)
	}
}

func TestRegularPlanBaseTimetable(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	mon := time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local)
	plan, err := c.RegularPlan(context.Background(), TimetableQuery{ResourceType: "STUDENT", ResourceID: 8685, TimetableType: "MY_TIMETABLE",
		Start: mon, End: mon.AddDate(0, 0, 4)}, RegularOptions{WeeksBefore: 2, WeeksAfter: 2})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceBase || len(plan.Weeks) != 1 {
		t.Fatalf("source %s, %d weeks, notes %v", plan.Source, len(plan.Weeks), plan.Notes)
	}
	w := plan.Weeks[0]
	if got := dayString(w, 0); got != "07:45-09:20 D, 09:40-10:25 M" {
		t.Errorf("Monday: %s", got)
	}
	l := w.Days[0].Lessons[0]
	if l.TeacherLabel() != "LER" || l.SubjectLong() != "DEUTSCH SEK.I" || w.Resource.ID != 8685 {
		t.Errorf("substitution not reverted or subject info missing: %+v", l)
	}
	if got := dayString(w, 1); got != "07:45-08:30 E" {
		t.Errorf("holiday Tuesday not filled from other weeks: %s", got)
	}
}

// Review finding: an anomalous week next to the target week must not become
// the template of the B week.
func TestRegularPlanAnomalousNearestOddWeek(t *testing.T) {
	a := full(map[int][]string{1: {"7:INF", "8:INF"}})
	b := without(full(nil), 2, "3:GE")
	b[2] = append(b[2], "3:EK")
	c := without(b, 2, "3:EK")
	c[2] = append(c[2], "3:CH")
	plan := build(map[int]*Timetable{
		-3: week(-3, b), -2: week(-2, a), -1: week(-1, b), 0: week(0, a),
		1: week(1, c), 2: week(2, a), 3: week(3, b), 4: week(4, a),
	})
	if !plan.Alternating || len(plan.Weeks) != 2 {
		t.Fatalf("A/B rhythm not detected: %v", plan.Notes)
	}
	B := plan.Weeks[1]
	if got := dayString(B, 2); !strings.Contains(got, "EK") || strings.Contains(got, "CH") {
		t.Errorf("B week taken from the anomalous week: %s (%v)", got, plan.Notes)
	}
	if !strings.Contains(strings.Join(plan.Notes, "\n"), "Nicht deckungsgleich, nicht verwendet: KW 49") {
		t.Errorf("anomalous week not reported: %v", plan.Notes)
	}
}

// Review finding: a lesson that moves into a free period is a plan change,
// the result must not contain it twice (old and new period).
func TestRegularPlanLessonMovedIntoFreePeriod(t *testing.T) {
	old := full(nil)
	moved := without(full(map[int][]string{0: {"7:M"}}), 0, "3:M")
	plan := build(map[int]*Timetable{
		-1: week(-1, old), 0: week(0, old),
		1: week(1, moved), 2: week(2, moved), 3: week(3, moved), 4: week(4, moved),
	})
	if got := dayString(plan.Weeks[0], 0); got != "07:45-09:20 D, 09:40-10:25 M, 10:30-11:15 E" {
		t.Errorf("hybrid of old and new plan: %s", got)
	}
	if notes := strings.Join(plan.Notes, "\n"); !strings.Contains(notes, "Planwechsel), nicht verwendet: KW 49, 50, 51, 52") {
		t.Errorf("plan change not reported: %s", notes)
	}
}

// Review finding: parts of a lesson outside the time grid must not be cut
// off, even without other weeks.
func TestRegularPlanKeepsTimesOutsideGrid(t *testing.T) {
	tt := week(0, map[int][]string{0: {"1:D", "2:D"}})
	d := tt.Days[0].Date
	tt.Days[0].Lessons = append(tt.Days[0].Lessons,
		Lesson{IDs: []int{90}, Start: hhmm(d, 1300), End: hhmm(d, 1350), Subjects: []Element{{Name: "AG"}}},
		Lesson{IDs: []int{91}, Start: hhmm(d, 1530), End: hhmm(d, 1615), Subjects: []Element{{Name: "CH"}}})
	tt.Days[0].Lessons[0].Start = hhmm(d, 740)
	plan := BuildRegularPlan([]*Timetable{tt}, []int{0}, nil)
	if got := dayString(plan.Weeks[0], 0); got != "07:40-08:30 D, 08:35-09:20 D, 13:00-13:50 AG, 15:30-16:15 CH" &&
		got != "07:40-09:20 D, 13:00-13:50 AG, 15:30-16:15 CH" {
		t.Errorf("lesson times changed: %s", got)
	}
}

// Review finding: a weekday the API left out entirely is filled too.
func TestRegularPlanMissingWeekday(t *testing.T) {
	base := week(0, full(nil))
	base.Days = append(base.Days[:1:1], base.Days[2:]...) // no Tuesday at all
	plan := BuildRegularPlan([]*Timetable{week(-1, full(nil)), base, week(1, full(nil))}, []int{-1, 0, 1}, nil)
	w := plan.Weeks[0]
	if len(w.Days) != 5 || w.Days[1].Date.Day() != 24 {
		t.Fatalf("days: %d", len(w.Days))
	}
	if got := dayString(w, 1); got != "07:45-08:30 M, 08:35-09:20 BI, 09:40-10:25 E, 10:30-12:20 SP" {
		t.Errorf("Tuesday not reconstructed: %s", got)
	}
}

// Review finding: pages (and the calendar export) carry the dates of the
// requested week, also if another week was the template.
func TestRegularPlanPagesAnchoredAtRequestedWeek(t *testing.T) {
	plan := build(map[int]*Timetable{0: week(0, map[int][]string{}), 2: week(2, full(nil)), 4: week(4, full(nil))})
	if len(plan.Weeks) != 1 || plan.Weeks[0].Start.Day() != 7 {
		t.Fatalf("template: %+v", plan.Weeks)
	}
	mon := time.Date(2026, 11, 23, 0, 0, 0, 0, time.Local)
	pages := plan.Pages(mon)
	if len(pages) != 1 || !pages[0].Start.Equal(mon) || !pages[0].Days[0].Date.Equal(mon) || pages[0].Days[0].Lessons[0].Start.Day() != 23 {
		t.Errorf("page not dated like the requested week: %v / %v", pages[0].Start, pages[0].Days[0].Lessons[0].Start)
	}
	if !strings.Contains(strings.Join(plan.Notes, "\n"), "KW 48 ohne Unterricht, Regelplan aus den Nachbarwochen\nMehrheit je Stunde aus KW 50, 52") {
		t.Errorf("notes: %v", plan.Notes)
	}
}

// Review finding: a holiday week between two timetables must not panic;
// the plan of the nearest following week is used.
func TestRegularPlanHolidayBetweenPlans(t *testing.T) {
	old := full(nil)
	changed := without(full(nil), 0, "3:M")
	changed[0] = append(changed[0], "3:CH")
	plan := build(map[int]*Timetable{
		-2: week(-2, old), -1: week(-1, old), 0: week(0, nil),
		1: week(1, changed), 2: week(2, changed),
	})
	if len(plan.Weeks) != 1 {
		t.Fatalf("no regular plan: %v", plan.Notes)
	}
	if got := dayString(plan.Weeks[0], 0); !strings.Contains(got, "CH") || strings.Contains(got, " M") {
		t.Errorf("Monday: %s", got)
	}
	if notes := strings.Join(plan.Notes, "\n"); !strings.Contains(notes, "Planwechsel), nicht verwendet: KW 46, 47") {
		t.Errorf("notes: %s", notes)
	}
}

// Review finding: an A/B difference that is only a lesson more or less
// (AG in A weeks) must not leak into the B week from one anomalous B week.
func TestRegularPlanPresenceOnlyRhythm(t *testing.T) {
	a := full(map[int][]string{3: {"7:AG", "8:AG"}})
	b := full(nil)
	plan := build(map[int]*Timetable{
		-4: week(-4, a), -3: week(-3, b), -2: week(-2, a), -1: week(-1, b), 0: week(0, a),
		1: week(1, a), 2: week(2, a), 3: week(3, b), 4: week(4, a),
	})
	if !plan.Alternating || len(plan.Weeks) != 2 {
		t.Fatalf("A/B rhythm not detected: %v", plan.Notes)
	}
	if got := dayString(plan.Weeks[0], 3); !strings.Contains(got, "AG") {
		t.Errorf("A week lost the AG: %s", got)
	}
	if got := dayString(plan.Weeks[1], 3); strings.Contains(got, "AG") {
		t.Errorf("AG of the anomalous week in the B week: %s", got)
	}
	if notes := strings.Join(plan.Notes, "\n"); !strings.Contains(notes, "Nicht deckungsgleich, nicht verwendet: KW 49") {
		t.Errorf("notes: %s", notes)
	}
}

// Review finding: a lesson moved into a free period is a plan change even
// without earlier weeks.
func TestRegularPlanLessonMovedWithoutHistory(t *testing.T) {
	old := full(nil)
	moved := without(full(map[int][]string{0: {"7:M"}}), 0, "3:M")
	plan := build(map[int]*Timetable{0: week(0, old), 1: week(1, moved), 2: week(2, moved), 3: week(3, moved), 4: week(4, moved)})
	if got := dayString(plan.Weeks[0], 0); got != "07:45-09:20 D, 09:40-10:25 M, 10:30-11:15 E" {
		t.Errorf("hybrid of old and new plan: %s", got)
	}
	if notes := strings.Join(plan.Notes, "\n"); !strings.Contains(notes, "Planwechsel), nicht verwendet: KW 49, 50, 51, 52") {
		t.Errorf("notes: %s", notes)
	}
}

// Review finding: a new timetable starting in the target week is not
// outvoted by the older weeks.
func TestRegularPlanNewPlanInTargetWeek(t *testing.T) {
	old := full(nil)
	changed := without(full(nil), 0, "3:M")
	changed[0] = append(changed[0], "3:CH")
	plan := build(map[int]*Timetable{-4: week(-4, old), -3: week(-3, old), -2: week(-2, old), -1: week(-1, old), 0: week(0, changed)})
	if got := dayString(plan.Weeks[0], 0); !strings.Contains(got, "CH") || strings.Contains(got, " M") {
		t.Errorf("target week outvoted: %s", got)
	}
	if notes := strings.Join(plan.Notes, "\n"); !strings.Contains(notes, "Planwechsel), nicht verwendet: KW 44, 45, 46, 47") {
		t.Errorf("notes: %s", notes)
	}
}

// Review finding: weeks of a new timetable must not hide the A/B rhythm of
// the current one.
func TestRegularPlanRhythmBeforePlanChange(t *testing.T) {
	a := full(nil)
	b := without(full(nil), 2, "3:GE")
	b[2] = append(b[2], "3:EK")
	c := without(full(nil), 2, "3:GE")
	c[2] = append(c[2], "3:CH")
	plan := build(map[int]*Timetable{
		-4: week(-4, a), -3: week(-3, b), -2: week(-2, a), -1: week(-1, b), 0: week(0, a),
		1: week(1, c), 2: week(2, c), 3: week(3, c), 4: week(4, c),
	})
	if !plan.Alternating || len(plan.Weeks) != 2 {
		t.Fatalf("A/B rhythm lost: %v", plan.Notes)
	}
	if got := dayString(plan.Weeks[1], 2); !strings.Contains(got, "EK") || strings.Contains(got, "CH") {
		t.Errorf("B week: %s", got)
	}
	if notes := strings.Join(plan.Notes, "\n"); !strings.Contains(notes, "Planwechsel), nicht verwendet: KW 49, 50, 51, 52") {
		t.Errorf("notes: %s", notes)
	}
}

// Review finding: a regularly free weekday (Friday in B weeks, e.g. a
// part-time teacher) is free, not missing data; holidays by the calendar
// and unpublished weeks still are missing data.
func TestRegularPlanFreeWeekday(t *testing.T) {
	a := full(nil)
	b := without(full(nil), 4)
	holiday := time.Date(2026, 11, 27, 0, 0, 0, 0, time.Local) // Friday of the target week
	schoolDay := func(d time.Time) bool {
		return d.Weekday() != time.Saturday && d.Weekday() != time.Sunday && !d.Equal(holiday)
	}
	var tts []*Timetable
	var ks []int
	for k := -4; k <= 4; k++ {
		spec := a
		switch {
		case k >= 3:
			spec = map[int][]string{} // not published yet
		case k == 0:
			spec = without(a, 4)
		case odd(k):
			spec = b
		}
		tts, ks = append(tts, week(k, spec)), append(ks, k)
	}
	plan := BuildRegularPlan(tts, ks, schoolDay)
	if !plan.Alternating || len(plan.Weeks) != 2 {
		t.Fatalf("A/B rhythm of the free Friday not detected: %v", plan.Notes)
	}
	if got := dayString(plan.Weeks[0], 4); got != "07:45-08:30 MU, 08:35-09:20 E, 09:40-10:25 D, 10:30-11:15 PH" {
		t.Errorf("A Friday (holiday in the target week) not filled: %s", got)
	}
	if got := dayString(plan.Weeks[1], 4); got != "" {
		t.Errorf("B Friday should be free: %s", got)
	}
	if notes := strings.Join(plan.Notes, "\n"); !strings.Contains(notes, "Ohne Unterricht im Regelplan: Fr (Woche B)") ||
		!strings.Contains(notes, "Ohne Unterricht (Ferien?): KW 51, 52") {
		t.Errorf("notes: %s", notes)
	}
	// Without holiday calendar the free day can't be told from a holiday:
	// it counts as missing data, and the notes say so.
	plan = BuildRegularPlan(tts, ks, nil)
	notes := strings.Join(plan.Notes, "\n")
	if plan.Alternating || !strings.Contains(notes, "Ferienkalender nicht verfügbar, Tage ohne Unterricht als Ferien gewertet") ||
		!strings.Contains(notes, "Fr KW 45") || strings.Contains(notes, "Fr KW 51") {
		t.Errorf("without calendar: %v", plan.Notes)
	}
}
