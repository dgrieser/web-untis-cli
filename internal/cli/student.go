package cli

import (
	"strconv"
	"strings"

	ics "github.com/arran4/golang-ical"
	"github.com/spf13/cobra"

	"github.com/dgrieser/web-untis-cli/internal/dates"
	"github.com/dgrieser/web-untis-cli/internal/views"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

const dateHelp = "(2026-09-21, 21.09., today, -2w, monday …)"

func (a *app) absencesCmd() *cobra.Command {
	var dr dateRange
	var times, open bool
	cmd := &cobra.Command{
		Use:     "absences",
		Aliases: []string{"abwesenheiten", "abs"},
		Short:   "Reported absences (Meine Abwesenheiten); --times for Fehlzeiten",
		Example: `  webuntis absences
  webuntis absences --times            # missed lessons (Fehlzeiten) + summary
  webuntis absences --from 2025-08-01 --to 2026-07-31 -o ics`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if times {
				return a.runAbsenceTimes(cmd, dr, false, false)
			}
			ctx := cmd.Context()
			c, st, err := a.resolveStudent(ctx)
			if err != nil {
				return err
			}
			sy, ey := a.schoolYearRange(ctx, c)
			from, to, err := dr.resolve(sy, ey)
			if err != nil {
				return err
			}
			abs, err := c.Absences(ctx, st.ID, from, to)
			if err != nil {
				return err
			}
			if open {
				var f []webuntis.Absence
				for _, x := range abs {
					if !x.IsExcused {
						f = append(f, x)
					}
				}
				abs = f
			}
			return a.emit(abs, func() string { return views.Absences(st.Name, from, to, abs) },
				func() *ics.Calendar { return views.AbsencesICS(abs, c.Profile.School, st.Name) })
		},
	}
	dr.register(cmd, "start date "+dateHelp+" (default: school year start)", "end date (default: school year end)")
	cmd.Flags().BoolVar(&times, "times", false, "show missed lessons (Fehlzeiten) instead")
	cmd.Flags().BoolVar(&open, "open", false, "only absences that are not excused yet")
	return cmd
}

func (a *app) absenceTimesCmd() *cobra.Command {
	var dr dateRange
	var noAbs, noLate bool
	cmd := &cobra.Command{
		Use:     "absence-times",
		Aliases: []string{"fehlzeiten", "missed"},
		Short:   "Missed lessons (Fehlzeiten) with totals per subject",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runAbsenceTimes(cmd, dr, noAbs, noLate)
		},
	}
	dr.register(cmd, "start date "+dateHelp+" (default: school year start)", "end date (default: school year end)")
	cmd.Flags().BoolVar(&noAbs, "exclude-absences", false, "exclude absences (only late arrivals)")
	cmd.Flags().BoolVar(&noLate, "exclude-lateness", false, "exclude late arrivals")
	return cmd
}

func (a *app) runAbsenceTimes(cmd *cobra.Command, dr dateRange, noAbs, noLate bool) error {
	ctx := cmd.Context()
	c, st, err := a.resolveStudent(ctx)
	if err != nil {
		return err
	}
	sy, ey := a.schoolYearRange(ctx, c)
	from, to, err := dr.resolve(sy, ey)
	if err != nil {
		return err
	}
	times, err := c.AbsenceTimes(ctx, st.ID, from, to, noAbs, noLate)
	if err != nil {
		return err
	}
	data := struct {
		Student webuntis.Student         `json:"student" yaml:"student"`
		Summary views.AbsenceTimeSummary `json:"summary" yaml:"summary"`
		Times   []webuntis.AbsenceTime   `json:"absenceTimes" yaml:"absenceTimes"`
	}{st, views.SummarizeAbsenceTimes(times), times}
	return a.emit(data, func() string { return views.AbsenceTimes(st.Name, from, to, times) }, nil)
}

func (a *app) homeworkCmd() *cobra.Command {
	var dr dateRange
	var open bool
	cmd := &cobra.Command{
		Use:     "homework",
		Aliases: []string{"hw", "hausaufgaben"},
		Short:   "Homework (assigned in the date range, sorted by due date)",
		Example: `  webuntis homework
  webuntis homework --open
  webuntis hw --from -4w --to +2w -o ics > hausaufgaben.ics`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, st, err := a.resolveStudent(ctx)
			if err != nil {
				return err
			}
			today := dates.Today()
			from, to, err := dr.resolve(today.AddDate(0, 0, -14), today.AddDate(0, 0, 14))
			if err != nil {
				return err
			}
			hw, err := c.Homework(ctx, st.ID, from, to)
			if err != nil {
				return err
			}
			if open {
				var f []webuntis.Homework
				for _, h := range hw {
					if !h.Completed {
						f = append(f, h)
					}
				}
				hw = f
			}
			return a.emit(hw, func() string { return views.Homework(st.Name, from, to, hw) },
				func() *ics.Calendar { return views.HomeworkICS(hw, c.Profile.School, st.Name) })
		},
	}
	dr.register(cmd, "assigned from "+dateHelp+" (default: -2 weeks)", "assigned until (default: +2 weeks)")
	cmd.Flags().BoolVar(&open, "open", false, "only homework not marked as completed")
	return cmd
}

