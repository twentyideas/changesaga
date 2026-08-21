package saga

import (
	"path"
	"regexp"
	"strings"
)

// mediaTypePattern mirrors schema/v2/fragment.schema.json exactly. Keeping the
// runtime predicate and the published schema on the same grammar prevents a
// fragment that one accepts and the other rejects.
var mediaTypePattern = regexp.MustCompile(`^(text/(markdown|html|plain)|image/[A-Za-z0-9.+-]+)$`)

// ValidMediaType reports whether a fragment media type is one the format
// defines. Engines render text/markdown, text/html, text/plain, image/svg+xml,
// and raster image/* fragments.
func ValidMediaType(value string) bool { return mediaTypePattern.MatchString(value) }

// windowsReservedNames cannot be used as a path component on Windows, with or
// without an extension. A saga containing one cannot be checked out there.
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// EntrypointError reports why a fragment entrypoint is unusable, or "" when it
// is a well-formed package-relative path.
//
// The check is deliberately expressed in slash-path terms rather than through
// path/filepath. filepath.Clean rewrites "assets/app.js" to "assets\app.js" on
// Windows, so an OS-dependent normalization comparison would reject a portable
// nested entrypoint on exactly one platform. Backslashes are rejected outright
// because they are an ordinary filename byte on Unix and a separator on
// Windows, which would make one saga address two different files.
func EntrypointError(value string) string {
	switch {
	case value == "":
		return "entrypoint is required"
	case strings.ContainsFunc(value, isControl):
		return "entrypoint must not contain control characters"
	case strings.ContainsRune(value, '\\'):
		return `entrypoint must use "/" separators; a backslash addresses different files on Windows and Unix`
	case strings.HasPrefix(value, "/"):
		return "entrypoint must be relative to its fragment package"
	case len(value) >= 2 && value[1] == ':' && isASCIILetter(value[0]):
		return "entrypoint must be relative to its fragment package"
	case path.Clean(value) != value:
		return "entrypoint must be a normalized fragment-relative path"
	case value == "." || value == ".." || strings.HasPrefix(value, "../"):
		return "entrypoint must name a file inside its fragment package"
	}
	for _, part := range strings.Split(value, "/") {
		if strings.HasPrefix(part, "___") || part == "fragment.json" {
			// The renderer refuses to serve reserved package paths, so an
			// entrypoint naming one would validate and then fail to load.
			return "entrypoint cannot address a reserved fragment path"
		}
	}
	return ""
}

// PortabilityWarning reports why a path component cannot exist on every
// platform Change Saga supports, or "" when the name is portable. It never
// produces errors: a saga that already contains such a name stays loadable on
// the platform that created it, but authors are told before publishing it.
func PortabilityWarning(name string) string {
	if name == "" || name == "." || name == ".." {
		return ""
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return "contains a control character"
		}
		if strings.ContainsRune(`<>:"|?*`, character) {
			return "contains a character Windows cannot store in a filename"
		}
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "ends in a dot or space, which Windows silently strips"
	}
	stem := strings.ToLower(name)
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	if windowsReservedNames[stem] {
		return "is a reserved Windows device name"
	}
	return ""
}

// PortablePathWarning applies PortabilityWarning to every component of a slash
// path.
func PortablePathWarning(value string) string {
	for _, part := range strings.Split(value, "/") {
		if reason := PortabilityWarning(part); reason != "" {
			return reason
		}
	}
	return ""
}

func isControl(value rune) bool { return value < 0x20 || value == 0x7f }

func isASCIILetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
