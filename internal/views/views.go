// Package views builds Markdown documents and iCalendar feeds from the
// normalized WebUntis data.
package views

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/dates"
	md "github.com/dgrieser/web-untis-cli/internal/render"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

// ---------------------------------------------------------------- today

// TodayData is everything shown by `webuntis today`.
type TodayData struct {
	Date          time.Time                `json:"date" yaml:"date"`
	School        string                   `json:"school" yaml:"school"`
	User          string                   `json:"user" yaml:"user"`
	LastLogin     string                   `json:"lastLogin,omitempty" yaml:"lastLogin,omitempty"`
	LastImport    *time.Time               `json:"lastImport,omitempty" yaml:"lastImport,omitempty"`
	Unread        webuntis.UnreadCounts    `json:"unread" yaml:"unread"`
	News          *webuntis.News           `json:"news" yaml:"news"`
	Cards         []webuntis.DashboardCard `json:"cards,omitempty" yaml:"cards,omitempty"`
	Student       *webuntis.Student        `json:"student,omitempty" yaml:"student,omitempty"`
	Lessons       []webuntis.Lesson        `json:"lessons,omitempty" yaml:"lessons,omitempty"`
	HomeworkDue   []webuntis.Homework      `json:"homeworkDue,omitempty" yaml:"homeworkDue,omitempty"`
	UpcomingExams []webuntis.Exam          `json:"upcomingExams,omitempty" yaml:"upcomingExams,omitempty"`
	Agenda        bool                     `json:"-" yaml:"-"`
	OnlyNew       bool                     `json:"-" yaml:"-"`
}

// Today renders the "Heute" page.
func Today(t TodayData, full bool) string {
	var d md.Doc
	d.H(1, "Heute · %s", dates.WeekdayLong(t.Date)+", "+t.Date.Format("02.01.2006"))
	var meta []string
	if t.School != "" {
		meta = append(meta, "🏫 "+md.Esc(t.School))
	}
	if t.User != "" {
		meta = append(meta, "👤 "+md.Esc(t.User))
	}
	if t.LastLogin != "" {
		if lt, err := dates.ParseLocalDateTime(t.LastLogin); err == nil {
			meta = append(meta, "Letzte Anmeldung: "+dates.WeekdayLong(lt)+", "+lt.Format("02.01.2006 15:04"))
		}
	}
	if t.LastImport != nil {
		meta = append(meta, "Letzte Planaktualisierung: "+dates.WeekdayLong(*t.LastImport)+", "+t.LastImport.Format("02.01.2006 15:04"))
	}
	if t.Unread.Messages > 0 {
		meta = append(meta, fmt.Sprintf("📬 **%d ungelesene Mitteilung(en)** → `webuntis messages`", t.Unread.Messages))
	}
	d.Bullets(meta...)

	if t.News != nil {
		if sm := systemMessage(t.News.SystemMessage); sm != "" {
			d.H(2, "⚠️ Systemnachricht")
			d.P(md.Body(sm))
		}
		d.H(2, "📰 Nachrichten")
		if len(t.News.MessagesOfDay) == 0 {
			if t.OnlyNew {
				d.Empty("Keine neuen Nachrichten.")
			} else {
				d.Empty("Keine Nachrichten für heute.")
			}
		}
		unread := map[int]bool{}
		for _, c := range t.Cards {
			if strings.EqualFold(c.Status, "UNREAD") {
				unread[c.ID] = true
			}
		}
		for _, m := range t.News.MessagesOfDay {
			title := md.Esc(m.Subject)
			if m.New {
				title = "🆕 " + title
			}
			if unread[m.ID] {
				title += " 🔵"
			}
			d.H(3, "%s", title)
			if full {
				d.P(md.Body(m.Text))
			} else {
				d.P(md.Esc(md.Truncate(md.StripTags(m.Text), 220)))
			}
			for _, a := range m.Attachments {
				d.Line("- 📎 %s", link(a.Name, a.DownloadURL))
			}
			if len(m.Attachments) > 0 {
				d.Line("")
			}
		}
		if !full && len(t.News.MessagesOfDay) > 0 {
			d.P("_Volltext: `webuntis today --full` · einzelne Nachricht: `webuntis news <nr>`_")
		}
	}
	if t.Agenda {
		if t.Student != nil {
			d.H(2, "📅 Stundenplan heute · %s", md.Esc(t.Student.Name))
		} else {
			d.H(2, "📅 Stundenplan heute")
		}
		if len(t.Lessons) == 0 {
			d.Empty("Kein Unterricht.")
		} else {
			d.Table(LessonHeaders, lessonRows(t.Lessons))
		}
		d.H(2, "📚 Hausaufgaben fällig (bis morgen)")
		if len(t.HomeworkDue) == 0 {
			d.Empty("Nichts fällig.")
		} else {
			d.Table([]string{"Fällig", "Fach", "Aufgabe", "Erledigt"}, homeworkRows(t.HomeworkDue))
		}
		d.H(2, "📝 Prüfungen (nächste 14 Tage)")
		if len(t.UpcomingExams) == 0 {
			d.Empty("Keine Prüfungen.")
		} else {
			d.Table([]string{"Datum", "Zeit", "Fach", "Art", "Raum", "Info"}, examRows(t.UpcomingExams))
		}
	}
	return d.String()
}

