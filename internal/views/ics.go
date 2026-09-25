package views

import (
	"fmt"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"

	"github.com/dgrieser/web-untis-cli/internal/dates"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

func newCal(name string) *ics.Calendar {
	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	cal.SetProductId("-//webuntis-cli//DE")
	cal.SetName(name)
	cal.SetXWRCalName(name)
	return cal
}

func uid(parts ...any) string {
	var s []string
	for _, p := range parts {
		s = append(s, fmt.Sprint(p))
	}
	return strings.Join(s, "-") + "@webuntis-cli"
}

// TimetableICS exports lessons as events.
func TimetableICS(tt *webuntis.Timetable, school string) *ics.Calendar {
	cal := newCal("WebUntis " + firstNonEmpty(tt.Resource.LongName, tt.Resource.Label()))
	now := time.Now()
	for _, day := range tt.Days {
		for _, l := range day.Lessons {
			id := "x"
			if len(l.IDs) > 0 {
				id = fmt.Sprint(l.IDs[0])
			}
			ev := cal.AddEvent(uid("tt", school, tt.ResourceType, tt.Resource.ID, id, l.Start.Format("200601021504")))
			ev.SetDtStampTime(now)
			if l.AllDay {
				ev.SetAllDayStartAt(day.Date)
				ev.SetAllDayEndAt(day.Date.AddDate(0, 0, 1))
			} else {
				ev.SetStartAt(l.Start)
				ev.SetEndAt(l.End)
			}
			summary := l.SubjectLabel()
			if long := l.SubjectLong(); long != "" && long != summary {
				summary += " (" + long + ")"
			}
			switch {
			case l.Cancelled():
				summary = "Entfall: " + summary
				ev.SetStatus(ics.ObjectStatusCancelled)
			case l.Changed():
				summary = "Änderung: " + summary
			}
			ev.SetSummary(summary)
			if r := l.RoomLabel(); r != "" {
				ev.SetLocation(r)
			}
			var desc []string
			if t := l.TeacherLabel(); t != "" {
				desc = append(desc, "Lehrkraft: "+t)
			}
			if c := l.ClassLabel(); c != "" {
				desc = append(desc, "Klasse: "+c)
			}
			if s := statusLabel(l); s != "" {
				desc = append(desc, "Status: "+s)
			}
			if i := l.Info(); i != "" {
				desc = append(desc, i)
			}
			if len(desc) > 0 {
				ev.SetDescription(strings.Join(desc, "\n"))
			}
		}
	}
	return cal
}

// ExamsICS exports exams.
func ExamsICS(ex []webuntis.Exam, school, student string) *ics.Calendar {
	cal := newCal("Prüfungen " + student)
	now := time.Now()
	for _, e := range ex {
		ev := cal.AddEvent(uid("exam", school, e.ExamDate, e.StartTime, e.Subject, e.ID))
		ev.SetDtStampTime(now)
		ev.SetStartAt(e.Start())
		ev.SetEndAt(e.End())
		ev.SetSummary(strings.TrimSpace(e.ExamType + ": " + e.Subject))
		if len(e.Rooms) > 0 {
			ev.SetLocation(strings.Join(e.Rooms, ", "))
		}
		desc := []string{}
		if e.Name != "" {
			desc = append(desc, e.Name)
		}
		if e.Text != "" {
			desc = append(desc, e.Text)
		}
		if len(e.Teachers) > 0 {
			desc = append(desc, "Lehrkraft: "+strings.Join(e.Teachers, ", "))
		}
		if e.Grade != "" {
			desc = append(desc, "Note: "+e.Grade)
		}
		ev.SetDescription(strings.Join(desc, "\n"))
	}
	return cal
}

// HomeworkICS exports homework as all-day events on the due date.
func HomeworkICS(hw []webuntis.Homework, school, student string) *ics.Calendar {
	cal := newCal("Hausaufgaben " + student)
	now := time.Now()
	for _, h := range hw {
		ev := cal.AddEvent(uid("hw", school, h.ID))
		ev.SetDtStampTime(now)
		ev.SetAllDayStartAt(h.DueDate)
		ev.SetAllDayEndAt(h.DueDate.AddDate(0, 0, 1))
		prefix := "HA"
		if h.Completed {
			prefix = "HA ✓"
		}
		ev.SetSummary(fmt.Sprintf("%s %s: %s", prefix, h.Subject, truncate(h.Text, 60)))
		desc := h.Text
		if h.Remark != "" {
			desc += "\n\n" + h.Remark
		}
		desc += "\n\nErteilt: " + dates.Human(h.Date)
		if h.Teacher != "" {
			desc += "\nLehrkraft: " + h.Teacher
		}
		ev.SetDescription(desc)
	}
	return cal
}

// AbsencesICS exports absences.
func AbsencesICS(abs []webuntis.Absence, school, student string) *ics.Calendar {
	cal := newCal("Abwesenheiten " + student)
	now := time.Now()
	for _, a := range abs {
		ev := cal.AddEvent(uid("absence", school, a.ID))
		ev.SetDtStampTime(now)
		ev.SetStartAt(a.Start())
		ev.SetEndAt(a.End())
		s := "Abwesend"
		if a.Reason != "" {
			s += ": " + a.Reason
		}
		if a.ExcuseStatus != "" {
			s += " (" + a.ExcuseStatus + ")"
		}
		ev.SetSummary(s)
		if a.Text != "" {
			ev.SetDescription(a.Text)
		}
	}
	return cal
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
