package server

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestDarkPaletteTextRolesMeetWCAGContrast(t *testing.T) {
	tokens := cssCustomProperties(darkTokens)
	pairs := []struct {
		name       string
		foreground string
		background string
	}{
		{"warning", "--ink", "--warning-bg"},
		{"danger", "--ink", "--danger-bg"},
		{"landmark", "--muted", "--landmark-bg"},
		{"primary button", "--primary-ink", "--primary-bg"},
		{"primary button hover", "--primary-ink", "--primary-hover"},
		{"copied feedback", "--copied-ink", "--copied-bg"},
		{"suggestion", "--suggestion-ink", "--add-bg"},
		{"added gutter", "--faint", "--add-gutter"},
		{"deleted gutter", "--faint", "--del-gutter"},
		{"approve control", "--approve-ink", "--approve-bg"},
		{"reject control", "--reject-ink", "--reject-bg"},
		{"syntax keyword", "--syntax-keyword", "--code-bg"},
		{"syntax string", "--syntax-string", "--code-bg"},
		{"syntax number", "--syntax-number", "--code-bg"},
		{"syntax comment", "--syntax-comment", "--code-bg"},
		{"syntax type", "--syntax-type", "--code-bg"},
		{"syntax property", "--syntax-property", "--code-bg"},
		{"syntax punctuation", "--syntax-punctuation", "--code-bg"},
	}
	for _, pair := range pairs {
		ratio := contrastRatio(t, tokens[pair.foreground], tokens[pair.background])
		if ratio < 4.5 {
			t.Errorf("%s contrast is %.2f:1; want at least 4.5:1", pair.name, ratio)
		}
	}
}

func cssCustomProperties(block string) map[string]string {
	properties := map[string]string{}
	for declaration := range strings.SplitSeq(block, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(declaration), ":")
		if ok && strings.HasPrefix(name, "--") {
			properties[name] = strings.TrimSpace(value)
		}
	}
	return properties
}

func contrastRatio(t *testing.T, foreground, background string) float64 {
	t.Helper()
	front := relativeLuminance(t, foreground)
	back := relativeLuminance(t, background)
	return (math.Max(front, back) + 0.05) / (math.Min(front, back) + 0.05)
}

func relativeLuminance(t *testing.T, value string) float64 {
	t.Helper()
	hex := strings.TrimPrefix(value, "#")
	if len(hex) == 3 {
		hex = strings.Repeat(hex[0:1], 2) + strings.Repeat(hex[1:2], 2) + strings.Repeat(hex[2:3], 2)
	}
	if len(hex) != 6 {
		t.Fatalf("unsupported test colour %q", value)
	}
	channels := [3]float64{}
	for index := range channels {
		parsed, err := strconv.ParseUint(hex[index*2:index*2+2], 16, 8)
		if err != nil {
			t.Fatalf("parse test colour %q: %v", value, err)
		}
		channel := float64(parsed) / 255
		if channel <= 0.04045 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}
