package main

import (
	"html"
	"html/template"
	"strings"
)

type logFilter struct {
	Query    string
	Severity string
}

type logBodyParts struct {
	severity string
	tag      string
	message  string
	raw      string
}

func matchesLogFilter(visible string, filter logFilter) bool {
	if filter.Severity != "" && !matchesSeverityFilter(visible, filter.Severity) {
		return false
	}
	if filter.Query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(visible), strings.ToLower(filter.Query))
}

func matchesSeverityFilter(visible, severity string) bool {
	return logSeverity(visible) == severity
}

func logSeverity(visible string) string {
	fields := strings.Fields(visible)
	if len(fields) < 3 {
		return ""
	}
	severity, ok := normalizeSeverityName(fields[2])
	if !ok {
		return ""
	}
	return severity
}

func splitLogLine(line string) (string, string) {
	sep := strings.Index(line, "  ")
	if sep < 0 {
		return line, ""
	}

	rest := visibleLogText(strings.TrimLeft(line[sep+2:], " "))
	firstSpace := strings.Index(rest, " ")
	if firstSpace < 0 {
		return rest, ""
	}

	timestamp := rest[:firstSpace]
	afterTimestamp := strings.TrimLeft(rest[firstSpace+1:], " ")
	secondSpace := strings.Index(afterTimestamp, " ")
	if secondSpace < 0 {
		return afterTimestamp + " " + timestamp, ""
	}

	host := afterTimestamp[:secondSpace]
	tail := afterTimestamp[secondSpace:]
	return host + " " + timestamp, tail
}

func logHeading(line string) string {
	head, _ := splitLogLine(line)
	return head
}

func renderLogBody(line string) template.HTML {
	_, tail := splitLogLine(line)
	parts := parseLogBody(tail)
	if parts.tag == "" {
		return template.HTML(html.EscapeString(tail))
	}

	var b strings.Builder
	b.WriteString(" ")
	if color := statusColor(parts.severity); color != "" {
		b.WriteString(`<span class="log-tag" style="color: `)
		b.WriteString(html.EscapeString(color))
		b.WriteString(`">`)
	} else {
		b.WriteString(`<span class="log-tag">`)
	}
	b.WriteString(html.EscapeString(parts.tag))
	b.WriteString(`</span>`)
	b.WriteString(html.EscapeString(parts.message))
	return template.HTML(b.String())
}

func logDevice(line string) string {
	sep := strings.Index(line, "  ")
	if sep < 0 {
		return ""
	}
	fields := strings.Fields(visibleLogText(line[sep+2:]))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func parseLogBody(tail string) logBodyParts {
	trimmed := strings.TrimLeft(tail, " ")
	if trimmed == "" {
		return logBodyParts{raw: tail}
	}

	firstSpace := strings.Index(trimmed, " ")
	if firstSpace < 0 {
		return logBodyParts{raw: tail}
	}

	severity, ok := normalizeSeverityName(trimmed[:firstSpace])
	if !ok {
		return logBodyParts{raw: tail}
	}

	rest := strings.TrimLeft(trimmed[firstSpace+1:], " ")
	if rest == "" {
		return logBodyParts{severity: severity, raw: tail}
	}

	secondSpace := strings.Index(rest, " ")
	if secondSpace < 0 {
		return logBodyParts{
			severity: severity,
			tag:      rest,
			raw:      tail,
		}
	}

	return logBodyParts{
		severity: severity,
		tag:      rest[:secondSpace],
		message:  rest[secondSpace:],
		raw:      tail,
	}
}