func systemMessage(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case map[string]any:
		for _, k := range []string{"text", "message", "body", "subject"} {
			if s, ok := x[k].(string); ok && s != "" {
				return s
			}
		}
	}
	return fmt.Sprint(v)
}

// NewsItem renders a single message of the day.
func NewsItem(m webuntis.MessageOfDay) string {
	var d md.Doc
	title := md.Esc(m.Subject)
	if m.New {
		title = "🆕 " + title
	}
	d.H(1, "%s", title)
	d.P(md.Body(m.Text))
	if len(m.Attachments) > 0 {
		d.H(2, "Anhänge")
		for _, a := range m.Attachments {
			d.Line("- 📎 %s", link(a.Name, a.DownloadURL))
		}
	}
	return d.String()
}

func link(name, u string) string {
	if u == "" {
		return md.Esc(name)
	}
	return "[" + md.Esc(name) + "](" + strings.ReplaceAll(u, ")", "%29") + ")"
}

// ---------------------------------------------------------------- messages

// MessageList renders a folder.
func MessageList(folder string, msgs []webuntis.MessageSummary) string {
	var d md.Doc
	title := map[string]string{"inbox": "📥 Posteingang", "sent": "📤 Gesendet", "drafts": "📝 Entwürfe"}[folder]
	if title == "" {
		title = folder
	}
	unread := 0
	for _, m := range msgs {
		if folder == "inbox" && !m.IsMessageRead {
			unread++
		}
	}
	if unread > 0 {
		d.H(1, "%s (%d, %d ungelesen)", title, len(msgs), unread)
	} else {
		d.H(1, "%s (%d)", title, len(msgs))
	}
	if len(msgs) == 0 {
		return d.Empty("Keine Mitteilungen.").String()
	}
	var rows [][]string
	for _, m := range msgs {
		who := ""
		if folder == "inbox" && m.Sender != nil {
			who = m.Sender.DisplayName
		} else {
			var n []string
			for _, r := range m.RecipientPersons {
				n = append(n, r.DisplayName)
			}
			who = strings.Join(n, ", ")
			if who == "" && m.NumberOfRecipients > 0 {
				who = fmt.Sprintf("%d Empfänger", m.NumberOfRecipients)
			}
		}
		subj := md.Esc(md.Truncate(m.Subject, 60))
		if folder == "inbox" && !m.IsMessageRead {
			subj = "**" + subj + "** 🔵"
		}
		flags := ""
		if m.HasAttachments {
			flags += "📎"
		}
		if m.IsReply {
			flags += "↩"
		}
		if m.RequiresReadConfirmation {
			flags += "✅?"
		}
		sent := m.Sent()
		rows = append(rows, []string{strconv.Itoa(m.ID), sent.Format("02.01.06 15:04"), md.Esc(md.Truncate(who, 30)), subj, flags})
	}
	d.Table([]string{"ID", "Datum", map[bool]string{true: "Von", false: "An"}[folder == "inbox"], "Betreff", ""}, rows)
	d.P("_Anzeigen: `webuntis messages show <ID>` · Anhänge: `webuntis messages attachments <ID>`_")
	return d.String()
}

