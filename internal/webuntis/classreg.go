package webuntis

import (
	"context"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/dates"
)

// Row is a generic JSON object for endpoints whose shape varies between
// schools or that we have not seen populated yet.
type Row = map[string]any

func rangeQuery(from, to time.Time) url.Values {
	return url.Values{"startDate": {dates.IntStr(from)}, "endDate": {dates.IntStr(to)}}
}

func ttlFor(to time.Time, recent time.Duration) time.Duration {
	if to.Before(dates.Today().AddDate(0, 0, -7)) {
		return 12 * time.Hour
	}
	return recent
}

// ---------------------------------------------------------------- absences

// Excuse of an absence.
type Excuse struct {
	ID           int    `json:"id" yaml:"id"`
	Text         string `json:"text" yaml:"text"`
	ExcuseDate   int    `json:"excuseDate" yaml:"excuseDate"`
	ExcuseStatus string `json:"excuseStatus" yaml:"excuseStatus"`
	IsExcused    bool   `json:"isExcused" yaml:"isExcused"`
	UserID       int    `json:"userId" yaml:"userId"`
	Username     string `json:"username" yaml:"username"`
}

// Absence is a reported absence ("Meine Abwesenheiten").
type Absence struct {
	ID            int     `json:"id" yaml:"id"`
	StartDate     int     `json:"startDate" yaml:"startDate"`
	EndDate       int     `json:"endDate" yaml:"endDate"`
	StartTime     int     `json:"startTime" yaml:"startTime"`
	EndTime       int     `json:"endTime" yaml:"endTime"`
	CreateDate    int64   `json:"createDate" yaml:"createDate"`
	LastUpdate    int64   `json:"lastUpdate" yaml:"lastUpdate"`
	CreatedUser   string  `json:"createdUser" yaml:"createdUser"`
	UpdatedUser   string  `json:"updatedUser" yaml:"updatedUser"`
	ReasonID      int     `json:"reasonId" yaml:"reasonId"`
	Reason        string  `json:"reason" yaml:"reason"`
	Text          string  `json:"text" yaml:"text"`
	Interruptions []any   `json:"interruptions" yaml:"interruptions"`
	StudentName   string  `json:"studentName" yaml:"studentName"`
	ExcuseStatus  string  `json:"excuseStatus" yaml:"excuseStatus"`
	IsExcused     bool    `json:"isExcused" yaml:"isExcused"`
	Excuse        *Excuse `json:"excuse" yaml:"excuse"`
}

// Start returns the absence start.
func (a Absence) Start() time.Time { return dates.FromIntTime(a.StartDate, a.StartTime) }

// End returns the absence end.
func (a Absence) End() time.Time { return dates.FromIntTime(a.EndDate, a.EndTime) }

