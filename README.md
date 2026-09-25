# webuntis-cli

Read-only [WebUntis](https://webuntis.com) client for the terminal. Works with any school.

## Setup

1. **Install** – download a binary from the [releases](https://github.com/dgrieser/web-untis-cli/releases)
   (Linux amd64 / arm64 / armv7, macOS arm64) or build it:

   ```sh
   go install github.com/dgrieser/web-untis-cli/cmd/webuntis@latest
   ```

2. **Log in** – the wizard asks for profile, school (live search), username, password and default student:

   ```sh
   webuntis setup
   ```

   Non-interactive: `echo "$PW" | webuntis login ge-huellhorst --user me@example.com --password-stdin`

3. **Shell completion** (optional): `source <(webuntis completion bash)` (also zsh, fish).

4. **Try it:** `webuntis today`

## Features

| Command | Shows |
| --- | --- |
| `today [--agenda] [--new]` | Heute → Nachrichten, last login/import, unread counts; `--agenda` adds today's lessons, homework, exams |
| `news [N] [--new]` | Messages of the day in full; 🆕 marks unseen items |
| `news forward` | E-mails each news item once via SMTP |
| `messages [inbox\|sent\|drafts]` | Mitteilungen; `show ID`, `attachments ID`, `--search`, `--unread` |
| `messages forward` | E-mails messages (with attachments + history) via SMTP |
| `timetable [DATE]` | Student timetable as week grid; `--class [NAME]`, `--day`, `--days N`, `--list` |
| `absences` | Reported absences; `--open` for unexcused |
| `absence-times` | Fehlzeiten with totals per subject |
| `homework [--open]` | Homework by due date |
| `class-register` | Klassenbucheinträge |
| `class-services` | Dienste |
| `exams [--all]` | Prüfungen incl. grades |
| `exemptions` | Befreiungen |
| `contact-hours` | Sprechstunden |
| `klassengeld [-T]` | Klassengeld addin: balance, payments, transactions |
| `addins`, `addins open NAME` | Addins; open/download link addins (e.g. PDF, `--text`) |
| `students`, `status` | Children of the account, login status |
| `schools search Q` | Public school directory |
| `profiles`, `config`, `cache clear` | Multiple accounts, SMTP/default student/time zone, cache |
| `api get PATH`, `api rpc METHOD` | Raw read-only API access |

**Common flags**

- `-o pretty|markdown|json|yaml|ics`: pretty is the default on a terminal, markdown when piped. `ics` works for timetable, exams, homework and absences.
- `-s NAME`: pick the student when the account has several children.
- `-p PROFILE`: use a different account or school.
- `-r`: bypass the cache.
- `--debug`: log HTTP requests.

**Dates:** `2026-09-21`, `21.09.`, `today`, `morgen`, `monday`, `+1w`, `next-week`.

Opening an unread message marks it as read, just like in the web UI. Nothing else is written to WebUntis.

## Forwarding via SMTP

```sh
webuntis config smtp --host smtp.example.com --user me@example.com --password-prompt \
    --from me@example.com --to me@example.com   # --security starttls|tls|opportunistic|none
webuntis messages forward --mark-only           # skip existing messages once
webuntis messages forward --watch 10m           # or run from cron; same for `news forward`
```

Already forwarded items are tracked, so every item is sent only once. `--dry-run` previews and `--force` resends.

## Storage

Everything lives in `~/.cache/webuntis-cli/<profile>/` (override with `$WEBUNTIS_CLI_HOME`):

- `config.json`: school, credentials, SMTP settings (file mode 0600)
- `session.json`: the login session
- `cache/`: response cache
- `forwarded.json`, `news-seen.json`, `news-forwarded.json`: already sent and already seen items

The password is stored so the tool can log in again when the session expires. `--no-store-password` or `$WEBUNTIS_PASSWORD` avoids that.

Environment variables: `WEBUNTIS_PROFILE`, `WEBUNTIS_OUTPUT`, `WEBUNTIS_STYLE`, `WEBUNTIS_FORM_THEME`, `WEBUNTIS_SMTP_PASSWORD`.

## How it works

There is no public API for end users. The official JSON-RPC API (`/WebUntis/jsonrpc.do`) is partner-documented and limited. This tool logs in via JSON-RPC and then uses the same JSON endpoints as the web UI:

- `/WebUntis/api/token/new`: returns a Bearer JWT used for `/api/rest/view/v1/*` (timetable, messages, app data).
- `/api/classreg/*`, `/api/homeworks/lessons`, `/api/exams`, `/api/public/news|officehours/*`: these use the session cookie.
- Klassengeld: logs in via WebUntis single sign-on and reads the klassengeld.app pages.

See `internal/webuntis/`. These APIs are undocumented and may change.

## Development

```sh
make test lint build
goreleaser release --snapshot --clean --skip=publish   # local release dry run
```

CI runs tests (incl. 32-bit), lint and a snapshot build. Pushing a tag `vX.Y.Z` publishes a release.
