package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/web-untis-cli/internal/dates"
	"github.com/dgrieser/web-untis-cli/internal/mailer"
	"github.com/dgrieser/web-untis-cli/internal/render"
	"github.com/dgrieser/web-untis-cli/internal/tracker"
	"github.com/dgrieser/web-untis-cli/internal/views"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

func mdTable(h []string, rows [][]string) string { return render.Table(h, rows) }

func errNotFound(kind, sel string, avail []string) error {
	return fmt.Errorf("no %s matches %q (available: %s)", kind, sel, strings.Join(avail, ", "))
}

func normFolder(f string) (string, error) {
	switch strings.ToLower(f) {
	case "", "inbox", "in", "posteingang", "received":
		return webuntis.FolderInbox, nil
	case "sent", "out", "gesendet", "outbox":
		return webuntis.FolderSent, nil
	case "drafts", "draft", "entwürfe", "entwuerfe":
		return webuntis.FolderDrafts, nil
	case "all", "alle":
		return "all", nil
	}
	return "", fmt.Errorf("unknown folder %q (inbox, sent, drafts, all)", f)
}

func (a *app) messagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "messages",
		Aliases: []string{"msg", "mail", "mitteilungen"},
		Short:   "Messages: inbox, sent, drafts, show, attachments, forward via SMTP",
		Long: `Messages (Mitteilungen). Without subcommand the inbox is listed.

Note: like in the web UI, opening an unread inbox message (show, attachments,
forward, list --full) marks it as read on the server. Message details are
cached locally, so repeated access does not hit the server again.`,
	}
	list := a.messagesListCmd()
	cmd.RunE = list.RunE
	cmd.Flags().AddFlagSet(list.Flags())
	cmd.AddCommand(list, a.messagesShowCmd(), a.messagesAttachmentsCmd(), a.messagesForwardCmd())
	for _, f := range []string{"inbox", "sent", "drafts"} {
		folder := f
		sub := &cobra.Command{
			Use:   folder,
			Short: "List " + folder,
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.listMessages(cmd.Context(), folder, false, false, 0, "")
			},
		}
		cmd.AddCommand(sub)
	}
	return cmd
}

