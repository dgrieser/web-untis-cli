package webuntis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/cache"
	"github.com/dgrieser/web-untis-cli/internal/dates"
	"github.com/dgrieser/web-untis-cli/internal/render"
)

// ---------------------------------------------------------------- today / news

// NewsAttachment of a message of the day (usually a OneDrive link).
type NewsAttachment struct {
	ID          int    `json:"id" yaml:"id"`
	StorageID   string `json:"storageId,omitempty" yaml:"storageId,omitempty"`
	Name        string `json:"name" yaml:"name"`
	DownloadURL string `json:"downloadUrl,omitempty" yaml:"downloadUrl,omitempty"`
	StorageType string `json:"storageType,omitempty" yaml:"storageType,omitempty"`
}

// MessageOfDay is a news item shown on the "Heute" page.
type MessageOfDay struct {
	ID          int              `json:"id" yaml:"id"`
	Subject     string           `json:"subject" yaml:"subject"`
	Text        string           `json:"text" yaml:"text"` // HTML
	IsExpanded  bool             `json:"isExpanded" yaml:"isExpanded"`
	Attachments []NewsAttachment `json:"attachments" yaml:"attachments"`
	// New is set by the CLI for items not shown before (not part of the API).
	New bool `json:"-" yaml:"-"`
	// IncludeHTML adds the original HTML body as "html" to JSON output.
	IncludeHTML bool `json:"-" yaml:"-"`
}

// MarshalJSON emits the body as plain text in "text"; the original HTML
// from the API is added as "html" only if IncludeHTML is set.
func (m MessageOfDay) MarshalJSON() ([]byte, error) {
	type out struct {
		ID          int              `json:"id"`
		Subject     string           `json:"subject"`
		New         bool             `json:"new"`
		Text        string           `json:"text"`
		HTML        string           `json:"html,omitempty"`
		Attachments []NewsAttachment `json:"attachments"`
	}
	if m.Attachments == nil {
		m.Attachments = []NewsAttachment{}
	}
	o := out{ID: m.ID, Subject: m.Subject, New: m.New, Text: render.PlainText(m.Text), Attachments: m.Attachments}
	if m.IncludeHTML {
		o.HTML = m.Text
	}
	return marshalNoEscape(o)
}

// News is the content of the "Heute -> Nachrichten" widget.
type News struct {
	SystemMessage any            `json:"systemMessage" yaml:"systemMessage"`
	MessagesOfDay []MessageOfDay `json:"messagesOfDay" yaml:"messagesOfDay"`
	RSSURL        string         `json:"rssUrl" yaml:"rssUrl"`
}

// MarshalJSON converts an HTML system message to Markdown as well.
func (n News) MarshalJSON() ([]byte, error) {
	type alias News
	a := alias(n)
	if s, ok := a.SystemMessage.(string); ok && render.LooksLikeHTML(s) {
		a.SystemMessage = render.PlainText(s)
	}
	if a.MessagesOfDay == nil {
		a.MessagesOfDay = []MessageOfDay{}
	}
	return marshalNoEscape(a)
}

// marshalNoEscape is json.Marshal without escaping <, > and & (keeps the
// raw HTML fields readable).
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// News returns the news of the given day.
func (c *Client) News(ctx context.Context, day time.Time) (*News, error) {
	var r struct {
		Data News `json:"data"`
	}
	q := url.Values{"date": {dates.IntStr(day)}}
	if err := c.GetJSON(ctx, "/WebUntis/api/public/news/newsWidgetData", q, 5*time.Minute, &r); err != nil {
		return nil, err
	}
	return &r.Data, nil
}

// DashboardCard is an item of the dashboard (mirrors messages of the day
// including read status).
type DashboardCard struct {
	ID             int    `json:"id" yaml:"id"`
	Title          string `json:"title" yaml:"title"`
	Subtitle       string `json:"subtitle" yaml:"subtitle"`
	HasAttachments bool   `json:"hasAttachments" yaml:"hasAttachments"`
	HeaderColor    string `json:"headerColor" yaml:"headerColor"`
	OrderNo        int    `json:"orderNo" yaml:"orderNo"`
	Status         string `json:"status" yaml:"status"`
	Icon           string `json:"icon" yaml:"icon"`
}

