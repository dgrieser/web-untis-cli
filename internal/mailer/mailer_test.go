package mailer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dgrieser/web-untis-cli/internal/config"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

func TestBuild(t *testing.T) {
	cfg := config.SMTPConfig{Host: "smtp.example.com", From: "me@example.com", To: []string{"me@example.com"}}
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
	m := &webuntis.MessageDetail{ID: 42, Folder: "inbox", Subject: "Projektwoche", Content: "Liebe Eltern,\n\nsiehe Anhang.",
		SentDateTime: "2026-06-25T11:26:00", Sender: &webuntis.MessagePerson{DisplayName: "Lehrmann, M (LER)"},
		ReplyHistory: []webuntis.MessageDetail{{Content: "vorher", SentDateTime: "2026-06-24T10:00:00", Sender: &webuntis.MessagePerson{DisplayName: "Ich"}}}}
	files := []webuntis.DownloadedFile{{Name: "Brief.pdf", ContentType: "application/octet-stream", Data: []byte("%PDF-1.4")}, {Name: "Bild", Link: "https://x/y"}}
	msg, err := Build(cfg, "school", "https://s.webuntis.com/messages/inbox", m, files)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := msg.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"Subject: [WebUntis] Projektwoche", "Message-ID: <webuntis.school.inbox.42@webuntis-cli>",
		"X-WebUntis-Message-Id: 42", `filename="Brief.pdf"`, `application/pdf; name="Brief.pdf"`, "text/html", "Von:     Lehrmann, M (LER)", "> vorher", "https://x/y"} {
		if !strings.Contains(s, want) {
			t.Errorf("mail missing %q", want)
		}
	}
	if !strings.Contains(s, "Lehrmann, M (LER) via WebUntis") && !strings.Contains(s, "=?UTF-8?") {
		t.Errorf("from display name missing:\n%s", s[:400])
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(config.SMTPConfig{}); err == nil || !strings.Contains(err.Error(), "host, from, to") {
		t.Fatalf("got %v", err)
	}
	if err := Validate(config.SMTPConfig{Host: "h", From: "a@b", To: []string{"c@d"}, Security: "weird"}); err == nil {
		t.Fatal("expected error for invalid security")
	}
}

func TestBuildNews(t *testing.T) {
	cfg := config.SMTPConfig{Host: "h", From: "me@example.com", To: []string{"me@example.com"}}
	n := webuntis.MessageOfDay{ID: 371, Subject: "Einladung", Text: "<font>Hallo<br />Welt &amp; Co</font>",
		Attachments: []webuntis.NewsAttachment{{Name: "Bild.png", DownloadURL: "https://x/y?a=1&b=2"}}}
	msg, err := BuildNews(cfg, "school", "GES Test", "https://s.webuntis.com/today", n)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := msg.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"Subject: [WebUntis] Nachricht: Einladung", "Message-ID: <webuntis.school.news.371@webuntis-cli>",
		"X-WebUntis-News-Id: 371", "Hallo\r\nWelt & Co", "Anhang: Bild.png: https://x/y?a", "text/html", "https://s.webuntis.com/today"} {
		if !strings.Contains(s, want) {
			t.Errorf("news mail missing %q", want)
		}
	}
}
