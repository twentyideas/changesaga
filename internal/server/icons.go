package server

import (
	"path"
	"strings"
)

// The renderer ships its own icon set so a committed saga can be reviewed with
// no network access and no third-party font or icon license to honour. Every
// glyph below is original to this repository and inherits its MIT licence.
//
// Icons are emitted once per page as SVG <symbol> elements and referenced with
// <use>. They are always decorative: the accessible name comes from the
// aria-label or visible text of the control that owns them.

var iconSprite = `<svg class="icon-sprite" aria-hidden="true" focusable="false" width="0" height="0" style="position:absolute">` +
	uiIconSymbols + fileIconSymbols + `</svg>`

const uiIconSymbols = `` +
	`<symbol id="i-chevron" viewBox="0 0 16 16"><path d="M6 3.5 10.5 8 6 12.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-link" viewBox="0 0 16 16"><path d="M6.6 9.4a2.8 2.8 0 0 0 4 0l2-2a2.8 2.8 0 0 0-4-4l-.9.9M9.4 6.6a2.8 2.8 0 0 0-4 0l-2 2a2.8 2.8 0 0 0 4 4l.9-.9" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></symbol>` +
	`<symbol id="i-more" viewBox="0 0 16 16"><circle cx="3.5" cy="8" r="1.3" fill="currentColor"/><circle cx="8" cy="8" r="1.3" fill="currentColor"/><circle cx="12.5" cy="8" r="1.3" fill="currentColor"/></symbol>` +
	`<symbol id="i-diff" viewBox="0 0 16 16"><path d="M3.5 2h5l4 4v8h-9z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M8.5 2v4h4M6 9h4M8 7v4M6 12.5h4" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></symbol>` +
	`<symbol id="i-close" viewBox="0 0 16 16"><path d="m4 4 8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></symbol>` +
	`<symbol id="i-undo" viewBox="0 0 16 16"><path d="M3 8h6.5a3.2 3.2 0 0 1 0 6.4H6M3 8l3-3M3 8l3 3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-redo" viewBox="0 0 16 16"><path d="M13 8H6.5a3.2 3.2 0 0 0 0 6.4H10M13 8l-3-3M13 8l-3 3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-cursor" viewBox="0 0 16 16"><path d="M3.5 2.2 12.6 8l-3.9.7-1.9 3.6z" fill="currentColor" stroke="currentColor" stroke-width="1.1" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-comment" viewBox="0 0 16 16"><path d="M2.5 3.5h11v7.2h-6l-3.2 2.6v-2.6H2.5z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-note" viewBox="0 0 16 16"><path d="M2.5 2.5h11v7.6l-3.4 3.4H2.5z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M10.1 13.5v-3.4h3.4M5 6h6M5 8.6h4" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-marker" viewBox="0 0 16 16"><path d="m9.6 2.6 3.8 3.8-5.3 5.3H4.3V8z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M2.6 14.2h10.8" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></symbol>` +
	`<symbol id="i-square" viewBox="0 0 16 16"><rect x="2.6" y="3.4" width="10.8" height="9.2" rx="1.4" fill="none" stroke="currentColor" stroke-width="1.5"/></symbol>` +
	`<symbol id="i-pen" viewBox="0 0 16 16"><path d="M2.4 13.6c2.6.4 3.4-1 3.4-2.3 0-1-.8-1.6-1.6-1.3-.9.4-.8 1.9.6 2.6 2 1 4.6-.6 6.2-3.1" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/><path d="m10.6 2.6 2.8 2.8-4.6 4.6-3.4.6.6-3.4z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-trash" viewBox="0 0 16 16"><path d="M3.5 4.5h9M6.5 4.5V3h3v1.5M4.8 4.5l.6 8.2h5.2l.6-8.2" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-inline" viewBox="0 0 16 16"><rect x="2.4" y="3.2" width="11.2" height="3.4" rx="1" fill="none" stroke="currentColor" stroke-width="1.4"/><rect x="2.4" y="9.4" width="11.2" height="3.4" rx="1" fill="none" stroke="currentColor" stroke-width="1.4"/></symbol>` +
	`<symbol id="i-split" viewBox="0 0 16 16"><rect x="2.4" y="3.2" width="4.6" height="9.6" rx="1" fill="none" stroke="currentColor" stroke-width="1.4"/><rect x="9" y="3.2" width="4.6" height="9.6" rx="1" fill="none" stroke="currentColor" stroke-width="1.4"/></symbol>` +
	`<symbol id="i-panel-left" viewBox="0 0 16 16"><rect x="2.2" y="3" width="11.6" height="10" rx="1.4" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M6.4 3v10" stroke="currentColor" stroke-width="1.4"/></symbol>` +
	`<symbol id="i-panel-right" viewBox="0 0 16 16"><rect x="2.2" y="3" width="11.6" height="10" rx="1.4" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M9.6 3v10" stroke="currentColor" stroke-width="1.4"/></symbol>` +
	`<symbol id="i-check" viewBox="0 0 16 16"><path d="m3.4 8.4 3 3 6.2-6.6" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-dot" viewBox="0 0 16 16"><circle cx="8" cy="8" r="3.4" fill="currentColor"/></symbol>` +
	`<symbol id="i-circle" viewBox="0 0 16 16"><circle cx="8" cy="8" r="3.4" fill="none" stroke="currentColor" stroke-width="1.4"/></symbol>` +
	`<symbol id="i-half" viewBox="0 0 16 16"><circle cx="8" cy="8" r="3.4" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M8 4.6a3.4 3.4 0 0 1 0 6.8z" fill="currentColor"/></symbol>` +
	`<symbol id="i-alert" viewBox="0 0 16 16"><path d="M8 2.4 14.4 13.4H1.6z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M8 6.4v3.2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><circle cx="8" cy="11.6" r=".9" fill="currentColor"/></symbol>` +
	`<symbol id="i-search" viewBox="0 0 16 16"><circle cx="7" cy="7" r="4.2" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="m10.2 10.2 3.2 3.2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></symbol>` +
	`<symbol id="i-folder" viewBox="0 0 16 16"><path d="M1.8 3.6h4.3l1.3 1.6h6.8v7.2H1.8z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-arrow-right" viewBox="0 0 16 16"><path d="M3 8h9.4M8.6 4.2 12.4 8l-3.8 3.8" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-external" viewBox="0 0 16 16"><path d="M8.6 3.4H3.4v9.2h9.2V7.4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/><path d="M10 2.6h3.4V6M13.4 2.6 7.8 8.2" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></symbol>` +
	`<symbol id="i-book" viewBox="0 0 16 16"><path d="M2.6 2.8h4a2 2 0 0 1 1.4.6 2 2 0 0 1 1.4-.6h4v9.4h-4a2 2 0 0 0-1.4.6 2 2 0 0 0-1.4-.6h-4z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M8 3.4v9.4" stroke="currentColor" stroke-width="1.3"/></symbol>` +
	`<symbol id="i-list" viewBox="0 0 16 16"><path d="M5.4 4.4h8M5.4 8h8M5.4 11.6h8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><circle cx="2.8" cy="4.4" r="1" fill="currentColor"/><circle cx="2.8" cy="8" r="1" fill="currentColor"/><circle cx="2.8" cy="11.6" r="1" fill="currentColor"/></symbol>`

