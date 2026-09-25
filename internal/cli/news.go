package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/web-untis-cli/internal/dates"
	"github.com/dgrieser/web-untis-cli/internal/mailer"
	"github.com/dgrieser/web-untis-cli/internal/tracker"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

const newsKind = "news"

// applySeen flags news items that were not shown before as New, optionally
// keeps only those, and records all returned items as seen (unless peek).
func (a *app) applySeen(c *webuntis.Client, items []webuntis.MessageOfDay, onlyNew, peek, withHTML bool) ([]webuntis.MessageOfDay, error) {
	seen, err := tracker.Load(c.Profile.Path("news-seen.json"))
	if err != nil {
		return nil, err
	}
	out := []webuntis.MessageOfDay{}
	for _, m := range items {
		m.New = !seen.Has(newsKind, m.ID)
		m.IncludeHTML = withHTML
		if onlyNew && !m.New {
			continue
		}
		out = append(out, m)
	}
	if peek {
		return out, nil
	}
	for _, m := range out {
		seen.Mark(newsKind, m.ID)
	}
	return out, seen.Save()
}

func (a *app) newsForwardCmd() *cobra.Command {
	var date string
	var dryRun, force, markOnly bool
	var watch time.Duration
	var limit int
	cmd := &cobra.Command{
		Use:   "forward [ID...]",
		Short: "Forward messages of the day to an e-mail address via SMTP",
		Long: `Sends each message of the day ("Heute → Nachrichten") once as e-mail
(original HTML + plain text, attachment links). Already forwarded items are
tracked in <profile>/news-forwarded.json.

Run it regularly (cron, systemd timer or --watch) to get new news by e-mail.
SMTP settings: "webuntis config smtp". Does not affect the 🆕 "seen" state.`,
		Example: `  webuntis news forward --dry-run
  webuntis news forward --mark-only     # skip the current news
  webuntis news forward                 # send all not yet forwarded
  webuntis news forward 371 --force     # (re)send one item
  webuntis news forward --watch 30m`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.api()
			if err != nil {
				return err
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
					return fmt.Errorf("invalid news id %q", arg)
				}
				ids = append(ids, id)
			}
			run := func() error {
				day, err := dates.Parse(date) // re-evaluated each round so --watch follows the date
				if err != nil {
					return err
				}
				return a.forwardNewsOnce(ctx, c, day, ids, dryRun, force, markOnly, limit)
			}
			if watch <= 0 {
				return run()
			}
			if len(ids) > 0 {
				return errors.New("--watch cannot be combined with explicit IDs")
			}
			for {
				if err := run(); err != nil {
					fmt.Fprintf(os.Stderr, "%s news forward failed: %v\n", time.Now().Format(time.RFC3339), err)
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
	f.StringVarP(&date, "date", "d", "", "day whose news to forward (default today)")
	f.BoolVar(&dryRun, "dry-run", false, "show what would be sent")
	f.BoolVar(&force, "force", false, "send even if already forwarded")
	f.BoolVar(&markOnly, "mark-only", false, "mark news as forwarded without sending")
	f.DurationVar(&watch, "watch", 0, "repeat every interval (e.g. 30m) until interrupted")
	f.IntVarP(&limit, "limit", "n", 0, "maximum number of items to send per run")
	return cmd
}

func (a *app) forwardNewsOnce(ctx context.Context, c *webuntis.Client, day time.Time, ids []int, dryRun, force, markOnly bool, limit int) error {
	state, err := tracker.Load(c.Profile.Path("news-forwarded.json"))
	if err != nil {
		return err
	}
	news, err := c.News(ctx, day)
	if err != nil {
		return err
	}
	items := news.MessagesOfDay
	if len(ids) > 0 {
		var sel []webuntis.MessageOfDay
		for _, id := range ids {
			found := false
			for _, m := range items {
				if m.ID == id {
					sel, found = append(sel, m), true
				}
			}
			if !found {
				return fmt.Errorf("news %d not found on %s", id, dates.Human(day))
			}
		}
		items = sel
	}
	sent, skipped := 0, 0
	for _, m := range items {
		if !force && state.Has(newsKind, m.ID) {
			skipped++
			continue
		}
		if limit > 0 && sent >= limit {
			break
		}
		switch {
		case markOnly:
			state.Mark(newsKind, m.ID)
		case dryRun:
			fmt.Fprintf(os.Stderr, "would forward news %d  %s\n", m.ID, m.Subject)
		default:
			msg, err := mailer.BuildNews(c.Profile.SMTP, c.Profile.School, c.Profile.SchoolDisplayName, c.Profile.BaseURL()+"/today", m)
			if err != nil {
				return err
			}
			if err := mailer.Send(ctx, c.Profile.SMTP, msg); err != nil {
				return err
			}
			state.Mark(newsKind, m.ID)
			if err := state.Save(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "forwarded news %d  %s\n", m.ID, m.Subject)
		}
		sent++
	}
	switch {
	case markOnly:
		if err := state.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "marked %d news item(s) as forwarded (%d already marked)\n", sent, skipped)
	case dryRun:
		fmt.Fprintf(os.Stderr, "would forward %d news item(s), %d already forwarded\n", sent, skipped)
	default:
		fmt.Fprintf(os.Stderr, "forwarded %d news item(s), %d already forwarded\n", sent, skipped)
	}
	return nil
}
