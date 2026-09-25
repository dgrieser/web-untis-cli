package webuntis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/config"
)

func fakeJWT(exp time.Time) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	p := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return h + "." + p + ".sig"
}

// fixtures mirror responses captured from a real WebUntis instance (trimmed).
var fixtures = map[string]string{
	"/WebUntis/api/rest/view/v1/app/data": `{"currentSchoolYear":{"dateRange":{"start":"2026-08-31","end":"2027-07-16"},"id":83,"name":"2026/2027",
		"timeGrid":{"schoolyearId":83,"units":[{"endTime":830,"startTime":745,"unitOfDay":1},{"endTime":920,"startTime":835,"unitOfDay":2},{"endTime":1025,"startTime":940,"unitOfDay":3}]}},
		"tenant":{"displayName":"GES  HÜLLHORST","id":"5144100","name":"ge-huellhorst"},
		"user":{"id":11031,"locale":"deNG","name":"parent@example.com","email":"parent@example.com","person":{"displayName":"Parent P","id":8243},
		"roles":["LEGAL_GUARDIAN"],"students":[{"displayName":"Kid Alpha","id":8685},{"displayName":"Kid Beta","id":9000}],"lastLogin":"2026-09-23T14:43:05.524"}}`,
	"/WebUntis/api/rest/view/v1/schoolyears": `[{"dateRange":{"start":"2026-08-31","end":"2027-07-16"},"id":83,"name":"2026/2027"}]`,
	"/WebUntis/api/public/news/newsWidgetData": `{"data":{"systemMessage":null,"messagesOfDay":[{"id":371,"subject":"Einladung","text":"<font size=\"4\">Hallo <b>Welt</b><br />Zeile 2</font>","isExpanded":false,
		"attachments":[{"id":-1,"storageId":"","name":"Bild.png","downloadUrl":"https://example.sharepoint.com/x","storageType":"ONEDRIVE","headers":null}]}],"rssUrl":"NewsFeed.do?school=x"}}`,
	"/WebUntis/api/rest/view/v1/messages": `{"incomingMessages":[{"id":115070,"subject":"Kreatives Klassenzimmer","contentPreview":"Liebe Eltern","sender":{"className":null,"displayName":"Teacher, T (TT)","imageUrl":null,"userId":2567},
		"sentDateTime":"2026-07-21T17:11:00","allowMessageDeletion":true,"hasAttachments":true,"isMessageRead":false,"isReply":false,"isReplyAllowed":true}],"readConfirmationMessages":[]}`,
	"/WebUntis/api/rest/view/v1/messages/115070": `{"id":115070,"subject":"Kreatives Klassenzimmer","content":"Liebe Eltern,\n\nText.","sender":{"className":null,"displayName":"Teacher, T (TT)","imageUrl":null,"userId":2567},
		"sentDateTime":"2026-07-21T17:11:00","allowMessageDeletion":true,"attachments":[],"blobAttachment":null,"storageAttachments":[{"id":"c3a2eefc","name":"Brief.pdf"}],
		"isReply":false,"isReplyAllowed":true,"isReportMessage":false,"isReplyForbidden":false,"replyHistory":[],"requestConfirmation":null}`,
	"/WebUntis/api/rest/view/v1/messages/c3a2eefc/attachmentstorageurl": `{"additionalHeaders":[{"key":"x-amz-test","value":"1"}],"downloadUrl":"STORAGE/file"}`,
	"/WebUntis/api/rest/view/v1/timetable/entries": `{"format":1,"days":[{"date":"2026-09-21","resourceType":"STUDENT","resource":{"id":8685,"shortName":"KidAlp","longName":"Alpha","displayName":""},"status":"REGULAR","dayEntries":[],
		"gridEntries":[
		 {"ids":[1],"duration":{"start":"2026-09-21T07:45","end":"2026-09-21T09:20"},"type":"NORMAL_TEACHING_PERIOD","status":"REGULAR","statusDetail":null,"texts":[],"lessonText":"","substitutionText":"",
		  "position1":[{"current":{"type":"TEACHER","status":"REGULAR","shortName":"LER","longName":"Lehrmann","displayName":"LER"},"removed":null}],
		  "position2":[{"current":{"type":"SUBJECT","status":"REGULAR","shortName":"D","longName":"DEUTSCH SEK.I","displayName":"D"},"removed":null}],
		  "position3":[{"current":{"type":"ROOM","status":"REGULAR","shortName":"D104","longName":"KR 6c","displayName":"D104"},"removed":null}]},
		 {"ids":[2],"duration":{"start":"2026-09-21T09:40","end":"2026-09-21T10:25"},"type":"ADDITIONAL_PERIOD","status":"ADDITIONAL","statusDetail":"MOVED","moved":{"start":"2026-09-21T12:20","end":"2026-09-21T13:05"},
		  "position1":[{"current":{"type":"TEACHER","status":"ADDED","shortName":"LER","longName":"Lehrmann","displayName":"LER"},"removed":{"type":"TEACHER","status":"REMOVED","shortName":"POD","longName":"Podszuweit","displayName":"POD"}}],
		  "position2":[{"current":{"type":"SUBJECT","status":"REGULAR","shortName":"E","longName":"ENGLISCH","displayName":"E"},"removed":null}]},
		 {"ids":[3],"duration":{"start":"2026-09-21T09:40","end":"2026-09-21T10:25"},"type":"NORMAL_TEACHING_PERIOD","status":"CANCELLED","statusDetail":null,
		  "position2":[{"current":{"type":"SUBJECT","status":"REGULAR","shortName":"M","longName":"MATHE","displayName":"M"},"removed":null}]}
		],"backEntries":[]}],"errors":[]}`,
	"/WebUntis/api/homeworks/lessons": `{"data":{"records":[{"homeworkId":1,"teacherId":172,"elementIds":[8685]},{"homeworkId":2,"teacherId":172,"elementIds":[9000]}],
		"homeworks":[{"id":1,"lessonId":10,"date":20260915,"dueDate":20260922,"text":"vocabulary p. 241","remark":"","completed":true,"attachments":[]},
		             {"id":2,"lessonId":10,"date":20260915,"dueDate":20260923,"text":"other kid","remark":"","completed":false,"attachments":[]}],
		"teachers":[{"id":172,"name":"Albert, Saskia (ALB)"}],"lessons":[{"id":10,"subject":"E","lessonType":"Unterricht"}]}}`,
	"/WebUntis/api/classreg/absencetimes/student": `{"data":{"absenceTimes":[
		{"absenceId":1,"klasseName":"5c","subjectName":"AH","teacherName":"Janek, M (JAN)","excuseStatusName":"entschuldigt","excused":true,"date":20250922,"startTime":1130,"endTime":1215,"missedHours":0,"missedMins":45,"counting":true},
		{"absenceId":1,"klasseName":"5c","subjectName":"D","teacherName":"Lehrmann (LER)","excuseStatusName":"","excused":false,"date":20250922,"startTime":745,"endTime":830,"missedHours":0,"missedMins":45,"counting":true}]}}`,
	"/WebUntis/api/rest/view/v1/app/platform-application/menus": `[{"icon":"pa-eigenerlink-logo.png","id":54,"logoutUrl":null,"name":"Namen eingeben","openInNewTab":true,
		"redirectUrl":"https%3A%2F%2Fexample.org%2Fwp-content%2Fuploads%2F2026%2F09%2FKW39.pdf?tenant_id=5144100&school=ge-huellhorst","reloadOnResume":false},
		{"icon":"pa-klassengeld-logo.svg","id":67,"logoutUrl":"https://klassengeld.app/logout_untis","name":"Klassengeld","openInNewTab":true,"redirectUrl":"https://klassengeld.app/login_untis?tenant_id=5144100&school=ge-huellhorst","reloadOnResume":false}]`,
}

