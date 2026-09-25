# webuntis-cli

A fast, **read-only** command line client for [WebUntis](https://webuntis.com)
written in Go. It works with any WebUntis school and was built against
[GES Hüllhorst](https://ge-huellhorst.webuntis.com/today) (parent account).

```
webuntis today --agenda          # news of the day + today's lessons, homework, exams
webuntis timetable               # colored week grid of the student
webuntis tt --class next-week    # class timetable
webuntis messages                # inbox
webuntis messages forward        # new messages → your mailbox via SMTP
webuntis absences --times        # Fehlzeiten with totals per subject
webuntis klassengeld -T          # balance, payments and bookings of klassengeld.app
```

## Features

| WebUntis page | Command | Formats |
| --- | --- | --- |
| Heute → Nachrichten (`/today`) | `today`, `news` | pretty, md, json, yaml |
| Mitteilungen (`/messages/inbox`, sent, drafts) | `messages [list\|show\|attachments\|forward\|inbox\|sent\|drafts]` | pretty, md, json, yaml |
| Mein Stundenplan (`/timetable/my-student`) | `timetable` | pretty grid, md, json, yaml, **ics** |
| Klassenstundenplan (`/timetable/class`) | `timetable --class [NAME]` | pretty grid, md, json, yaml, **ics** |
| Abwesenheiten (`/student-absences`) | `absences` | pretty, md, json, yaml, **ics** |
| Fehlzeiten (tab on `/student-absences`) | `absences --times`, `absence-times` | pretty, md, json, yaml |
| Hausaufgaben (`/student-homework`) | `homework` | pretty, md, json, yaml, **ics** |
| Klassenbucheinträge | `class-register` | pretty, md, json, yaml |
| Dienste | `class-services` | pretty, md, json, yaml |
| Prüfungen | `exams` | pretty, md, json, yaml, **ics** |
| Befreiungen | `exemptions` | pretty, md, json, yaml |
| Sprechstunden (`/timetable-contact-hours`) | `contact-hours` | pretty, md, json, yaml |
| Addin Klassengeld | `klassengeld` | pretty, md, json, yaml |
| Addin custom links (e.g. "Namen eingeben") | `addins`, `addins open NAME` | pretty, md, json, yaml |

Plus: `setup` (interactive wizard), `status`, `students`, `schools search`, `profiles`, `config`, `cache clear`,
`api get` / `api rpc` (raw read-only API access), shell `completion`.

Everything is read-only. The only server-side side effect is the same as in the
web UI: opening an unread message marks it as read.

## Install

```sh
go install github.com/dgrieser/web-untis-cli/cmd/webuntis@latest
# or from a checkout
make install        # go install ./cmd/webuntis
```

## Getting started

```sh
webuntis setup      # interactive wizard (same as `webuntis login` without arguments)
webuntis status
webuntis today
```

The wizard lets you pick an existing profile or create a new one, search the
school with live results from the WebUntis school directory (name, city, login
name or URL), enter username/password, choose whether to store the password and
pick the default student (parents with several children).

Non-interactive (scripts):

```sh
webuntis login https://ge-huellhorst.webuntis.com --user me@example.com   # prompts for the password
echo "$PASSWORD" | webuntis login ge-huellhorst --user me@example.com --password-stdin
webuntis schools search hüllhorst                                          # find a school's login name
```

Parents with several children select one with `--student <name|id>` or set a
default: `webuntis config set student Liana`.

### Shell completion

```sh
source <(webuntis completion bash)     # or zsh / fish / powershell
```

Completes school names (`webuntis login hüll<TAB>`), profiles (`--profile`,
`profiles use`), students (`--student`), classes (`timetable --class=<TAB>`),
addins (`addins open`), folders, output formats and styles.

## Output

* `pretty` (default on a terminal): Markdown rendered with
  [glamour](https://github.com/charmbracelet/glamour); the timetable is a colored
  [lipgloss](https://github.com/charmbracelet/lipgloss) grid. Style via
  `--style dark|light|dracula|tokyo-night|notty|…` or `$WEBUNTIS_STYLE`.
* `-o markdown` – raw Markdown (default when stdout is not a terminal).
* `-o json`, `-o yaml` – normalized data for scripting.
* `-o ics` – iCalendar for timetable, exams, homework and absences:

```sh
webuntis timetable --days 28 -o ics > stundenplan.ics
webuntis exams -o ics > pruefungen.ics
```

Dates accept `2026-09-21`, `21.09.2026`, `21.09.`, `today`, `morgen`, `monday`,
`+1w`, `-3d`, `next-week` …

## Forwarding messages via SMTP

```sh
webuntis config smtp --host smtp.example.com --user me@example.com --password-prompt \
    --from me@example.com --to me@example.com          # security: starttls (default), tls, opportunistic, none
webuntis messages forward --dry-run
webuntis messages forward --mark-only                  # baseline: don't send existing messages
webuntis messages forward                              # send all new ones (incl. attachments + history)
webuntis messages forward --watch 10m                  # or run from cron / a systemd timer
```

Forwarded messages are tracked in `<profile>/forwarded.json`; mails get a stable
`Message-ID` (`webuntis.<school>.<folder>.<id>@webuntis-cli`), the original date,
a text + HTML body and the attachments. `$WEBUNTIS_SMTP_PASSWORD` overrides the
stored SMTP password.

## Storage and caching

Default directory: `~/.cache/webuntis-cli` (`$XDG_CACHE_HOME`, override with
`$WEBUNTIS_CLI_HOME`):

```
current                      active profile name
<profile>/config.json        server, school, username, password, SMTP (mode 0600)
<profile>/session.json       session cookies + JWT (reused between runs)
<profile>/cache/             HTTP response cache
<profile>/forwarded.json     forwarded message ids
```

The password is stored in plain text (file mode 0600) so that expired sessions
can be renewed automatically; use `login --no-store-password` (or
`$WEBUNTIS_PASSWORD`) to avoid that.

Cache TTLs: master data (school years, app data, filters) hours; timetable 5 min
(12 h for the past); messages 1 min; message details forever (immutable);
Klassengeld 10 min. `--refresh` bypasses the cache, `webuntis cache clear` empties it.

Multiple schools/accounts: `webuntis login -p kid2 …`, `webuntis profiles`,
`webuntis profiles use kid2`, or `--profile kid2` / `$WEBUNTIS_PROFILE`.

## How it works – API research

**Official API.** Untis offers a JSON-RPC API (`/WebUntis/jsonrpc.do?school=…`,
methods like `authenticate`, `getTimetable`, `getSubjects`, `getHomeworks`,
`getLatestImportTime`). Its documentation is only handed out to partners and it
does not cover messages, the "Heute" news, class register details or addins.
The newer *Untis Platform* APIs require a registered partner application. There
is no public, documented API for end users.

**What the web UI uses** (reverse engineered from the web app, see
`internal/webuntis`):

| Purpose | Endpoint |
| --- | --- |
| Login | JSON-RPC `authenticate` → `JSESSIONID` cookie (fallback: form post `/WebUntis/j_spring_security_check`) |
| JWT for REST | `GET /WebUntis/api/token/new` (cookie) → Bearer token for `/api/rest/view/*` |
| Bootstrap | `/api/rest/view/v1/app/data`, `/api/rest/view/v1/schoolyears` |
| Heute → Nachrichten | `/api/public/news/newsWidgetData?date=YYYYMMDD`, `/api/rest/view/v1/dashboard/cards` |
| Messages | `/api/rest/view/v1/messages`, `…/messages/sent`, `…/messages/drafts`, `…/messages/{id}`, `…/messages/{storageId}/attachmentstorageurl`, `…/messages/{id}/attachment` |
| Timetable | `/api/rest/view/v1/timetable/entries?start&end&resourceType&resources&timetableType&format=1`, `…/timetable/filter` |
| Absences / Fehlzeiten | `/api/classreg/absences/students`, `/api/classreg/absencetimes/student` |
| Homework | `/api/homeworks/lessons?startDate&endDate` |
| Class register | `/api/classreg/classregevents` |
| Class services | `/api/classreg/classservices` |
| Exams | `/api/exams?studentId&withGrades=true` |
| Exemptions | `/api/classreg/exemptions` |
| Contact hours | `/api/public/officehours/{hours,classes,settings,registrations}` |
| Addins | `/api/rest/view/v1/app/platform-application/menus`; SSO apps use WebUntis OAuth (`/WebUntis/api/sso/…/authorize`) |
| Klassengeld | `klassengeld.app/login_untis` (SSO) → `/dashboard` + `/ajax/saldo_account/{id}` (HTML with embedded grid data) |
| School search | `https://mobile.webuntis.com/ms/schoolquery2` (`searchSchool`) |

Dates in `/api/…` endpoints are integers (`20260921`, times `745`). The
`webuntis api get <path>` command lets you explore further endpoints with your
session, `webuntis api rpc <getMethod>` calls the official JSON-RPC API.

These are undocumented APIs and may change without notice.

## Development

```sh
make test     # go test ./...
make build    # ./bin/webuntis
webuntis --debug …   # log HTTP requests (secrets redacted)
```