// Message renders a full message with reply history.
func Message(m *webuntis.MessageDetail) string {
	var d md.Doc
	d.H(1, "%s", md.Esc(m.Subject))
	var rcpt []string
	for _, r := range m.AllRecipients() {
		rcpt = append(rcpt, md.Esc(r.DisplayName))
	}
	from := ""
	if m.Sender != nil {
		from = md.Esc(m.Sender.DisplayName)
	}
	d.KV("Von", from, "An", strings.Join(rcpt, ", "), "Datum", dates.WeekdayShort(m.Sent())+" "+m.Sent().Format("02.01.2006 15:04"), "ID", strconv.Itoa(m.ID))
	d.Rule()
	if strings.TrimSpace(m.Content) == "" {
		d.Empty("(kein Inhalt)")
	} else {
		d.P(md.Body(m.Content))
	}
	if n := m.AttachmentCount(); n > 0 {
		d.H(2, "📎 Anhänge (%d)", n)
		for _, a := range m.StorageAttachments {
			d.Line("- %s", md.Esc(a.Name))
		}
		if m.BlobAttachment != nil {
			d.Line("- %s", md.Esc(m.BlobAttachment.Name))
		}
		for _, a := range m.Attachments {
			d.Line("- %s", link(a.Name, a.DownloadURL))
		}
		d.Line("")
		d.Pf("_Download: `webuntis messages attachments %d`_", m.ID)
	}
	if len(m.ReplyHistory) > 0 {
		d.H(2, "Verlauf")
		for _, h := range m.ReplyHistory {
			from := ""
			if h.Sender != nil {
				from = h.Sender.DisplayName
			}
			d.H(3, "%s · %s", md.Esc(from), h.Sent().Format("02.01.2006 15:04"))
			if h.IsRevoked {
				d.Empty("zurückgezogen")
				continue
			}
			d.P(md.Body(h.Content))
		}
	}
	return d.String()
}

// ---------------------------------------------------------------- homework

func homeworkRows(hw []webuntis.Homework) [][]string {
	var rows [][]string
	for _, h := range hw {
		text := md.Esc(h.Text)
		if h.Remark != "" {
			text += " _(" + md.Esc(h.Remark) + ")_"
		}
		if len(h.Attachments) > 0 {
			text += " 📎"
		}
		rows = append(rows, []string{dates.Human(h.DueDate), md.Esc(h.Subject), text, md.Check(h.Completed)})
	}
	return rows
}

// Homework renders homework grouped by due date.
func Homework(student string, from, to time.Time, hw []webuntis.Homework) string {
	var d md.Doc
	d.H(1, "📚 Hausaufgaben · %s", md.Esc(student))
	d.Pf("_%s – %s (nach Fälligkeit)_", dates.Human(from), dates.Human(to))
	if len(hw) == 0 {
		return d.Empty("Keine Hausaufgaben.").String()
	}
	var rows [][]string
	open := 0
	for _, h := range hw {
		if !h.Completed {
			open++
		}
		var text strings.Builder
		text.WriteString(md.Esc(h.Text))
		if h.Remark != "" {
			text.WriteString(" _(" + md.Esc(h.Remark) + ")_")
		}
		for _, a := range h.Attachments {
			text.WriteString(" 📎" + link(a.Name, a.URL))
		}
		due := dates.Human(h.DueDate)
		if !h.Completed && h.DueDate.Before(dates.Today()) {
			due = "**" + due + "** ⚠"
		}
		rows = append(rows, []string{due, md.Esc(h.Subject), text.String(), md.Esc(shortTeacher(h.Teacher)), dates.Short(h.Date), md.Check(h.Completed)})
	}
	d.Table([]string{"Fällig", "Fach", "Aufgabe", "Lehrkraft", "Erteilt", "Erledigt"}, rows)
	d.Pf("%d Hausaufgabe(n), %d offen.", len(hw), open)
	return d.String()
}