func (a *app) messagesListCmd() *cobra.Command {
	var folder, search string
	var unread, full bool
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List messages of a folder",
		Example: `  webuntis messages
  webuntis messages list --folder sent
  webuntis messages --unread
  webuntis messages --search projektwoche
  webuntis messages list --folder all --full -o json > messages.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := normFolder(folder)
			if err != nil {
				return err
			}
			return a.listMessages(cmd.Context(), f, unread, full, limit, search)
		},
	}
	cmd.Flags().StringVarP(&folder, "folder", "F", "inbox", "inbox, sent, drafts or all")
	cmd.Flags().BoolVarP(&unread, "unread", "u", false, "only unread messages")
	cmd.Flags().BoolVar(&full, "full", false, "fetch full content of every message (marks them as read)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "maximum number of messages")
	cmd.Flags().StringVar(&search, "search", "", "filter by text in subject, preview or sender")
	return cmd
}

func (a *app) listMessages(ctx context.Context, folder string, unread, full bool, limit int, search string) error {
	c, err := a.api()
	if err != nil {
		return err
	}
	folders := []string{folder}
	if folder == "all" {
		folders = []string{webuntis.FolderInbox, webuntis.FolderSent, webuntis.FolderDrafts}
	}
	type folderData struct {
		Folder   string                    `json:"folder" yaml:"folder"`
		Messages []webuntis.MessageSummary `json:"messages" yaml:"messages"`
		Details  []*webuntis.MessageDetail `json:"details,omitempty" yaml:"details,omitempty"`
	}
	var all []folderData
	for _, f := range folders {
		msgs, err := c.Messages(ctx, f)
		if err != nil {
			return err
		}
		var filtered []webuntis.MessageSummary
		for _, m := range msgs {
			if unread && (f != webuntis.FolderInbox || m.IsMessageRead) {
				continue
			}
			if search != "" {
				hay := strings.ToLower(m.Subject + " " + m.ContentPreview)
				if m.Sender != nil {
					hay += " " + strings.ToLower(m.Sender.DisplayName)
				}
				for _, r := range m.RecipientPersons {
					hay += " " + strings.ToLower(r.DisplayName)
				}
				if !strings.Contains(hay, strings.ToLower(search)) {
					continue
				}
			}
			filtered = append(filtered, m)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
		fd := folderData{Folder: f, Messages: filtered}
		if full {
			for _, m := range filtered {
				d, err := c.Message(ctx, f, m.ID)
				if err != nil {
					return err
				}
				fd.Details = append(fd.Details, d)
			}
		}
		all = append(all, fd)
	}
	var data any = all
	if len(all) == 1 {
		data = all[0]
	}
	return a.emit(data, func() string {
		var parts []string
		for _, fd := range all {
			if full {
				parts = append(parts, views.MessageList(fd.Folder, fd.Messages))
				for _, d := range fd.Details {
					parts = append(parts, views.Message(d))
				}
				continue
			}
			parts = append(parts, views.MessageList(fd.Folder, fd.Messages))
		}
		return strings.Join(parts, "\n---\n\n")
	}, nil)
}

func (a *app) findFolder(ctx context.Context, c *webuntis.Client, folder string, id int) (string, error) {
	if folder != "" {
		return normFolder(folder)
	}
	return c.FindMessage(ctx, id)
}

func (a *app) messagesShowCmd() *cobra.Command {
	var folder string
	cmd := &cobra.Command{
		Use:     "show ID...",
		Aliases: []string{"read", "cat", "get"},
		Short:   "Show messages with full content, attachments and reply history",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.api()
			if err != nil {
				return err
			}
			var list []*webuntis.MessageDetail
			for _, arg := range args {
				id, err := strconv.Atoi(arg)
				if err != nil {
					return fmt.Errorf("invalid message id %q", arg)
				}
				f, err := a.findFolder(ctx, c, folder, id)
				if err != nil {
					return err
				}
				m, err := c.Message(ctx, f, id)
				if err != nil {
					return err
				}
				list = append(list, m)
			}
			var data any = list
			if len(list) == 1 {
				data = list[0]
			}
			return a.emit(data, func() string {
				var parts []string
				for _, m := range list {
					parts = append(parts, views.Message(m))
				}
				return strings.Join(parts, "\n---\n\n")
			}, nil)
		},
	}
	cmd.Flags().StringVarP(&folder, "folder", "F", "", "folder of the message (default: auto-detect)")
	return cmd
}

var unsafeName = regexp.MustCompile(`[/\\\x00-\x1f]`)

func safeFileName(s string) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, "_"))
	if s == "" || s == "." || s == ".." {
		s = "attachment"
	}
	return s
}

func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}

func (a *app) messagesAttachmentsCmd() *cobra.Command {
	var folder, dir string
	var listOnly bool
	cmd := &cobra.Command{
		Use:     "attachments ID",
		Aliases: []string{"att", "download"},
		Short:   "Download the attachments of a message",
		Example: `  webuntis messages attachments 111990
  webuntis messages attachments 111990 --dir ~/Downloads`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.api()
			if err != nil {
				return err
			}
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid message id %q", args[0])
			}
			f, err := a.findFolder(ctx, c, folder, id)
			if err != nil {
				return err
			}
			m, err := c.Message(ctx, f, id)
			if err != nil {
				return err
			}
			if m.AttachmentCount() == 0 {
				fmt.Fprintln(os.Stderr, "Message has no attachments.")
				return nil
			}
			if listOnly {
				return a.renderer().Markdown(views.Message(m))
			}
			files, err := c.DownloadAttachments(ctx, m)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			type saved struct {
				Name string `json:"name" yaml:"name"`
				Path string `json:"path,omitempty" yaml:"path,omitempty"`
				Link string `json:"link,omitempty" yaml:"link,omitempty"`
				Size int    `json:"size" yaml:"size"`
			}
			var out []saved
			for _, file := range files {
				if file.Data == nil {
					out = append(out, saved{Name: file.Name, Link: file.Link})
					continue
				}
				p := uniquePath(dir, safeFileName(file.Name))
				if err := os.WriteFile(p, file.Data, 0o644); err != nil {
					return err
				}
				out = append(out, saved{Name: file.Name, Path: p, Size: len(file.Data)})
			}
			return a.emit(out, func() string {
				var rows [][]string
				for _, s := range out {
					where := s.Path
					if where == "" {
						where = s.Link + " (Link, nur im Browser)"
					}
					rows = append(rows, []string{render.Esc(s.Name), render.Esc(where), humanSize(s.Size)})
				}
				return "## 📎 Anhänge gespeichert\n\n" + mdTable([]string{"Datei", "Pfad", "Größe"}, rows)
			}, nil)
		},
	}
	cmd.Flags().StringVarP(&folder, "folder", "F", "", "folder of the message (default: auto-detect)")
	cmd.Flags().StringVarP(&dir, "dir", "d", ".", "target directory")
	cmd.Flags().BoolVarP(&listOnly, "list", "l", false, "only list attachments")
	return cmd
}

func humanSize(n int) string {
	switch {
	case n == 0:
		return ""
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
}

func (a *app) messagesForwardCmd() *cobra.Command {
	var folder, since string
	var dryRun, force, markOnly, noAttach bool
	var watch time.Duration
	var limit int
	cmd := &cobra.Command{
		Use:   "forward [ID...]",
		Short: "Forward messages to an e-mail address via SMTP",
		Long: `Forward WebUntis messages (incl. attachments and reply history) as e-mail.

Without IDs, all messages of the folder that have not been forwarded yet are
sent (tracked in <profile>/forwarded.json). Use --mark-only once to mark all
existing messages as forwarded without sending, then run "forward" regularly
(cron, systemd timer or --watch) to get new messages by e-mail.

SMTP settings: "webuntis config smtp" (password also via $WEBUNTIS_SMTP_PASSWORD).
Note: forwarding opens the message, which marks it as read in WebUntis.`,
		Example: `  webuntis messages forward --dry-run
  webuntis messages forward --mark-only          # baseline: skip existing messages
  webuntis messages forward                      # send everything new
  webuntis messages forward 115070 --force       # (re)send one message
  webuntis messages forward --since 2026-09-01
  webuntis messages forward --watch 10m          # keep running`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.api()
			if err != nil {
				return err
			}
			f, err := normFolder(folder)
			if err != nil {
				return err
			}
			if f == "all" {
				return errors.New("forward works per folder (inbox or sent)")
			}
			if !dryRun && !markOnly {
				if err := mailer.Validate(c.Profile.SMTP); err != nil {
					return err
				}
			}
			var ids []int
			for _, arg := range args {
				id, err := strconv.Atoi(arg)
				if err != nil {
					return fmt.Errorf("invalid message id %q", arg)
				}
				ids = append(ids, id)
			}
			var sinceT time.Time
			if since != "" {
				if sinceT, err = dates.Parse(since); err != nil {
					return err
				}
			}
			run := func() error {
				return a.forwardOnce(ctx, c, f, ids, sinceT, dryRun, force, markOnly, noAttach, limit)
			}
			if watch <= 0 {
				return run()
			}
			if len(ids) > 0 {
				return errors.New("--watch cannot be combined with explicit IDs")
			}
			for {
				if err := run(); err != nil {
					fmt.Fprintf(os.Stderr, "%s forward failed: %v\n", time.Now().Format(time.RFC3339), err)
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(watch):
				}
			}
		},
	}
	f := cmd.Flags()
	f.StringVarP(&folder, "folder", "F", "inbox", "folder to forward (inbox or sent)")
	f.StringVar(&since, "since", "", "only messages sent on/after this date "+dateHelp)
	f.BoolVar(&dryRun, "dry-run", false, "show what would be sent")
	f.BoolVar(&force, "force", false, "send even if already forwarded")
	f.BoolVar(&markOnly, "mark-only", false, "mark messages as forwarded without sending")
	f.BoolVar(&noAttach, "no-attachments", false, "do not attach files (links only)")
	f.DurationVar(&watch, "watch", 0, "repeat every interval (e.g. 10m) until interrupted")
	f.IntVarP(&limit, "limit", "n", 0, "maximum number of messages to send per run")
	return cmd
}