// Absences lists reported absences of a student.
func (c *Client) Absences(ctx context.Context, studentID int, from, to time.Time) ([]Absence, error) {
	q := rangeQuery(from, to)
	q.Set("studentId", strconv.Itoa(studentID))
	q.Set("excuseStatusId", "-1")
	var r struct {
		Data struct {
			Absences []Absence `json:"absences"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/classreg/absences/students", q, ttlFor(to, 5*time.Minute), &r); err != nil {
		return nil, err
	}
	sort.Slice(r.Data.Absences, func(i, j int) bool { return r.Data.Absences[i].Start().After(r.Data.Absences[j].Start()) })
	return r.Data.Absences, nil
}

// AbsenceTime is a missed lesson ("Fehlzeiten").
type AbsenceTime struct {
	AbsenceID         int    `json:"absenceId" yaml:"absenceId"`
	KlasseID          int    `json:"klasseId" yaml:"klasseId"`
	KlasseName        string `json:"klasseName" yaml:"klasseName"`
	SubjectID         int    `json:"subjectId" yaml:"subjectId"`
	SubjectName       string `json:"subjectName" yaml:"subjectName"`
	TeacherID         int    `json:"teacherId" yaml:"teacherId"`
	TeacherName       string `json:"teacherName" yaml:"teacherName"`
	AbsenceReasonID   int    `json:"absenceReasonId" yaml:"absenceReasonId"`
	AbsenceReasonName string `json:"absenceReasonName" yaml:"absenceReasonName"`
	ExcuseStatusID    int    `json:"excuseStatusId" yaml:"excuseStatusId"`
	ExcuseStatusName  string `json:"excuseStatusName" yaml:"excuseStatusName"`
	Excused           bool   `json:"excused" yaml:"excused"`
	Date              int    `json:"date" yaml:"date"`
	StartTime         int    `json:"startTime" yaml:"startTime"`
	EndTime           int    `json:"endTime" yaml:"endTime"`
	MissedDays        int    `json:"missedDays" yaml:"missedDays"`
	MissedHours       int    `json:"missedHours" yaml:"missedHours"`
	MissedMins        int    `json:"missedMins" yaml:"missedMins"`
	Counting          bool   `json:"counting" yaml:"counting"`
	Text              string `json:"text" yaml:"text"`
	IsLateness        bool   `json:"isLateness,omitempty" yaml:"isLateness,omitempty"`
}

// AbsenceTimes lists missed lessons of a student.
func (c *Client) AbsenceTimes(ctx context.Context, studentID int, from, to time.Time, excludeAbsences, excludeLateness bool) ([]AbsenceTime, error) {
	q := rangeQuery(from, to)
	q.Set("studentId", strconv.Itoa(studentID))
	q.Set("excuseStatusId", "-1")
	q.Set("excludeAbsences", strconv.FormatBool(excludeAbsences))
	q.Set("excludeLateness", strconv.FormatBool(excludeLateness))
	var r struct {
		Data struct {
			AbsenceTimes []AbsenceTime `json:"absenceTimes"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/classreg/absencetimes/student", q, ttlFor(to, 5*time.Minute), &r); err != nil {
		return nil, err
	}
	sort.SliceStable(r.Data.AbsenceTimes, func(i, j int) bool {
		a, b := r.Data.AbsenceTimes[i], r.Data.AbsenceTimes[j]
		if a.Date != b.Date {
			return a.Date > b.Date
		}
		return a.StartTime < b.StartTime
	})
	return r.Data.AbsenceTimes, nil
}

// ---------------------------------------------------------------- homework

// HomeworkAttachment of a homework.
type HomeworkAttachment struct {
	ID   int    `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}

// Homework is a normalized homework item.
type Homework struct {
	ID          int                  `json:"id" yaml:"id"`
	LessonID    int                  `json:"lessonId" yaml:"lessonId"`
	Date        time.Time            `json:"date" yaml:"date"`
	DueDate     time.Time            `json:"dueDate" yaml:"dueDate"`
	Subject     string               `json:"subject" yaml:"subject"`
	LessonType  string               `json:"lessonType,omitempty" yaml:"lessonType,omitempty"`
	Teacher     string               `json:"teacher,omitempty" yaml:"teacher,omitempty"`
	Text        string               `json:"text" yaml:"text"`
	Remark      string               `json:"remark,omitempty" yaml:"remark,omitempty"`
	Completed   bool                 `json:"completed" yaml:"completed"`
	Attachments []HomeworkAttachment `json:"attachments,omitempty" yaml:"attachments,omitempty"`
	StudentIDs  []int                `json:"studentIds,omitempty" yaml:"studentIds,omitempty"`
}

// Homework lists homework assigned in [from, to]. If studentID > 0 only
// homework of that student is returned.
func (c *Client) Homework(ctx context.Context, studentID int, from, to time.Time) ([]Homework, error) {
	var r struct {
		Data struct {
			Records []struct {
				HomeworkID int   `json:"homeworkId"`
				TeacherID  int   `json:"teacherId"`
				ElementIDs []int `json:"elementIds"`
			} `json:"records"`
			Homeworks []struct {
				ID          int                  `json:"id"`
				LessonID    int                  `json:"lessonId"`
				Date        int                  `json:"date"`
				DueDate     int                  `json:"dueDate"`
				Text        string               `json:"text"`
				Remark      string               `json:"remark"`
				Completed   bool                 `json:"completed"`
				Attachments []HomeworkAttachment `json:"attachments"`
			} `json:"homeworks"`
			Teachers []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"teachers"`
			Lessons []struct {
				ID         int    `json:"id"`
				Subject    string `json:"subject"`
				LessonType string `json:"lessonType"`
			} `json:"lessons"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/homeworks/lessons", rangeQuery(from, to), ttlFor(to, 5*time.Minute), &r); err != nil {
		return nil, err
	}
	teachers := map[int]string{}
	for _, t := range r.Data.Teachers {
		teachers[t.ID] = t.Name
	}
	type lesson struct{ subject, typ string }
	lessons := map[int]lesson{}
	for _, l := range r.Data.Lessons {
		lessons[l.ID] = lesson{l.Subject, l.LessonType}
	}
	type rec struct {
		teacher  int
		elements []int
	}
	records := map[int]rec{}
	for _, x := range r.Data.Records {
		records[x.HomeworkID] = rec{x.TeacherID, x.ElementIDs}
	}
	var out []Homework
	for _, h := range r.Data.Homeworks {
		rc, hasRec := records[h.ID]
		if studentID > 0 && hasRec && len(rc.elements) > 0 && !containsInt(rc.elements, studentID) {
			continue
		}
		l := lessons[h.LessonID]
		out = append(out, Homework{
			ID: h.ID, LessonID: h.LessonID,
			Date: dates.FromInt(h.Date), DueDate: dates.FromInt(h.DueDate),
			Subject: l.subject, LessonType: l.typ, Teacher: teachers[rc.teacher],
			Text: h.Text, Remark: h.Remark, Completed: h.Completed,
			Attachments: h.Attachments, StudentIDs: rc.elements,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].DueDate.Equal(out[j].DueDate) {
			return out[i].DueDate.Before(out[j].DueDate)
		}
		return out[i].Subject < out[j].Subject
	})
	return out, nil
}

func containsInt(s []int, v int) bool { return slices.Contains(s, v) }

// ---------------------------------------------------------------- class register entries

// ClassRegEvent is a class register entry ("Klassenbucheinträge").
type ClassRegEvent struct {
	ID                  int    `json:"id" yaml:"id"`
	ElementName         string `json:"elementName" yaml:"elementName"`
	SubjectName         string `json:"subjectName" yaml:"subjectName"`
	CreatorName         string `json:"creatorName" yaml:"creatorName"`
	CreateDate          int    `json:"createDate" yaml:"createDate"`
	CreateTime          int    `json:"createTime" yaml:"createTime"`
	EventReasonName     string `json:"eventReasonName" yaml:"eventReasonName"`
	CategoryName        string `json:"categoryName" yaml:"categoryName"`
	Text                string `json:"text" yaml:"text"`
	ElemType            string `json:"elemType" yaml:"elemType"`
	ConfirmedByUserName string `json:"confirmedByUserName" yaml:"confirmedByUserName"`
	ConfirmedByDateTime any    `json:"confirmedByDateTime" yaml:"confirmedByDateTime"`
	CanConfirm          bool   `json:"canConfirm" yaml:"canConfirm"`
}

// Created returns the creation time.
func (e ClassRegEvent) Created() time.Time { return dates.FromIntTime(e.CreateDate, e.CreateTime) }

// ClassRegEvents lists class register entries of a student.
func (c *Client) ClassRegEvents(ctx context.Context, studentID int, from, to time.Time) ([]ClassRegEvent, error) {
	q := rangeQuery(from, to)
	q.Set("studentId", strconv.Itoa(studentID))
	var r struct {
		Data struct {
			Rows []ClassRegEvent `json:"rows"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/classreg/classregevents", q, ttlFor(to, 10*time.Minute), &r); err != nil {
		return nil, err
	}
	sort.SliceStable(r.Data.Rows, func(i, j int) bool { return r.Data.Rows[i].Created().After(r.Data.Rows[j].Created()) })
	return r.Data.Rows, nil
}