func shortTeacher(s string) string {
	// "Albert, Saskia (ALB)" -> "ALB"
	if i := strings.LastIndex(s, "("); i >= 0 && strings.HasSuffix(s, ")") {
		return s[i+1 : len(s)-1]
	}
	return s
}

// ---------------------------------------------------------------- absences

// Absences renders reported absences.
func Absences(student string, from, to time.Time, abs []webuntis.Absence) string {
	var d md.Doc
	d.H(1, "🏥 Abwesenheiten · %s", md.Esc(student))
	d.Pf("_%s – %s_", dates.Human(from), dates.Human(to))
	if len(abs) == 0 {
		return d.Empty("Keine Abwesenheiten.").String()
	}
	var rows [][]string
	excused := 0
	for _, a := range abs {
		if a.IsExcused {
			excused++
		}
		status := a.ExcuseStatus
		if status == "" {
			status = "offen"
		}
		if !a.IsExcused {
			status = "**" + md.Esc(status) + "**"
		} else {
			status = "✓ " + md.Esc(status)
		}
		rows = append(rows, []string{
			strconv.Itoa(a.ID), fmtRange(a.Start(), a.End()), md.Esc(a.Reason), status, md.Esc(md.Truncate(a.Text, 80)),
		})
	}
	d.Table([]string{"ID", "Zeitraum", "Grund", "Status", "Text"}, rows)
	d.Pf("%d Abwesenheit(en), davon %d entschuldigt.", len(abs), excused)
	return d.String()
}

func fmtRange(a, b time.Time) string {
	if dates.Day(a).Equal(dates.Day(b)) {
		return dates.Human(a) + " " + a.Format("15:04") + "–" + b.Format("15:04")
	}
	return dates.Human(a) + " " + a.Format("15:04") + " – " + dates.Human(b) + " " + b.Format("15:04")
}

// AbsenceTimeSummary aggregates Fehlzeiten.
type AbsenceTimeSummary struct {
	Entries         int            `json:"entries" yaml:"entries"`
	Days            int            `json:"days" yaml:"days"`
	Lessons         int            `json:"lessons" yaml:"lessons"`
	Minutes         int            `json:"minutes" yaml:"minutes"`
	ExcusedLessons  int            `json:"excusedLessons" yaml:"excusedLessons"`
	OpenLessons     int            `json:"openLessons" yaml:"openLessons"`
	CountingLessons int            `json:"countingLessons" yaml:"countingLessons"`
	BySubject       map[string]int `json:"bySubject" yaml:"bySubject"`
	ByStatus        map[string]int `json:"byStatus" yaml:"byStatus"`
}

// SummarizeAbsenceTimes computes totals.
func SummarizeAbsenceTimes(times []webuntis.AbsenceTime) AbsenceTimeSummary {
	s := AbsenceTimeSummary{BySubject: map[string]int{}, ByStatus: map[string]int{}}
	// Per entry: missedHours = number of lessons, missedMins = minutes,
	// missedDays = days counted (set on the entry that starts a new day).
	for _, t := range times {
		n := t.MissedHours
		if n == 0 && t.MissedMins > 0 {
			n = 1
		}
		s.Entries++
		s.Days += t.MissedDays
		s.Lessons += n
		s.Minutes += t.MissedMins
		if t.Excused {
			s.ExcusedLessons += n
		} else {
			s.OpenLessons += n
		}
		if t.Counting {
			s.CountingLessons += n
		}
		s.BySubject[t.SubjectName] += n
		st := t.ExcuseStatusName
		if st == "" {
			st = "offen"
		}
		s.ByStatus[st]++
	}
	return s
}