type fakeServer struct {
	*httptest.Server
	logins     atomic.Int32
	tokenCalls atomic.Int32
	expireOnce atomic.Bool // next REST call answers 401 once
}

func newFakeServer(t *testing.T) *fakeServer {
	fs := &fakeServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/WebUntis/jsonrpc.do", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "authenticate":
			if req.Params["password"] != "secret" {
				_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","error":{"message":"bad credentials","code":-8504}}`)
				return
			}
			fs.logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "SESSION1", Path: "/WebUntis"})
			_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":{"sessionId":"SESSION1","personType":12,"personId":8243}}`)
		case "getLatestImportTime":
			_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":1790254884684}`)
		default:
			_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":null}`)
		}
	})
	mux.HandleFunc("/WebUntis/api/token/new", func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("JSESSIONID")
		if err != nil || ck.Value != "SESSION1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fs.tokenCalls.Add(1)
		_, _ = fmt.Fprint(w, fakeJWT(time.Now().Add(15*time.Minute)))
	})
	mux.HandleFunc("/STORAGE/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-amz-test") != "1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "%PDF-1.4 test")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/WebUntis/api/rest/") {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if fs.expireOnce.CompareAndSwap(true, false) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		body, ok := fixtures[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body = strings.ReplaceAll(body, `"STORAGE/file"`, `"`+fs.URL+`/STORAGE/file"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})
	fs.Server = httptest.NewTLSServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

func newTestClient(t *testing.T, fs *fakeServer, password string) *Client {
	t.Setenv("WEBUNTIS_CLI_HOME", t.TempDir())
	u, _ := url.Parse(fs.URL)
	p := &config.Profile{Name: "test", Server: u.Host, School: "ge-huellhorst", Username: "parent@example.com", Password: password}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	c, err := New(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	hc := fs.Client()
	jar, _ := cookiejar.New(nil)
	hc.Jar = jar
	c.http = hc
	c.jar = jar
	return c
}

func TestLoginAndAppData(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	ctx := context.Background()
	ad, err := c.AppData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fs.logins.Load() != 1 {
		t.Fatalf("expected 1 login, got %d", fs.logins.Load())
	}
	if ad.CurrentSchoolYear.Name != "2026/2027" || len(ad.User.Students) != 2 {
		t.Fatalf("unexpected app data: %+v", ad)
	}
	if c.Profile.TenantID != "5144100" || c.Profile.SchoolDisplayName != "GES HÜLLHORST" {
		t.Fatalf("tenant not stored: %+v", c.Profile)
	}
	st, err := c.ResolveStudent(ctx, "beta")
	if err != nil || st.ID != 9000 {
		t.Fatalf("resolve student: %v %+v", err, st)
	}
	if _, err := c.ResolveStudent(ctx, "kid"); err == nil {
		t.Fatal("expected ambiguity error")
	}
	// Session is persisted and reused by a new client without new login.
	if err := c.SaveSession(); err != nil {
		t.Fatal(err)
	}
	s := c.Profile.LoadSession()
	if s.Token == "" || len(s.Cookies) == 0 {
		t.Fatalf("session not persisted: %+v", s)
	}
}

func TestBadCredentials(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "wrong")
	_, err := c.AppData(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("expected bad credentials error, got %v", err)
	}
}

func TestReloginOn401(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	ctx := context.Background()
	if _, err := c.News(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	fs.expireOnce.Store(true)
	c.Cache = nil
	if _, _, err := c.Inbox(ctx); err != nil {
		t.Fatal(err)
	}
	if fs.logins.Load() != 2 {
		t.Fatalf("expected re-login, logins=%d", fs.logins.Load())
	}
}

func TestMessagesAndAttachments(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	ctx := context.Background()
	msgs, err := c.Messages(ctx, FolderInbox)
	if err != nil || len(msgs) != 1 || msgs[0].Folder != FolderInbox || msgs[0].IsMessageRead {
		t.Fatalf("inbox: %v %+v", err, msgs)
	}
	f, err := c.FindMessage(ctx, 115070)
	if err != nil || f != FolderInbox {
		t.Fatalf("find: %v %s", err, f)
	}
	m, err := c.Message(ctx, f, 115070)
	if err != nil || m.AttachmentCount() != 1 {
		t.Fatalf("message: %v %+v", err, m)
	}
	files, err := c.DownloadAttachments(ctx, m)
	if err != nil || len(files) != 1 || string(files[0].Data) != "%PDF-1.4 test" {
		t.Fatalf("attachments: %v %+v", err, files)
	}
}

func TestTimetableNormalization(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	day := time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local)
	tt, err := c.Timetable(context.Background(), TimetableQuery{ResourceType: "STUDENT", ResourceID: 8685, TimetableType: "MY_TIMETABLE", Start: day, End: day})
	if err != nil {
		t.Fatal(err)
	}
	if len(tt.Days) != 1 || len(tt.Days[0].Lessons) != 3 || len(tt.TimeGrid) != 3 {
		t.Fatalf("unexpected timetable: %+v", tt)
	}
	l := tt.Days[0].Lessons
	if l[0].SubjectLabel() != "D" || l[0].TeacherLabel() != "LER" || l[0].RoomLabel() != "D104" || l[0].Changed() || l[0].Cancelled() {
		t.Fatalf("lesson 0: %+v", l[0])
	}
	if l[1].TeacherLabel() != "LER (statt POD)" || !l[1].Changed() || l[1].MovedFrom == nil {
		t.Fatalf("lesson 1: %+v teacher=%q", l[1], l[1].TeacherLabel())
	}
	if !l[2].Cancelled() || l[2].Changed() {
		t.Fatalf("lesson 2 should be cancelled: %+v", l[2])
	}
}

func TestHomeworkFilteredByStudent(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	d := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	hw, err := c.Homework(context.Background(), 8685, d, d.AddDate(0, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(hw) != 1 || hw[0].Subject != "E" || hw[0].Teacher != "Albert, Saskia (ALB)" || !hw[0].Completed {
		t.Fatalf("homework: %+v", hw)
	}
}

func TestAbsenceTimesSorted(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	d := time.Date(2025, 8, 1, 0, 0, 0, 0, time.Local)
	times, err := c.AbsenceTimes(context.Background(), 8685, d, d.AddDate(1, 0, 0), false, false)
	if err != nil || len(times) != 2 || times[0].StartTime != 745 {
		t.Fatalf("absence times: %v %+v", err, times)
	}
}

func TestPlatformApps(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	a, err := c.PlatformApp(context.Background(), "namen")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "link" || a.LinkTarget() != "https://example.org/wp-content/uploads/2026/09/KW39.pdf" {
		t.Fatalf("link addin: %+v target=%s", a, a.LinkTarget())
	}
	kg, err := c.PlatformApp(context.Background(), "Klassengeld")
	if err != nil || kg.Kind != "sso" {
		t.Fatalf("klassengeld addin: %v %+v", err, kg)
	}
}

func TestLatestImportTime(t *testing.T) {
	fs := newFakeServer(t)
	c := newTestClient(t, fs, "secret")
	it, err := c.LatestImportTime(context.Background())
	if err != nil || it.Year() != 2026 {
		t.Fatalf("import time: %v %v", err, it)
	}
}