func (a *app) classRegCmd() *cobra.Command {
	var dr dateRange
	cmd := &cobra.Command{
		Use:     "class-register",
		Aliases: []string{"classreg", "entries", "klassenbuch"},
		Short:   "Class register entries (Klassenbucheinträge)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, st, err := a.resolveStudent(ctx)
			if err != nil {
				return err
			}
			sy, ey := a.schoolYearRange(ctx, c)
			from, to, err := dr.resolve(sy, ey)
			if err != nil {
				return err
			}
			ev, err := c.ClassRegEvents(ctx, st.ID, from, to)
			if err != nil {
				return err
			}
			return a.emit(ev, func() string { return views.ClassRegEvents(st.Name, from, to, ev) }, nil)
		},
	}
	dr.register(cmd, "start date "+dateHelp+" (default: school year start)", "end date (default: school year end)")
	return cmd
}

func (a *app) classServicesCmd() *cobra.Command {
	var dr dateRange
	cmd := &cobra.Command{
		Use:     "class-services",
		Aliases: []string{"services", "dienste"},
		Short:   "Class services / duties (Dienste)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, st, err := a.resolveStudent(ctx)
			if err != nil {
				return err
			}
			mon := dates.Monday(dates.Today())
			from, to, err := dr.resolve(mon, mon.AddDate(0, 0, 6))
			if err != nil {
				return err
			}
			cs, err := c.ClassServices(ctx, st.ID, from, to)
			if err != nil {
				return err
			}
			return a.emit(cs, func() string { return views.ClassServices(st.Name, from, to, cs) }, nil)
		},
	}
	dr.register(cmd, "start date "+dateHelp+" (default: this Monday)", "end date (default: this Sunday)")
	return cmd
}

func (a *app) examsCmd() *cobra.Command {
	var dr dateRange
	var all bool
	cmd := &cobra.Command{
		Use:     "exams",
		Aliases: []string{"pruefungen", "prüfungen", "tests"},
		Short:   "Exams (Prüfungen) incl. grades if published",
		Example: `  webuntis exams
  webuntis exams --all
  webuntis exams -o ics > pruefungen.ics`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, st, err := a.resolveStudent(ctx)
			if err != nil {
				return err
			}
			sy, ey := a.schoolYearRange(ctx, c)
			defFrom := dates.Today()
			if all {
				defFrom = sy
			}
			from, to, err := dr.resolve(defFrom, ey)
			if err != nil {
				return err
			}
			ex, err := c.Exams(ctx, st.ID, from, to)
			if err != nil {
				return err
			}
			return a.emit(ex, func() string { return views.Exams(st.Name, from, to, ex) },
				func() *ics.Calendar { return views.ExamsICS(ex, c.Profile.School, st.Name) })
		},
	}
	dr.register(cmd, "start date "+dateHelp+" (default: today)", "end date (default: school year end)")
	cmd.Flags().BoolVar(&all, "all", false, "whole school year (incl. past exams)")
	return cmd
}

func (a *app) exemptionsCmd() *cobra.Command {
	var dr dateRange
	cmd := &cobra.Command{
		Use:     "exemptions",
		Aliases: []string{"befreiungen"},
		Short:   "Exemptions (Befreiungen)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, st, err := a.resolveStudent(ctx)
			if err != nil {
				return err
			}
			sy, ey := a.schoolYearRange(ctx, c)
			from, to, err := dr.resolve(sy, ey)
			if err != nil {
				return err
			}
			rows, err := c.Exemptions(ctx, st.ID, from, to)
			if err != nil {
				return err
			}
			return a.emit(rows, func() string { return views.Exemptions(st.Name, from, to, rows) }, nil)
		},
	}
	dr.register(cmd, "start date "+dateHelp+" (default: school year start)", "end date (default: school year end)")
	return cmd
}

func (a *app) contactHoursCmd() *cobra.Command {
	var date, class string
	var listClasses bool
	cmd := &cobra.Command{
		Use:     "contact-hours",
		Aliases: []string{"office-hours", "sprechstunden"},
		Short:   "Teachers' contact hours (Sprechstunden) of a week",
		Example: `  webuntis contact-hours
  webuntis contact-hours --date next-week --class "S I"
  webuntis contact-hours --classes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.api()
			if err != nil {
				return err
			}
			if listClasses {
				cls, err := c.OfficeHourClasses(ctx)
				if err != nil {
					return err
				}
				return a.emit(cls, func() string {
					var rows [][]string
					for _, k := range cls {
						rows = append(rows, []string{strconv.Itoa(k.ID), k.Label})
					}
					return "## Klassen\n\n" + mdTable([]string{"ID", "Klasse"}, rows)
				}, nil)
			}
			day, err := dates.Parse(date)
			if err != nil {
				return err
			}
			klasseID, className := -1, ""
			if class != "" {
				cls, err := c.OfficeHourClasses(ctx)
				if err != nil {
					return err
				}
				for _, k := range cls {
					if strings.EqualFold(k.Label, class) || strconv.Itoa(k.ID) == class {
						klasseID, className = k.ID, k.Label
					}
				}
				if klasseID == -1 {
					return errNotFound("class", class, func() []string {
						var n []string
						for _, k := range cls {
							n = append(n, k.Label)
						}
						return n
					}())
				}
			}
			oh, err := c.OfficeHours(ctx, day, klasseID)
			if err != nil {
				return err
			}
			return a.emit(oh, func() string { return views.OfficeHours(oh, className) }, nil)
		},
	}
	cmd.Flags().StringVarP(&date, "date", "d", "", "date within the week "+dateHelp)
	cmd.Flags().StringVarP(&class, "class", "c", "", "filter by class (see --classes)")
	cmd.Flags().BoolVar(&listClasses, "classes", false, "list classes usable with --class")
	return cmd
}