// DashboardCards lists dashboard cards.
func (c *Client) DashboardCards(ctx context.Context) ([]DashboardCard, error) {
	var r struct {
		DashboardCards []DashboardCard `json:"dashboardCards"`
	}
	err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/dashboard/cards", nil, time.Minute, &r)
	return r.DashboardCards, err
}

// UnreadCounts of messages and dashboard cards.
type UnreadCounts struct {
	Messages int `json:"messages" yaml:"messages"`
	Cards    int `json:"cards" yaml:"cards"`
}

// UnreadCounts returns the unread counters shown in the navigation.
func (c *Client) UnreadCounts(ctx context.Context) (UnreadCounts, error) {
	var m struct {
		UnreadMessagesCount int `json:"unreadMessagesCount"`
	}
	var d struct {
		UnreadCardsCount int `json:"unreadCardsCount"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/messages/status", nil, 0, &m); err != nil {
		return UnreadCounts{}, err
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/dashboard/cards/status", nil, 0, &d); err != nil {
		return UnreadCounts{Messages: m.UnreadMessagesCount}, err
	}
	return UnreadCounts{Messages: m.UnreadMessagesCount, Cards: d.UnreadCardsCount}, nil
}

// ---------------------------------------------------------------- messages

// MessagePerson is a sender or recipient.
type MessagePerson struct {
	UserID      int    `json:"userId" yaml:"userId"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	ClassName   string `json:"className,omitempty" yaml:"className,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty" yaml:"imageUrl,omitempty"`
}

// MessageSummary is a row of inbox / sent / drafts.
type MessageSummary struct {
	ID                        int             `json:"id" yaml:"id"`
	Folder                    string          `json:"folder" yaml:"folder"`
	Subject                   string          `json:"subject" yaml:"subject"`
	ContentPreview            string          `json:"contentPreview" yaml:"contentPreview"`
	Sender                    *MessagePerson  `json:"sender,omitempty" yaml:"sender,omitempty"`
	SentDateTime              string          `json:"sentDateTime" yaml:"sentDateTime"`
	HasAttachments            bool            `json:"hasAttachments" yaml:"hasAttachments"`
	IsMessageRead             bool            `json:"isMessageRead" yaml:"isMessageRead"`
	IsReply                   bool            `json:"isReply" yaml:"isReply"`
	IsReplyAllowed            bool            `json:"isReplyAllowed" yaml:"isReplyAllowed"`
	AllowMessageDeletion      bool            `json:"allowMessageDeletion" yaml:"allowMessageDeletion"`
	NumberOfRecipients        int             `json:"numberOfRecipients,omitempty" yaml:"numberOfRecipients,omitempty"`
	RecipientPersons          []MessagePerson `json:"recipientPersons,omitempty" yaml:"recipientPersons,omitempty"`
	RecipientGroups           []any           `json:"recipientGroups,omitempty" yaml:"recipientGroups,omitempty"`
	RequestConfirmationStatus any             `json:"requestConfirmationStatus,omitempty" yaml:"requestConfirmationStatus,omitempty"`
	RequiresReadConfirmation  bool            `json:"requiresReadConfirmation,omitempty" yaml:"requiresReadConfirmation,omitempty"`
}

// Sent parses SentDateTime.
func (m MessageSummary) Sent() time.Time { t, _ := dates.ParseLocalDateTime(m.SentDateTime); return t }

// StorageAttachment is a file stored in WebUntis storage.
type StorageAttachment struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

// BlobAttachment is a legacy single attachment (download via /messages/{id}/attachment).
type BlobAttachment struct {
	ID   any    `json:"id,omitempty" yaml:"id,omitempty"`
	Name string `json:"name" yaml:"name"`
}

// MessageDetail is a full message.
type MessageDetail struct {
	ID                        int                 `json:"id" yaml:"id"`
	Folder                    string              `json:"folder" yaml:"folder"`
	Subject                   string              `json:"subject" yaml:"subject"`
	Content                   string              `json:"content" yaml:"content"`
	Sender                    *MessagePerson      `json:"sender,omitempty" yaml:"sender,omitempty"`
	Recipients                []MessagePerson     `json:"recipients,omitempty" yaml:"recipients,omitempty"`
	RecipientPersons          []MessagePerson     `json:"recipientPersons,omitempty" yaml:"recipientPersons,omitempty"`
	RecipientGroups           []any               `json:"recipientGroups,omitempty" yaml:"recipientGroups,omitempty"`
	SentDateTime              string              `json:"sentDateTime" yaml:"sentDateTime"`
	Attachments               []NewsAttachment    `json:"attachments" yaml:"attachments"`
	BlobAttachment            *BlobAttachment     `json:"blobAttachment" yaml:"blobAttachment"`
	StorageAttachments        []StorageAttachment `json:"storageAttachments" yaml:"storageAttachments"`
	IsReply                   bool                `json:"isReply" yaml:"isReply"`
	IsReplyAllowed            bool                `json:"isReplyAllowed" yaml:"isReplyAllowed"`
	IsReplyForbidden          bool                `json:"isReplyForbidden" yaml:"isReplyForbidden"`
	IsReportMessage           bool                `json:"isReportMessage" yaml:"isReportMessage"`
	IsRevoked                 bool                `json:"isRevoked,omitempty" yaml:"isRevoked,omitempty"`
	ReplyHistory              []MessageDetail     `json:"replyHistory,omitempty" yaml:"replyHistory,omitempty"`
	RequestConfirmation       any                 `json:"requestConfirmation,omitempty" yaml:"requestConfirmation,omitempty"`
	RequestConfirmationStatus any                 `json:"requestConfirmationStatus,omitempty" yaml:"requestConfirmationStatus,omitempty"`
}

// Sent parses SentDateTime.
func (m MessageDetail) Sent() time.Time { t, _ := dates.ParseLocalDateTime(m.SentDateTime); return t }

// AllRecipients merges recipient lists.
func (m MessageDetail) AllRecipients() []MessagePerson {
	if len(m.RecipientPersons) > 0 {
		return m.RecipientPersons
	}
	return m.Recipients
}

// AttachmentCount counts all attachment kinds.
func (m MessageDetail) AttachmentCount() int {
	n := len(m.Attachments) + len(m.StorageAttachments)
	if m.BlobAttachment != nil {
		n++
	}
	return n
}

// Folders.
const (
	FolderInbox  = "inbox"
	FolderSent   = "sent"
	FolderDrafts = "drafts"
)

// Inbox lists received messages. readConfirmation contains messages that
// require a read confirmation (shown separately in the UI).
func (c *Client) Inbox(ctx context.Context) (messages, readConfirmation []MessageSummary, err error) {
	var r struct {
		IncomingMessages         []MessageSummary `json:"incomingMessages"`
		ReadConfirmationMessages []MessageSummary `json:"readConfirmationMessages"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/messages", nil, time.Minute, &r); err != nil {
		return nil, nil, err
	}
	for i := range r.IncomingMessages {
		r.IncomingMessages[i].Folder = FolderInbox
	}
	for i := range r.ReadConfirmationMessages {
		r.ReadConfirmationMessages[i].Folder = FolderInbox
		r.ReadConfirmationMessages[i].RequiresReadConfirmation = true
	}
	return r.IncomingMessages, r.ReadConfirmationMessages, nil
}

// Sent lists sent messages.
func (c *Client) Sent(ctx context.Context) ([]MessageSummary, error) {
	var r struct {
		SentMessages []MessageSummary `json:"sentMessages"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/messages/sent", nil, time.Minute, &r); err != nil {
		return nil, err
	}
	for i := range r.SentMessages {
		r.SentMessages[i].Folder = FolderSent
	}
	return r.SentMessages, nil
}

// Drafts lists draft messages.
func (c *Client) Drafts(ctx context.Context) ([]MessageSummary, error) {
	var r struct {
		DraftMessages []MessageSummary `json:"draftMessages"`
	}
	if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/messages/drafts", nil, time.Minute, &r); err != nil {
		return nil, err
	}
	for i := range r.DraftMessages {
		r.DraftMessages[i].Folder = FolderDrafts
	}
	return r.DraftMessages, nil
}

