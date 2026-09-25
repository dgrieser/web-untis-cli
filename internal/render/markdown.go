package render

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// Doc is a small Markdown builder.
type Doc struct{ b strings.Builder }

// H adds a heading of the given level.
func (d *Doc) H(level int, format string, args ...any) *Doc {
	fmt.Fprintf(&d.b, "%s %s\n\n", strings.Repeat("#", level), fmt.Sprintf(format, args...))
	return d
}

// P adds a paragraph of markdown.
func (d *Doc) P(s string) *Doc {
	d.b.WriteString(strings.TrimRight(s, "\n") + "\n\n")
	return d
}

// Pf adds a formatted paragraph.
func (d *Doc) Pf(format string, args ...any) *Doc {
	return d.P(fmt.Sprintf(format, args...))
}

// Line adds a single line (no blank line after).
func (d *Doc) Line(format string, args ...any) *Doc {
	fmt.Fprintf(&d.b, format+"\n", args...)
	return d
}

// Raw adds raw markdown.
func (d *Doc) Raw(s string) *Doc {
	d.b.WriteString(s)
	return d
}

// Bullets adds a list.
func (d *Doc) Bullets(items ...string) *Doc {
	for _, it := range items {
		if it != "" {
			d.b.WriteString("- " + it + "\n")
		}
	}
	d.b.WriteString("\n")
	return d
}

// KV adds a list of "**key:** value" lines, skipping empty values.
func (d *Doc) KV(pairs ...string) *Doc {
	for i := 0; i+1 < len(pairs); i += 2 {
		if strings.TrimSpace(pairs[i+1]) == "" {
			continue
		}
		fmt.Fprintf(&d.b, "- **%s:** %s\n", pairs[i], pairs[i+1])
	}
	d.b.WriteString("\n")
	return d
}

// Table adds a Markdown table. Cells are escaped.
func (d *Doc) Table(headers []string, rows [][]string) *Doc {
	d.b.WriteString(Table(headers, rows))
	d.b.WriteString("\n")
	return d
}

// Rule adds a horizontal rule.
func (d *Doc) Rule() *Doc {
	d.b.WriteString("---\n\n")
	return d
}

// Empty adds an italic "nothing found" hint.
func (d *Doc) Empty(msg string) *Doc {
	return d.P("_" + msg + "_")
}

func (d *Doc) String() string { return d.b.String() }

// Table renders a GitHub flavored Markdown table.
func Table(headers []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("|")
	for _, h := range headers {
		b.WriteString(" " + Cell(h) + " |")
	}
	b.WriteString("\n|")
	for range headers {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString("|")
		for i := range headers {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			b.WriteString(" " + CellMD(v) + " |")
		}
		b.WriteString("\n")
	}
	return b.String()
}

var mdSpecial = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`,
	"<", `\<`, ">", `\>`, "#", `\#`, "|", `\|`, "~", `\~`,
)

// Esc escapes Markdown special characters in plain text.
func Esc(s string) string { return mdSpecial.Replace(s) }

// Cell escapes a plain text value for use in a table cell.
func Cell(s string) string { return CellMD(Esc(s)) }

// CellMD makes an already formatted markdown snippet table-safe.
func CellMD(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, `\\|`, `\|`)
	return strings.TrimSpace(s)
}

// Text converts plain text (e.g. a message body) to Markdown keeping line
// breaks and e-mail style "> " quotes.
func Text(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	prevQuote, prevBlank := false, true
	for i, raw := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(raw)
		quote := strings.HasPrefix(trimmed, ">")
		if trimmed == "" {
			if !prevBlank {
				b.WriteString("\n\n")
			}
			prevBlank, prevQuote = true, false
			continue
		}
		if i > 0 && !prevBlank {
			if quote != prevQuote {
				b.WriteString("\n\n") // start/end a quote block
			} else {
				b.WriteString("  \n") // hard line break
			}
		}
		if quote {
			b.WriteString("> " + Esc(strings.TrimSpace(strings.TrimLeft(trimmed, ">"))))
		} else {
			b.WriteString(Esc(raw))
		}
		prevBlank, prevQuote = false, quote
	}
	return strings.TrimSpace(b.String())
}

var htmlTagRe = regexp.MustCompile(`(?i)<\s*(p|div|br|font|span|b|i|u|strong|em|ul|ol|li|a|img|table|h\d)\b`)

// LooksLikeHTML reports whether s contains HTML markup.
func LooksLikeHTML(s string) bool { return htmlTagRe.MatchString(s) }

// HTML converts HTML (message of the day, rich messages) to Markdown.
func HTML(s string) string {
	md, err := htmltomd.ConvertString(s)
	if err != nil {
		return Text(StripTags(s))
	}
	return tidyMarkdown(md)
}

// Body converts either HTML or plain text to Markdown.
func Body(s string) string {
	if LooksLikeHTML(s) {
		return HTML(s)
	}
	return Text(s)
}

var (
	trailingBreakRe = regexp.MustCompile(` +\n\n`)
	manyNewlinesRe  = regexp.MustCompile(`\n{3,}`)
)

// tidyMarkdown removes whitespace-only lines and dangling hard breaks that
// HTML with lots of <br> produces.
func tidyMarkdown(md string) string {
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = ""
		}
	}
	md = strings.Join(lines, "\n")
	md = trailingBreakRe.ReplaceAllString(md, "\n\n")
	md = manyNewlinesRe.ReplaceAllString(md, "\n\n")
	return strings.TrimSpace(md)
}

var (
	tagRe      = regexp.MustCompile(`<[^>]*>`)
	blockTagRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>|</h\d>|</tr>`)
	liOpenRe   = regexp.MustCompile(`(?i)<li[^>]*>`)
	hSpaceRe   = regexp.MustCompile(`[ \t\x{00a0}]+`)
)

// PlainText converts HTML (or plain text) into clean plain text: line
// breaks for block elements, no tags, entities decoded, no trailing spaces.
func PlainText(s string) string {
	if LooksLikeHTML(s) {
		s = liOpenRe.ReplaceAllString(s, "\n- ")
		s = blockTagRe.ReplaceAllString(s, "\n")
		s = tagRe.ReplaceAllString(s, "")
		s = html.UnescapeString(s)
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(hSpaceRe.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	s = manyNewlinesRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// StripTags removes HTML tags.
func StripTags(s string) string {
	s = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>`).ReplaceAllString(s, "\n")
	return strings.TrimSpace(tagRe.ReplaceAllString(s, ""))
}

// Truncate shortens s to n runes.
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Check renders a boolean as a check mark.
func Check(b bool) string {
	if b {
		return "✓"
	}
	return ""
}

// SortedKeys returns map keys sorted.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