// ---------------------------------------------------------------- class services

// ClassServices is the response of the class services ("Dienste") view.
type ClassServices struct {
	ClassRoles      []Row          `json:"classRoles" yaml:"classRoles"`
	PersonKlasseMap map[string]int `json:"personKlasseMap" yaml:"personKlasseMap"`
}

// ClassServices lists class duties of the student's class.
func (c *Client) ClassServices(ctx context.Context, studentID int, from, to time.Time) (*ClassServices, error) {
	q := rangeQuery(from, to)
	q.Set("elementId", strconv.Itoa(studentID))
	var r struct {
		Data ClassServices `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/classreg/classservices", q, ttlFor(to, 30*time.Minute), &r); err != nil {
		return nil, err
	}
	return &r.Data, nil
}

// ---------------------------------------------------------------- exams

// ExamStudent is a student assigned to an exam.
type ExamStudent struct {
	ID          int    `json:"id" yaml:"id"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	Klasse      struct {
		ID   int    `json:"id" yaml:"id"`
		Name string `json:"name" yaml:"name"`
	} `json:"klasse" yaml:"klasse"`
	GradeProtection          bool `json:"gradeProtection" yaml:"gradeProtection"`
	DisadvantageCompensation bool `json:"disadvantageCompensation" yaml:"disadvantageCompensation"`
}

// Exam ("Prüfungen").
type Exam struct {
	ID               int           `json:"id" yaml:"id"`
	ExamType         string        `json:"examType" yaml:"examType"`
	Name             string        `json:"name" yaml:"name"`
	StudentClass     []string      `json:"studentClass" yaml:"studentClass"`
	AssignedStudents []ExamStudent `json:"assignedStudents" yaml:"assignedStudents"`
	ExamDate         int           `json:"examDate" yaml:"examDate"`
	StartTime        int           `json:"startTime" yaml:"startTime"`
	EndTime          int           `json:"endTime" yaml:"endTime"`
	Subject          string        `json:"subject" yaml:"subject"`
	Teachers         []string      `json:"teachers" yaml:"teachers"`
	Rooms            []string      `json:"rooms" yaml:"rooms"`
	Text             string        `json:"text" yaml:"text"`
	Grade            string        `json:"grade" yaml:"grade"`
}

// Start returns the exam start.
func (e Exam) Start() time.Time { return dates.FromIntTime(e.ExamDate, e.StartTime) }

// End returns the exam end.
func (e Exam) End() time.Time { return dates.FromIntTime(e.ExamDate, e.EndTime) }

// Exams lists exams of a student.
func (c *Client) Exams(ctx context.Context, studentID int, from, to time.Time) ([]Exam, error) {
	q := rangeQuery(from, to)
	q.Set("studentId", strconv.Itoa(studentID))
	q.Set("withGrades", "true")
	q.Set("klasseId", "-1")
	var r struct {
		Data struct {
			Exams []Exam `json:"exams"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/exams", q, ttlFor(to, 15*time.Minute), &r); err != nil {
		return nil, err
	}
	sort.SliceStable(r.Data.Exams, func(i, j int) bool { return r.Data.Exams[i].Start().Before(r.Data.Exams[j].Start()) })
	return r.Data.Exams, nil
}

// ---------------------------------------------------------------- exemptions

// Exemptions lists exemptions ("Befreiungen") of a student.
func (c *Client) Exemptions(ctx context.Context, studentID int, from, to time.Time) ([]Row, error) {
	q := rangeQuery(from, to)
	q.Set("studentId", strconv.Itoa(studentID))
	var r struct {
		Data struct {
			Rows []Row `json:"rows"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/classreg/exemptions", q, ttlFor(to, 30*time.Minute), &r); err != nil {
		return nil, err
	}
	return r.Data.Rows, nil
}

// ---------------------------------------------------------------- contact hours

// OfficeHourClass is a selectable class for contact hours.
type OfficeHourClass struct {
	ID    int    `json:"id" yaml:"id"`
	Label string `json:"label" yaml:"label"`
}

// OfficeHourSettings controls which columns are shown.
type OfficeHourSettings struct {
	ShowHourNumber         bool   `json:"showHourNumber" yaml:"showHourNumber"`
	ShowPhoto              bool   `json:"showPhoto" yaml:"showPhoto"`
	ShowRoom               bool   `json:"showRoom" yaml:"showRoom"`
	ShowEmail              bool   `json:"showEmail" yaml:"showEmail"`
	ShowPhone              bool   `json:"showPhone" yaml:"showPhone"`
	NoAppointmentMsg       string `json:"noAppointmentMsg" yaml:"noAppointmentMsg"`
	CustomText             string `json:"customText" yaml:"customText"`
	SchoolPhone            string `json:"schoolPhone" yaml:"schoolPhone"`
	Anonymous              bool   `json:"anonymous" yaml:"anonymous"`
	AllowRegistration      bool   `json:"allowRegistration" yaml:"allowRegistration"`
	ShowRegistrationStatus bool   `json:"showRegistrationStatus" yaml:"showRegistrationStatus"`
}

// OfficeHours is the contact hours ("Sprechstunden") view of one week.
type OfficeHours struct {
	Week          time.Time          `json:"week" yaml:"week"`
	Settings      OfficeHourSettings `json:"settings" yaml:"settings"`
	Hours         []Row              `json:"hours" yaml:"hours"`
	Registrations []Row              `json:"registrations" yaml:"registrations"`
}

// OfficeHourClasses lists classes that can be used as filter.
func (c *Client) OfficeHourClasses(ctx context.Context) ([]OfficeHourClass, error) {
	var r struct {
		Data []OfficeHourClass `json:"data"`
	}
	err := c.GetJSON(ctx, "/WebUntis/api/public/officehours/classes", nil, 24*time.Hour, &r)
	return r.Data, err
}

// OfficeHours returns the contact hours of the week containing day.
// klasseID -1 means all classes.
func (c *Client) OfficeHours(ctx context.Context, day time.Time, klasseID int) (*OfficeHours, error) {
	oh := &OfficeHours{Week: dates.Monday(day)}
	var s struct {
		Data OfficeHourSettings `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/public/officehours/settings", nil, 24*time.Hour, &s); err != nil {
		return nil, err
	}
	oh.Settings = s.Data
	var h struct {
		Data []Row `json:"data"`
	}
	q := url.Values{"date": {dates.IntStr(oh.Week)}, "klasseId": {strconv.Itoa(klasseID)}}
	if err := c.GetJSON(ctx, "/WebUntis/api/public/officehours/hours", q, 30*time.Minute, &h); err != nil {
		return nil, err
	}
	oh.Hours = h.Data
	var reg struct {
		Data []Row `json:"data"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/public/officehours/registrations", nil, 5*time.Minute, &reg); err == nil {
		oh.Registrations = reg.Data
	}
	return oh, nil
}