// AbsenceTimes renders missed lessons ("Fehlzeiten") with a summary.
func AbsenceTimes(student string, from, to time.Time, times []webuntis.AbsenceTime) string {
	var d md.Doc
	d.H(1, "⏱ Fehlzeiten · %s", md.Esc(student))
	d.Pf("_%s – %s_", dates.Human(from), dates.Human(to))
	if len(times) == 0 {
		return d.Empty("Keine Fehlzeiten.").String()
	}
	s := SummarizeAbsenceTimes(times)
	d.KV("Fehltage", strconv.Itoa(s.Days), "Fehlstunden", strconv.Itoa(s.Lessons),
		"Dauer", fmt.Sprintf("%dh %02dmin", s.Minutes/60, s.Minutes%60),
		"Entschuldigt", strconv.Itoa(s.ExcusedLessons),
		"Offen/unentschuldigt", strconv.Itoa(s.OpenLessons))
	var subj [][]string
	for _, k := range md.SortedKeys(s.BySubject) {
		subj = append(subj, []string{md.Esc(k), strconv.Itoa(s.BySubject[k])})
	}
	sort.SliceStable(subj, func(i, j int) bool {
		a, _ := strconv.Atoi(subj[i][1])
		b, _ := strconv.Atoi(subj[j][1])
		return a > b
	})
	d.H(2, "Nach Fach")
	d.Table([]string{"Fach", "Stunden"}, subj)
	d.H(2, "Einzelstunden")
	var rows [][]string
	for _, t := range times {
		st := md.Esc(t.ExcuseStatusName)
		if !t.Excused {
			st = "**" + st + "**"
		}
		kind := ""
		if t.IsLateness {
			kind = "Verspätung"
		}
		rows = append(rows, []string{
			dates.Human(dates.FromInt(t.Date)), dates.HM(t.StartTime) + "–" + dates.HM(t.EndTime),
			md.Esc(t.SubjectName), md.Esc(shortTeacher(t.TeacherName)), fmt.Sprintf("%d min", t.MissedMins),
			st, md.Esc(t.AbsenceReasonName), kind,
		})
	}
	d.Table([]string{"Datum", "Zeit", "Fach", "Lehrkraft", "Fehlzeit", "Status", "Grund", ""}, rows)
	return d.String()
}

// ---------------------------------------------------------------- class register

// ClassRegEvents renders class register entries.
func ClassRegEvents(student string, from, to time.Time, ev []webuntis.ClassRegEvent) string {
	var d md.Doc
	d.H(1, "📒 Klassenbucheinträge · %s", md.Esc(student))
	d.Pf("_%s – %s_", dates.Human(from), dates.Human(to))
	if len(ev) == 0 {
		return d.Empty("Keine Einträge.").String()
	}
	var rows [][]string
	for _, e := range ev {
		conf := ""
		if e.ConfirmedByUserName != "" {
			conf = "✓ " + md.Esc(e.ConfirmedByUserName)
		}
		rows = append(rows, []string{
			dates.Human(e.Created()) + " " + dates.HM(e.CreateTime), md.Esc(e.SubjectName), md.Esc(e.EventReasonName),
			md.Esc(e.CategoryName), md.Esc(e.Text), md.Esc(shortTeacher(e.CreatorName)), conf,
		})
	}
	d.Table([]string{"Datum", "Fach", "Grund", "Kategorie", "Text", "Lehrkraft", "Bestätigt"}, rows)
	d.Pf("%d Eintrag/Einträge.", len(ev))
	return d.String()
}

// ---------------------------------------------------------------- exams

func examRows(ex []webuntis.Exam) [][]string {
	var rows [][]string
	for _, e := range ex {
		rows = append(rows, []string{
			dates.Human(e.Start()), dates.HM(e.StartTime) + "–" + dates.HM(e.EndTime), md.Esc(e.Subject),
			md.Esc(e.ExamType), md.Esc(strings.Join(e.Rooms, ", ")), md.Esc(e.Text),
		})
	}
	return rows
}

