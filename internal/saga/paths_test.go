package saga

import (
	"math/rand"
	"path"
	"strings"
	"testing"
)

// entrypointProbes is shared with the schema contract test so both sides of the
// format are exercised by exactly the same corpus. It mixes ordinary values,
// traversal attempts, Windows-shaped paths, and reserved package paths.
var entrypointProbes = []string{
	"index.html", "content.md", "content.txt", "image.svg", "screenshot.png",
	"assets/app.js", "a/b/c/d.css", "sample-data.json", "a.b.c", ".hidden",
	"a.fragment.json", "___notreserved.txt",

	"", ".", "..", "./index.html", "../index.html", "../../etc/passwd",
	"a/../b", "a/./b", "a/..", "a/.", "/index.html", "//index.html",
	"/etc/passwd", "a//b", "a/", "sub/", "index.html/",

	`a\b.html`, `..\..\windows\system32`, `\index.html`, `C:\index.html`,
	"C:/index.html", "c:/index.html", "Z:/x", "C:index.html",

	"fragment.json", "sub/fragment.json", "___landmarks/x.landmark/landmark.json",
	"___diffs/a.json", "sub/___diffs/a.json", "___approvals/x.json",

	"a\x00b.html", "CON", "aux.html", "trailing.", "trailing ", "a<b>.html",
	"question?.html", "star*.html", "pipe|.html", `quote".html`, "colon:.html",
}

func TestEntrypointErrorTable(t *testing.T) {
	accepted := map[string]bool{
		"index.html": true, "content.md": true, "content.txt": true,
		"image.svg": true, "screenshot.png": true, "assets/app.js": true,
		"a/b/c/d.css": true, "sample-data.json": true, "a.b.c": true,
		".hidden": true, "a.fragment.json": true, "___notreserved.txt": false,
		"a\x00b.html": false, "CON": true, "aux.html": true,
		"trailing.": true, "trailing ": true, "a<b>.html": true,
		"question?.html": true, "star*.html": true, "pipe|.html": true,
		`quote".html`: true, "colon:.html": true, "C:index.html": false,
	}
	for _, probe := range entrypointProbes {
		reason := EntrypointError(probe)
		want, known := accepted[probe]
		if !known {
			want = false
		}
		if got := reason == ""; got != want {
			t.Errorf("EntrypointError(%q) = %q; accepted=%v want accepted=%v", probe, reason, got, want)
		}
	}
}

// A rejected entrypoint must never resolve outside its package, and an accepted
// one must always stay inside it. This is the property the traversal rules
// exist to guarantee, checked independently of the hand-written table.
func TestEntrypointAcceptedPathsStayInsideThePackage(t *testing.T) {
	for _, probe := range entrypointProbes {
		if EntrypointError(probe) != "" {
			continue
		}
		joined := path.Join("/pkg", probe)
		if joined != "/pkg/"+probe {
			t.Errorf("accepted entrypoint %q normalizes to %q", probe, joined)
		}
		if !strings.HasPrefix(joined, "/pkg/") {
			t.Errorf("accepted entrypoint %q escapes its package as %q", probe, joined)
		}
	}
}

// TestEntrypointFuzzNeverAcceptsEscape generates adversarial paths from the
// segment alphabet that traversal bugs are built from and asserts the invariant
// directly, rather than trusting an enumerated table to be complete.
func TestEntrypointFuzzNeverAcceptsEscape(t *testing.T) {
	segments := []string{"a", "b", "..", ".", "", "___diffs", "fragment.json", `x\y`, "C:", "sub"}
	random := rand.New(rand.NewSource(20260820))
	for i := 0; i < 20000; i++ {
		count := 1 + random.Intn(4)
		parts := make([]string, count)
		for j := range parts {
			parts[j] = segments[random.Intn(len(segments))]
		}
		value := strings.Join(parts, "/")
		if random.Intn(4) == 0 {
			value = "/" + value
		}
		if EntrypointError(value) != "" {
			continue
		}
		joined := path.Join("/pkg", value)
		if joined != "/pkg/"+value || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
			t.Fatalf("accepted entrypoint %q resolves to %q", value, joined)
		}
		for _, part := range strings.Split(value, "/") {
			if part == "" || part == "." || part == ".." || part == "fragment.json" || strings.HasPrefix(part, "___") {
				t.Fatalf("accepted entrypoint %q contains forbidden segment %q", value, part)
			}
		}
	}
}

func FuzzEntrypointError(f *testing.F) {
	for _, probe := range entrypointProbes {
		f.Add(probe)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if EntrypointError(value) != "" {
			return
		}
		if value == "" || strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") {
			t.Fatalf("accepted unsafe entrypoint %q", value)
		}
		if path.Clean(value) != value {
			t.Fatalf("accepted non-normalized entrypoint %q", value)
		}
		if joined := path.Join("/pkg", value); !strings.HasPrefix(joined, "/pkg/") {
			t.Fatalf("accepted escaping entrypoint %q -> %q", value, joined)
		}
	})
}

func TestPortabilityWarning(t *testing.T) {
	portable := []string{"index.html", "a-b_c.md", "Overview.fragment", "a.b.c", "9lives", "connect.md", "console"}
	for _, name := range portable {
		if reason := PortabilityWarning(name); reason != "" {
			t.Errorf("PortabilityWarning(%q) = %q, want portable", name, reason)
		}
	}
	unportable := []string{
		"con", "CON", "Con.txt", "nul", "aux", "prn", "com1", "COM9.md", "lpt3",
		"a:b", "a<b", "a>b", `a"b`, "a|b", "a?b", "a*b", "trailing.", "trailing ",
		"line\nbreak", "tab\there", "bell\x07",
	}
	for _, name := range unportable {
		if PortabilityWarning(name) == "" {
			t.Errorf("PortabilityWarning(%q) reported no problem", name)
		}
	}
	if PortablePathWarning("assets/con/app.js") == "" {
		t.Error("a reserved component anywhere in the path must be reported")
	}
	if reason := PortablePathWarning("assets/app.js"); reason != "" {
		t.Errorf("PortablePathWarning(assets/app.js) = %q", reason)
	}
}