// Messages lists a folder ("inbox", "sent", "drafts").
func (c *Client) Messages(ctx context.Context, folder string) ([]MessageSummary, error) {
	switch folder {
	case FolderInbox, "":
		m, rc, err := c.Inbox(ctx)
		return append(rc, m...), err
	case FolderSent:
		return c.Sent(ctx)
	case FolderDrafts:
		return c.Drafts(ctx)
	}
	return nil, fmt.Errorf("unknown folder %q (inbox, sent, drafts)", folder)
}

// Message fetches a full message. Note: like in the web UI, opening an
// unread inbox message marks it as read on the server. Details are cached
// permanently since messages are immutable.
func (c *Client) Message(ctx context.Context, folder string, id int) (*MessageDetail, error) {
	path := "/WebUntis/api/rest/view/v1/messages/" + strconv.Itoa(id)
	switch folder {
	case FolderSent:
		path = "/WebUntis/api/rest/view/v1/messages/sent/" + strconv.Itoa(id)
	case FolderDrafts:
		path = "/WebUntis/api/rest/view/v1/messages/drafts/" + strconv.Itoa(id)
	}
	ttl := cache.Forever
	if folder == FolderDrafts {
		ttl = time.Minute
	}
	var m MessageDetail
	if err := c.GetJSON(ctx, path, nil, ttl, &m); err != nil {
		return nil, err
	}
	if folder == "" {
		folder = FolderInbox
	}
	m.Folder = folder
	return &m, nil
}

