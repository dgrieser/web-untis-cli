package views

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/render"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

func at(d, hm string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", d+" "+hm, time.Local)
	return t
}

func sampleTimetable() *webuntis.Timetable {
	mon := at("2026-09-21", "00:00")
	moved := at("2026-09-21", "12:20")
	return &webuntis.Timetable{
		ResourceType: "STUDENT",
		Resource:     webuntis.Resource{ID: 8685, ShortName: "KidAlp", LongName: "Kid Alpha"},
		Start:        mon, End: mon.AddDate(0, 0, 4),
		TimeGrid: []webuntis.TimeUnit{{UnitOfDay: 1, StartTime: 745, EndTime: 830}, {UnitOfDay: 2, StartTime: 835, EndTime: 920}, {UnitOfDay: 3, StartTime: 940, EndTime: 1025}},
		Days: []webuntis.TimetableDay{
			{Date: mon, Lessons: []webuntis.Lesson{
				{IDs: []int{1}, Start: at("2026-09-21", "07:45"), End: at("2026-09-21", "09:20"), Status: "REGULAR",
					Subjects: []webuntis.Element{{Name: "D", LongName: "DEUTSCH SEK.I"}}, Teachers: []webuntis.Element{{Name: "LER"}}, Rooms: []webuntis.Element{{Name: "D104"}}},
				{IDs: []int{2}, Start: at("2026-09-21", "09:40"), End: at("2026-09-21", "10:25"), Status: "ADDITIONAL", StatusDetail: "MOVED", MovedFrom: &moved,
					Subjects: []webuntis.Element{{Name: "E"}}, Teachers: []webuntis.Element{{Name: "LER", Removed: "POD"}}},
				{IDs: []int{3}, Start: at("2026-09-21", "09:40"), End: at("2026-09-21", "10:25"), Status: "CANCELLED",
					Subjects: []webuntis.Element{{Name: "M"}}},
			}},
			{Date: mon.AddDate(0, 0, 1), Lessons: []webuntis.Lesson{
				{IDs: []int{4}, Start: at("2026-09-22", "08:35"), End: at("2026-09-22", "09:20"), Status: "REGULAR", Subjects: []webuntis.Element{{Name: "SP"}}},
			}},
		},
	}
}

func TestTimetableViews(t *testing.T) {
	tt := sampleTimetable()
	grid := TimetableGridMD(tt, "")
	for _, want := range []string{"| Std. |", "D D104 LER", "**E LER (statt POD)**", "~~M~~", "SP"} {
		if !strings.Contains(grid, want) {
			t.Errorf("grid missing %q:\n%s", want, grid)
		}
	}
	list := TimetableList(tt, "")
	if !strings.Contains(list, "❌ Entfall") || !strings.Contains(list, "↪ verlegt") {
		t.Errorf("list:\n%s", list)
	}
	pretty := TimetableGridPretty(tt, "", false, 100)
	if !strings.Contains(pretty, "✗M") || !strings.Contains(pretty, "E*") {
		t.Errorf("pretty:\n%s", pretty)
	}
	cal := TimetableICS(tt, "school").Serialize()
	if strings.Count(cal, "BEGIN:VEVENT") != 4 || !strings.Contains(cal, "STATUS:CANCELLED") {
		t.Errorf("ics:\n%s", cal)
	}
	if os.Getenv("SHOW") != "" {
		t.Log("\n" + pretty)
		t.Log("\n" + TimetableGridPretty(tt, "", true, 100))
		r := &render.Renderer{Format: render.Markdown, Out: os.Stdout}
		_ = r.Markdown(grid)
		_ = r.Markdown(list)
	}
}

func TestRowsGeneric(t *testing.T) {
	rows := []webuntis.Row{
		{"id": 1.0, "startDate": 20260921.0, "startTime": 745.0, "duty": map[string]any{"label": "Tafeldienst"}, "text": "a|b", "active": true},
	}
	out := Rows("T", "", rows, []string{"duty.label", "startDate", "startTime"}, "none")
	for _, want := range []string{"duty.label", "Tafeldienst", "Mo 21.09.2026", "07:45", `a\|b`, "✓"} {
		if !strings.Contains(out, want) {
			t.Errorf("rows missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| id |") {
		t.Errorf("id column should be skipped:\n%s", out)
	}
}

func TestTodayAndMessage(t *testing.T) {
	news := &webuntis.News{MessagesOfDay: []webuntis.MessageOfDay{{ID: 1, Subject: "Hallo *Welt*", Text: "<font>Zeile 1<br/>Zeile <b>2</b></font>",
		Attachments: []webuntis.NewsAttachment{{Name: "Bild.png", DownloadURL: "https://x/y"}}}}}
	out := Today(TodayData{Date: at("2026-09-24", "00:00"), News: news, Unread: webuntis.UnreadCounts{Messages: 2}}, true)
	for _, want := range []string{"Donnerstag, 24.09.2026", `Hallo \*Welt\*`, "**2**", "[Bild.png](https://x/y)", "2 ungelesene"} {
		if !strings.Contains(out, want) {
			t.Errorf("today missing %q:\n%s", want, out)
		}
	}
	m := &webuntis.MessageDetail{ID: 5, Subject: "S", Content: "Liebe Eltern,\n\n> zitat\nGruß", SentDateTime: "2026-07-21T17:11:00",
		Sender: &webuntis.MessagePerson{DisplayName: "Teacher, T (TT)"}, StorageAttachments: []webuntis.StorageAttachment{{ID: "x", Name: "a.pdf"}}}
	mo := Message(m)
	if !strings.Contains(mo, "Anhänge (1)") || !strings.Contains(mo, "> zitat") {
		t.Errorf("message:\n%s", mo)
	}
}
