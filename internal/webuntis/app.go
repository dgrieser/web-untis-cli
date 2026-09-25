package webuntis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/dates"
)

// DateRange as used by the REST API.
type DateRange struct {
	Start string `json:"start" yaml:"start"`
	End   string `json:"end" yaml:"end"`
}

// StartTime parses Start.
func (d DateRange) StartTime() time.Time { t, _ := dates.ParseLocalDateTime(d.Start); return t }

// EndTime parses End.
func (d DateRange) EndTime() time.Time { t, _ := dates.ParseLocalDateTime(d.End); return t }

// TimeUnit is one period of the time grid (times as HHMM ints).
type TimeUnit struct {
	UnitOfDay int `json:"unitOfDay" yaml:"unitOfDay"`
	StartTime int `json:"startTime" yaml:"startTime"`
	EndTime   int `json:"endTime" yaml:"endTime"`
}

// SchoolYear with optional time grid.
type SchoolYear struct {
	ID        int       `json:"id" yaml:"id"`
	Name      string    `json:"name" yaml:"name"`
	DateRange DateRange `json:"dateRange" yaml:"dateRange"`
	TimeGrid  *struct {
		Units []TimeUnit `json:"units" yaml:"units"`
	} `json:"timeGrid,omitempty" yaml:"timeGrid,omitempty"`
}

// PersonRef is a person reference in app data.
type PersonRef struct {
	ID          int    `json:"id" yaml:"id"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	ImageURL    string `json:"imageUrl,omitempty" yaml:"imageUrl,omitempty"`
}

// Holiday from app data.
type Holiday struct {
	ID        int    `json:"id" yaml:"id"`
	Name      string `json:"name" yaml:"name"`
	LongName  string `json:"longName" yaml:"longName"`
	Start     string `json:"start" yaml:"start"`
	End       string `json:"end" yaml:"end"`
	StartDate string `json:"startDate,omitempty" yaml:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty" yaml:"endDate,omitempty"`
}

// AppData is the bootstrap information of /api/rest/view/v1/app/data.
type AppData struct {
	CurrentSchoolYear SchoolYear `json:"currentSchoolYear" yaml:"currentSchoolYear"`
	Tenant            struct {
		ID          string `json:"id" yaml:"id"`
		Name        string `json:"name" yaml:"name"`
		DisplayName string `json:"displayName" yaml:"displayName"`
	} `json:"tenant" yaml:"tenant"`
	User struct {
		ID          int         `json:"id" yaml:"id"`
		Name        string      `json:"name" yaml:"name"`
		Email       string      `json:"email" yaml:"email"`
		Locale      string      `json:"locale" yaml:"locale"`
		Person      *PersonRef  `json:"person" yaml:"person"`
		Roles       []string    `json:"roles" yaml:"roles"`
		Students    []PersonRef `json:"students" yaml:"students"`
		LastLogin   string      `json:"lastLogin" yaml:"lastLogin"`
		Permissions struct {
			Views []string `json:"views" yaml:"views"`
		} `json:"permissions" yaml:"permissions"`
	} `json:"user" yaml:"user"`
	Holidays []Holiday `json:"holidays" yaml:"holidays"`
}

// AppData returns (cached) app bootstrap data.
func (c *Client) AppData(ctx context.Context) (*AppData, error) {
	if c.appData != nil {
		return c.appData, nil
	}
	var d AppData
	if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/app/data", nil, 6*time.Hour, &d); err != nil {
		return nil, err
	}
	c.appData = &d
	if c.Profile.TenantID == "" && d.Tenant.ID != "" {
		c.Profile.TenantID = d.Tenant.ID
		c.Profile.SchoolDisplayName = strings.Join(strings.Fields(d.Tenant.DisplayName), " ")
		_ = c.Profile.Save()
	}
	return &d, nil
}

// SchoolYears lists all school years.
func (c *Client) SchoolYears(ctx context.Context) ([]SchoolYear, error) {
	var out []SchoolYear
	err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/schoolyears", nil, 24*time.Hour, &out)
	return out, err
}

// SchoolYearFor returns the school year containing day (or the current one).
func (c *Client) SchoolYearFor(ctx context.Context, day time.Time) (SchoolYear, error) {
	years, err := c.SchoolYears(ctx)
	if err == nil {
		for _, y := range years {
			s, e := y.DateRange.StartTime(), y.DateRange.EndTime()
			if !day.Before(s) && !day.After(e) {
				return y, nil
			}
		}
	}
	ad, aerr := c.AppData(ctx)
	if aerr != nil {
		return SchoolYear{}, aerr
	}
	return ad.CurrentSchoolYear, nil
}

// Student is the student whose data is shown (the user itself for student
// accounts, or one of the children for legal guardians).
type Student struct {
	ID   int    `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

// Students lists the students the account can see.
func (c *Client) Students(ctx context.Context) ([]Student, error) {
	ad, err := c.AppData(ctx)
	if err != nil {
		return nil, err
	}
	var out []Student
	for _, s := range ad.User.Students {
		out = append(out, Student{ID: s.ID, Name: s.DisplayName})
	}
	if len(out) == 0 && ad.User.Person != nil {
		out = append(out, Student{ID: ad.User.Person.ID, Name: ad.User.Person.DisplayName})
	}
	return out, nil
}

// ResolveStudent picks a student by id or (partial, case-insensitive) name.
// An empty selector uses the profile default, then the first student.
func (c *Client) ResolveStudent(ctx context.Context, selector string) (Student, error) {
	students, err := c.Students(ctx)
	if err != nil {
		return Student{}, err
	}
	if len(students) == 0 {
		return Student{}, fmt.Errorf("this account has no associated student")
	}
	if selector == "" {
		selector = c.Profile.Student
	}
	if selector == "" {
		return students[0], nil
	}
	if id, err := strconv.Atoi(selector); err == nil {
		for _, s := range students {
			if s.ID == id {
				return s, nil
			}
		}
	}
	sel := strings.ToLower(selector)
	var matches []Student
	for _, s := range students {
		if strings.Contains(strings.ToLower(s.Name), sel) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Student{}, fmt.Errorf("no student matches %q (available: %s)", selector, studentNames(students))
	default:
		return Student{}, fmt.Errorf("%q is ambiguous (matches: %s)", selector, studentNames(matches))
	}
}

func studentNames(s []Student) string {
	var n []string
	for _, x := range s {
		n = append(n, fmt.Sprintf("%s (%d)", x.Name, x.ID))
	}
	return strings.Join(n, ", ")
}

// LatestImportTime returns the time of the last timetable import (JSON-RPC).
func (c *Client) LatestImportTime(ctx context.Context) (time.Time, error) {
	var ms int64
	if err := c.RPC(ctx, "getLatestImportTime", map[string]any{}, &ms); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms), nil
}