// FindMessage looks up a message id in all folders.
func (c *Client) FindMessage(ctx context.Context, id int) (string, error) {
	for _, f := range []string{FolderInbox, FolderSent, FolderDrafts} {
		list, err := c.Messages(ctx, f)
		if err != nil {
			return "", err
		}
		for _, m := range list {
			if m.ID == id {
				return f, nil
			}
		}
	}
	return "", fmt.Errorf("message %d not found in inbox, sent or drafts", id)
}

// DownloadedFile is an attachment's content.
type DownloadedFile struct {
	Name        string
	ContentType string
	Data        []byte
	// Link is set instead of Data for attachments that can only be opened
	// in a browser (e.g. OneDrive shares).
	Link string
}

// DownloadAttachments downloads all attachments of a message.
func (c *Client) DownloadAttachments(ctx context.Context, m *MessageDetail) ([]DownloadedFile, error) {
	var out []DownloadedFile
	for _, a := range m.StorageAttachments {
		var su struct {
			DownloadURL       string `json:"downloadUrl"`
			AdditionalHeaders []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"additionalHeaders"`
		}
		if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/messages/"+url.PathEscape(a.ID)+"/attachmentstorageurl", nil, 0, &su); err != nil {
			return out, fmt.Errorf("attachment %q: %w", a.Name, err)
		}
		h := map[string]string{}
		for _, kv := range su.AdditionalHeaders {
			h[kv.Key] = kv.Value
		}
		data, ct, err := c.Download(ctx, su.DownloadURL, h, false)
		if err != nil {
			return out, fmt.Errorf("attachment %q: %w", a.Name, err)
		}
		out = append(out, DownloadedFile{Name: a.Name, ContentType: ct, Data: data})
	}
	if m.BlobAttachment != nil {
		data, ct, err := c.Download(ctx, c.base()+"/WebUntis/api/rest/view/v1/messages/"+strconv.Itoa(m.ID)+"/attachment", nil, true)
		if err != nil {
			return out, fmt.Errorf("attachment %q: %w", m.BlobAttachment.Name, err)
		}
		out = append(out, DownloadedFile{Name: m.BlobAttachment.Name, ContentType: ct, Data: data})
	}
	for _, a := range m.Attachments {
		out = append(out, DownloadedFile{Name: a.Name, Link: a.DownloadURL})
	}
	return out, nil
}

// MessagePermissions describes what the user may do.
type MessagePermissions struct {
	RecipientOptions             []string `json:"recipientOptions" yaml:"recipientOptions"`
	AllowRequestReadConfirmation bool     `json:"allowRequestReadConfirmation" yaml:"allowRequestReadConfirmation"`
	ShowDraftsTab                bool     `json:"showDraftsTab" yaml:"showDraftsTab"`
	ShowSentTab                  bool     `json:"showSentTab" yaml:"showSentTab"`
	MaxFileSize                  int      `json:"maxFileSize" yaml:"maxFileSize"`
	MaxFileCount                 int      `json:"maxFileCount" yaml:"maxFileCount"`
}

// MessagePermissions returns the messaging permissions.
func (c *Client) MessagePermissions(ctx context.Context) (*MessagePermissions, error) {
	var p MessagePermissions
	err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/messages/permissions", nil, time.Hour, &p)
	return &p, err
}