// Exams renders exams.
func Exams(student string, from, to time.Time, ex []webuntis.Exam) string {
	var d md.Doc
	d.H(1, "📝 Prüfungen · %s", md.Esc(student))
	d.Pf("_%s – %s_", dates.Human(from), dates.Human(to))
	if len(ex) == 0 {
		return d.Empty("Keine Prüfungen.").String()
	}
	var rows [][]string
	today := dates.Today()
	for _, e := range ex {
		date := dates.Human(e.Start())
		if !e.Start().Before(today) && e.Start().Before(today.AddDate(0, 0, 7)) {
			date = "**" + date + "**"
		}
		rows = append(rows, []string{
			date, dates.HM(e.StartTime) + "–" + dates.HM(e.EndTime), md.Esc(e.Subject), md.Esc(e.ExamType),
			md.Esc(e.Name), md.Esc(strings.Join(e.Teachers, ", ")), md.Esc(strings.Join(e.Rooms, ", ")),
			md.Esc(e.Text), md.Esc(e.Grade),
		})
	}
	d.Table([]string{"Datum", "Zeit", "Fach", "Art", "Name", "Lehrkraft", "Raum", "Info", "Note"}, rows)
	return d.String()
}

// ---------------------------------------------------------------- generic rows

// Rows renders generic JSON objects as a table using all scalar keys.
func Rows(title, subtitle string, rows []webuntis.Row, preferred []string, empty string) string {
	var d md.Doc
	d.H(1, "%s", title)
	if subtitle != "" {
		d.Pf("_%s_", subtitle)
	}
	if len(rows) == 0 {
		return d.Empty(empty).String()
	}
	cols := columns(rows, preferred)
	var out [][]string
	for _, r := range rows {
		var line []string
		for _, c := range cols {
			line = append(line, md.Esc(FormatValue(c, lookup(r, c))))
		}
		out = append(out, line)
	}
	d.Table(cols, out)
	return d.String()
}

func lookup(r webuntis.Row, key string) any {
	cur := any(r)
	for part := range strings.SplitSeq(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func columns(rows []webuntis.Row, preferred []string) []string {
	seen := map[string]bool{}
	var cols []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			cols = append(cols, k)
		}
	}
	for _, p := range preferred {
		for _, r := range rows {
			if v := lookup(r, p); v != nil && FormatValue(p, v) != "" {
				add(p)
				break
			}
		}
	}
	for _, r := range rows {
		for _, k := range md.SortedKeys(r) {
			switch v := r[k].(type) {
			case map[string]any:
				for _, sub := range []string{"name", "label", "displayName", "longName"} {
					if s, ok := v[sub].(string); ok && s != "" {
						add(k + "." + sub)
						break
					}
				}
			case []any:
			case nil:
			default:
				if strings.EqualFold(k, "id") || strings.HasSuffix(k, "Id") {
					continue
				}
				if FormatValue(k, v) != "" {
					add(k)
				}
			}
		}
	}
	if len(cols) > 9 {
		cols = cols[:9]
	}
	return cols
}