// fileIconSymbols renders language badges. Two-character monograms stay legible
// at 16px and keep white-on-colour contrast above 4.5:1.
var fileIconSymbols = `` +
	fileBadge("f-go", "#0077a8", "GO") +
	fileBadge("f-ts", "#2a5fa5", "TS") +
	fileBadge("f-js", "#8a6d00", "JS") +
	fileBadge("f-md", "#4c5560", "MD") +
	fileBadge("f-json", "#875200", "{}") +
	fileBadge("f-yaml", "#63399c", "YM") +
	fileBadge("f-html", "#b23a16", "<>") +
	fileBadge("f-css", "#4a3468", "CS") +
	fileBadge("f-svg", "#8c5e08", "SV") +
	fileBadge("f-sh", "#2f6b4f", "$_") +
	`<symbol id="f-image" viewBox="0 0 16 16"><rect x="1.5" y="2.5" width="13" height="11" rx="2" fill="#6c3fbf"/><circle cx="5.6" cy="6.2" r="1.3" fill="#fff"/><path d="M2.6 12.2 6.4 8l2.4 2.6L10.9 9l2.5 3.2z" fill="#fff"/></symbol>` +
	`<symbol id="f-file" viewBox="0 0 16 16"><path d="M3.8 1.8h5.1l3.3 3.3v9.1H3.8z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/><path d="M8.9 1.8v3.3h3.3" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/></symbol>`

func fileBadge(id, color, label string) string {
	return `<symbol id="` + id + `" viewBox="0 0 16 16"><rect x="1.5" y="2.5" width="13" height="11" rx="2" fill="` + color + `"/>` +
		`<text x="8" y="11.1" text-anchor="middle" fill="#fff" font-family="ui-monospace,SFMono-Regular,Menlo,monospace" font-size="7.2" font-weight="700">` + label + `</text></symbol>`
}

var fileIconByExtension = map[string]string{
	"go":    "f-go",
	"mod":   "f-go",
	"sum":   "f-go",
	"ts":    "f-ts",
	"tsx":   "f-ts",
	"mts":   "f-ts",
	"cts":   "f-ts",
	"js":    "f-js",
	"jsx":   "f-js",
	"mjs":   "f-js",
	"cjs":   "f-js",
	"md":    "f-md",
	"mdx":   "f-md",
	"txt":   "f-md",
	"json":  "f-json",
	"jsonc": "f-json",
	"yaml":  "f-yaml",
	"yml":   "f-yaml",
	"toml":  "f-yaml",
	"html":  "f-html",
	"htm":   "f-html",
	"xml":   "f-html",
	"css":   "f-css",
	"scss":  "f-css",
	"less":  "f-css",
	"svg":   "f-svg",
	"png":   "f-image",
	"jpg":   "f-image",
	"jpeg":  "f-image",
	"gif":   "f-image",
	"webp":  "f-image",
	"avif":  "f-image",
	"ico":   "f-image",
	"sh":    "f-sh",
	"bash":  "f-sh",
	"zsh":   "f-sh",
	"fish":  "f-sh",
}

// fileIcon maps a repository path to the symbol that represents its file type.
// Unknown types fall back to a neutral document outline rather than guessing.
func fileIcon(filePath string) string {
	name := strings.ToLower(path.Base(filePath))
	if name == "" {
		return "f-file"
	}
	switch name {
	case "dockerfile", "makefile", "license", "licence", "notice", "contributing", "readme":
		return "f-md"
	case ".gitignore", ".gitattributes", ".editorconfig", ".dockerignore":
		return "f-sh"
	}
	extension := strings.TrimPrefix(path.Ext(name), ".")
	if icon, ok := fileIconByExtension[extension]; ok {
		return icon
	}
	return "f-file"
}