func (a *app) forwardOnce(ctx context.Context, c *webuntis.Client, folder string, ids []int, since time.Time, dryRun, force, markOnly, noAttach bool, limit int) error {
	state, err := tracker.Load(c.Profile.Path("forwarded.json"))
	if err != nil {
		return err
	}
	var candidates []webuntis.MessageSummary
	if len(ids) > 0 {
		for _, id := range ids {
			candidates = append(candidates, webuntis.MessageSummary{ID: id, Folder: folder})
		}
	} else {
		list, err := c.Messages(ctx, folder)
		if err != nil {
			return err
		}
		// oldest first so mails arrive in order
		for i := len(list) - 1; i >= 0; i-- {
			m := list[i]
			if !since.IsZero() && m.Sent().Before(since) {
				continue
			}
			candidates = append(candidates, m)
		}
	}
	sent, skipped := 0, 0
	for _, m := range candidates {
		if !force && state.Has(folder, m.ID) {
			skipped++
			continue
		}
		if limit > 0 && sent >= limit {
			break
		}
		if markOnly {
			state.Mark(folder, m.ID)
			sent++
			continue
		}
		if dryRun {
			subj := m.Subject
			if subj == "" {
				subj = "(details not loaded)"
			}
			fmt.Fprintf(os.Stderr, "would forward %d  %s  %s\n", m.ID, m.Sent().Format("02.01.2006 15:04"), subj)
			sent++
			continue
		}
		d, err := c.Message(ctx, folder, m.ID)
		if err != nil {
			return err
		}
		var files []webuntis.DownloadedFile
		if !noAttach {
			if files, err = c.DownloadAttachments(ctx, d); err != nil {
				return fmt.Errorf("message %d: %w", m.ID, err)
			}
		} else {
			for _, s := range d.StorageAttachments {
				files = append(files, webuntis.DownloadedFile{Name: s.Name + " (nicht angehängt)"})
			}
		}
		webURL := c.Profile.BaseURL() + "/messages/" + map[string]string{webuntis.FolderInbox: "inbox", webuntis.FolderSent: "sent"}[folder]
		msg, err := mailer.Build(c.Profile.SMTP, c.Profile.School, webURL, d, files)
		if err != nil {
			return err
		}
		if err := mailer.Send(ctx, c.Profile.SMTP, msg); err != nil {
			return err
		}
		state.Mark(folder, m.ID)
		if err := state.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "forwarded %d  %s → %s\n", d.ID, d.Subject, strings.Join(c.Profile.SMTP.To, ", "))
		sent++
	}
	if markOnly {
		if err := state.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "marked %d message(s) as forwarded (%d already marked)\n", sent, skipped)
		return nil
	}
	verb := "forwarded"
	if dryRun {
		verb = "would forward"
	}
	fmt.Fprintf(os.Stderr, "%s %d message(s), %d already forwarded\n", verb, sent, skipped)
	return nil
}