// FormatValue formats WebUntis scalar values (int dates/times, bools).
func FormatValue(key string, v any) string {
	lk := strings.ToLower(key)
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return md.Check(x)
	case float64:
		n := int(x)
		if float64(n) == x {
			if strings.Contains(lk, "date") && n > 19000101 && n < 21000101 {
				return dates.Human(dates.FromInt(n))
			}
			if strings.Contains(lk, "time") && n >= 0 && n <= 2400 {
				return dates.HM(n)
			}
			if strings.Contains(lk, "date") && n > 1e12 {
				return time.UnixMilli(int64(n)).Format("02.01.2006 15:04")
			}
			return strconv.Itoa(n)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return x
	case []any:
		var parts []string
		for _, e := range x {
			parts = append(parts, FormatValue(key, e))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, sub := range []string{"name", "label", "displayName", "longName"} {
			if s, ok := x[sub].(string); ok {
				return s
			}
		}
	}
	return fmt.Sprint(v)
}

// ClassServices renders class duties.
func ClassServices(student string, from, to time.Time, cs *webuntis.ClassServices) string {
	return Rows("🧹 Dienste · "+md.Esc(student), dates.Human(from)+" – "+dates.Human(to), cs.ClassRoles,
		[]string{"duty.label", "duty.name", "foreName", "longName", "displayName", "klasse.name", "startDate", "endDate", "text"},
		"Keine Dienste eingetragen.")
}

// Exemptions renders exemptions.
func Exemptions(student string, from, to time.Time, rows []webuntis.Row) string {
	return Rows("🩺 Befreiungen · "+md.Esc(student), dates.Human(from)+" – "+dates.Human(to), rows,
		[]string{"startDate", "endDate", "startTime", "endTime", "subjectName", "subject", "reason", "exemptionReason", "text", "lessons"},
		"Keine Befreiungen.")
}

// OfficeHours renders contact hours.
func OfficeHours(oh *webuntis.OfficeHours, className string) string {
	title := "🗣 Sprechstunden · Woche ab " + dates.Human(oh.Week)
	if className != "" {
		title += " · " + md.Esc(className)
	}
	s := Rows(title, oh.Settings.CustomText, oh.Hours,
		[]string{"teacherName", "displayName", "name", "date", "weekDay", "startTime", "endTime", "hour", "displayNameRooms", "room", "email", "phone", "registrationPossible", "registered"},
		firstNonEmpty(oh.Settings.NoAppointmentMsg, "Keine Sprechstunden in dieser Woche."))
	if len(oh.Registrations) > 0 {
		s += "\n" + Rows("Meine Anmeldungen", "", oh.Registrations, []string{"teacherName", "date", "startTime", "endTime", "room"}, "")
	}
	return s
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// ---------------------------------------------------------------- addins

// Addins renders the platform application list.
func Addins(apps []webuntis.PlatformApp) string {
	var d md.Doc
	d.H(1, "🧩 Addins")
	if len(apps) == 0 {
		return d.Empty("Keine Addins verfügbar.").String()
	}
	var rows [][]string
	for _, a := range apps {
		target := a.WebURL
		if a.Kind == "link" {
			target = a.LinkTarget()
		}
		supported := ""
		switch {
		case strings.EqualFold(a.Name, "Klassengeld"):
			supported = "`webuntis klassengeld`"
		case a.Kind == "link":
			supported = "`webuntis addins open \"" + a.Name + "\"`"
		}
		rows = append(rows, []string{strconv.Itoa(a.ID), md.Esc(a.Name), a.Kind, target, supported})
	}
	d.Table([]string{"ID", "Name", "Typ", "URL", "CLI"}, rows)
	return d.String()
}

// Klassengeld renders the klassengeld.app overview.
func Klassengeld(kg *webuntis.Klassengeld) string {
	var d md.Doc
	d.H(1, "💶 Klassengeld")
	if len(kg.Students) == 0 {
		return d.Empty("Keine Schüler registriert.").String()
	}
	for _, s := range kg.Students {
		d.H(2, "%s · %s", md.Esc(s.Name), md.Esc(s.School))
		d.KV("Kontostand", euro(s.Balance))
		if len(s.Projects) > 0 {
			var rows [][]string
			for _, p := range s.Projects {
				status := md.Esc(p.Status)
				if strings.Contains(strings.ToLower(p.Status), "bezahlt") && !strings.Contains(strings.ToLower(p.Status), "nicht") {
					status = "✓ " + status
				}
				rows = append(rows, []string{md.Esc(p.Name), md.Esc(p.Due), euro(p.Amount), status, md.Esc(p.Info)})
			}
			d.Table([]string{"Projekt", "Frist", "Betrag", "Zahlungsanforderung", "Info"}, rows)
		}
		if len(s.Reservations) > 0 {
			d.H(3, "Reservierte Beträge")
			var rows [][]string
			for _, r := range s.Reservations {
				rows = append(rows, []string{md.Esc(r.Name), euro(r.Amount)})
			}
			d.Table([]string{"Projekt", "Betrag"}, rows)
		}
		if len(s.Transactions) > 0 {
			d.H(3, "Transaktionen")
			var rows [][]string
			for _, t := range s.Transactions {
				dir := map[string]string{"in": "⬅ Eingang", "out": "➡ Ausgang"}[t.Direction]
				rows = append(rows, []string{md.Esc(t.Date), dir, euro(t.Amount), md.Esc(t.Type), md.Esc(t.Details)})
			}
			d.Table([]string{"Datum", "", "Betrag", "Art", "Details"}, rows)
		}
	}
	return d.String()
}

func euro(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.Replace(s, ".", ",", 1)
	return s + " €"
}
