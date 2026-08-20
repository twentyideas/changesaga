package saga

import "strings"

// MarkdownHeading describes the small heading subset supported by the
// reference renderer. Explicit anchors use: ## Heading {#stable-anchor}.
type MarkdownHeading struct {
	Level    int
	Text     string
	Anchor   string
	Explicit bool
}

func ParseMarkdownHeading(line string) (MarkdownHeading, bool) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	if level == 0 || len(trimmed) <= level || trimmed[level] != ' ' {
		return MarkdownHeading{}, false
	}
	heading := MarkdownHeading{Level: level, Text: strings.TrimSpace(trimmed[level:])}
	if marker := strings.LastIndex(heading.Text, " {#"); marker >= 0 && strings.HasSuffix(heading.Text, "}") {
		heading.Anchor = heading.Text[marker+3 : len(heading.Text)-1]
		heading.Text = strings.TrimSpace(heading.Text[:marker])
		heading.Explicit = true
	}
	return heading, true
}

func ValidMarkdownAnchor(value string) bool {
	if value == "" || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				if character != '-' {
					return false
				}
			}
		}
	}
	return true
}

func MarkdownHeadings(source string) []MarkdownHeading {
	var headings []MarkdownHeading
	inCode := false
	for _, line := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if heading, ok := ParseMarkdownHeading(line); ok {
			headings = append(headings, heading)
		}
	}
	return headings
}
