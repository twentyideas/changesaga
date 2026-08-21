package saga

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSagaJSON = `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`

// buildSaga writes a minimal valid saga and then applies the caller's overlay,
// so each case states only the thing under test.
func buildSaga(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "test.saga")
	base := map[string]string{
		"saga.json":                       validSagaJSON,
		"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`,
		"overview.fragment/content.md":    "The whole story.\n",
	}
	for rel, body := range files {
		base[rel] = body
	}
	for rel, body := range base {
		if body == "" {
			continue
		}
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	return root
}

func loadIssues(t *testing.T, root string) (Validation, string) {
	t.Helper()
	_, validation, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var messages []string
	for _, issue := range validation.Issues {
		messages = append(messages, issue.Severity+" "+issue.Path+": "+issue.Message)
	}
	return validation, strings.Join(messages, "\n")
}

// TestLoadRejectsMalformedMetadata is the adversarial table for records that a
// published JSON Schema rejects. Every case here used to load cleanly, which
// meant an engine validating against schema/v2 and this runtime disagreed about
// whether the same saga was well formed.
func TestLoadRejectsMalformedMetadata(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{{
		name:  "repository carries credentials",
		files: map[string]string{"saga.json": `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://user:secret@example.test/a.git","base":"main","head":"HEAD"}}`},
		want:  "userinfo",
	}, {
		name:  "repository keeps ssh userinfo",
		files: map[string]string{"saga.json": `{"version":2,"id":"test","title":"A saga","source":{"repository":"ssh://git@example.test/acme/app.git","base":"main","head":"HEAD"}}`},
		want:  `use "ssh://example.test/acme/app.git"`,
	}, {
		name:  "pull request number is not positive",
		files: map[string]string{"saga.json": `{"version":2,"id":"test","title":"A saga","pr":{"number":0},"source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`},
		want:  "pr.number",
	}, {
		name:  "pull request url is relative",
		files: map[string]string{"saga.json": `{"version":2,"id":"test","title":"A saga","pr":{"url":"/pull/7"},"source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`},
		want:  "pr.url",
	}, {
		name:  "media type is not a published type",
		files: map[string]string{"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"O","media_type":"image/","entrypoint":"content.md"}`},
		want:  "unsupported media_type",
	}, {
		name:  "media type smuggles a header break",
		files: map[string]string{"overview.fragment/fragment.json": "{\"version\":2,\"id\":\"overview\",\"title\":\"O\",\"media_type\":\"image/png\\r\\nX-Evil: 1\",\"entrypoint\":\"content.md\"}"},
		want:  "unsupported media_type",
	}, {
		name:  "entrypoint uses a backslash separator",
		files: map[string]string{"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"O","media_type":"text/markdown","entrypoint":"sub\\content.md"}`},
		want:  "backslash",
	}, {
		name:  "entrypoint is an absolute path",
		files: map[string]string{"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"O","media_type":"text/markdown","entrypoint":"/etc/passwd"}`},
		want:  "relative to its fragment package",
	}, {
		name:  "entrypoint is a Windows drive path",
		files: map[string]string{"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"O","media_type":"text/markdown","entrypoint":"C:/windows/win.ini"}`},
		want:  "relative to its fragment package",
	}, {
		name:  "entrypoint traverses out of the package",
		files: map[string]string{"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"O","media_type":"text/markdown","entrypoint":"../saga.json"}`},
		want:  "inside its fragment package",
	}, {
		name:  "entrypoint addresses reserved metadata",
		files: map[string]string{"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"O","media_type":"text/markdown","entrypoint":"___diffs/a.json"}`},
		want:  "reserved fragment path",
	}, {
		name:  "entrypoint names the fragment manifest",
		files: map[string]string{"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"O","media_type":"text/markdown","entrypoint":"fragment.json"}`},
		want:  "reserved fragment path",
	}, {
		name:  "evidence file selects nothing",
		files: map[string]string{"___diffs/empty.json": `{"version":2,"diffs":[]}`},
		want:  "at least one diff reference",
	}, {
		name: "thread id disagrees with its directory",
		files: map[string]string{
			"___review/threads/alpha.thread/thread.json":                                     `{"version":2,"id":"beta","target":"urn:change-saga:test:fragment:overview","anchor":{"type":"target"},"created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/alpha.thread/messages/m1.message/message.json":                `{"version":2,"id":"m1","created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/alpha.thread/messages/m1.message/body.fragment/fragment.json": `{"version":2,"id":"m1-body","media_type":"text/markdown","entrypoint":"content.md"}`,
			"___review/threads/alpha.thread/messages/m1.message/body.fragment/content.md":    "hi\n",
		},
		want: `thread id "beta" must match directory "alpha.thread"`,
	}, {
		name: "message id disagrees with its directory",
		files: map[string]string{
			"___review/threads/t1.thread/thread.json":                                     `{"version":2,"id":"t1","target":"urn:change-saga:test:fragment:overview","anchor":{"type":"target"},"created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/message.json":                `{"version":2,"id":"m2","created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/fragment.json": `{"version":2,"id":"m1-body","media_type":"text/markdown","entrypoint":"content.md"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/content.md":    "hi\n",
		},
		want: `message id "m2" must match directory "m1.message"`,
	}, {
		name:  "approval id is not a stable identifier",
		files: map[string]string{"___approvals/a.json": `{"version":2,"id":"../escape","state":"approved","created_at":"2026-08-19T12:00:00Z"}`},
		want:  "stable id",
	}, {
		name: "thread event id is not a stable identifier",
		files: map[string]string{
			"___review/threads/t1.thread/thread.json":                                     `{"version":2,"id":"t1","target":"urn:change-saga:test:fragment:overview","anchor":{"type":"target"},"created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/message.json":                `{"version":2,"id":"m1","created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/fragment.json": `{"version":2,"id":"m1-body","media_type":"text/markdown","entrypoint":"content.md"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/content.md":    "hi\n",
			"___review/threads/t1.thread/events/e.json":                                   `{"version":2,"id":"a/b","state":"resolved","created_at":"2026-08-19T12:01:00Z"}`,
		},
		want: "stable id",
	}, {
		name: "annotation color is not a safe value",
		files: map[string]string{
			"___review/threads/t1.thread/thread.json":                                     `{"version":2,"id":"t1","target":"urn:change-saga:test:fragment:overview","anchor":{"type":"region","coordinate_space":"normalized","shapes":[{"type":"rect","x":0.1,"y":0.1,"width":0.2,"height":0.2,"color":"expression(alert(1))"}]},"created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/message.json":                `{"version":2,"id":"m1","created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/fragment.json": `{"version":2,"id":"m1-body","media_type":"text/markdown","entrypoint":"content.md"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/content.md":    "hi\n",
		},
		want: "#rrggbb",
	}, {
		name: "text anchor positions run backwards",
		files: map[string]string{
			"___review/threads/t1.thread/thread.json":                                     `{"version":2,"id":"t1","target":"urn:change-saga:test:fragment:overview","anchor":{"type":"text","text":{"exact":"story","start":-4,"end":-9}},"created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/message.json":                `{"version":2,"id":"m1","created_at":"2026-08-19T12:00:00Z"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/fragment.json": `{"version":2,"id":"m1-body","media_type":"text/markdown","entrypoint":"content.md"}`,
			"___review/threads/t1.thread/messages/m1.message/body.fragment/content.md":    "hi\n",
		},
		want: "non-negative",
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			validation, report := loadIssues(t, buildSaga(t, testCase.files))
			if validation.Valid {
				t.Fatalf("saga should be invalid; issues:\n%s", report)
			}
			if !strings.Contains(report, testCase.want) {
				t.Fatalf("expected an issue containing %q; got:\n%s", testCase.want, report)
			}
		})
	}
}

// TestLoadAcceptsPortableStructure guards the other direction: nothing above
// may start rejecting a saga the schema still accepts.
func TestLoadAcceptsPortableStructure(t *testing.T) {
	root := buildSaga(t, map[string]string{
		"overview.fragment/fragment.json":             `{"version":2,"id":"overview","title":"Overview","media_type":"text/html","entrypoint":"assets/index.html"}`,
		"overview.fragment/content.md":                "",
		"overview.fragment/assets/index.html":         `<p id="intro">Hello</p>`,
		"backend.chapter/chapter.json":                `{"version":2,"id":"backend","title":"Backend","order":10}`,
		"backend.chapter/flow.fragment/fragment.json": `{"version":2,"id":"flow","title":"Flow","media_type":"image/svg+xml","entrypoint":"image.svg"}`,
		"backend.chapter/flow.fragment/image.svg":     `<svg xmlns="http://www.w3.org/2000/svg"><rect id="box"/></svg>`,
	})
	validation, report := loadIssues(t, root)
	if !validation.Valid {
		t.Fatalf("portable saga should be valid; issues:\n%s", report)
	}
	if !strings.Contains(report, "") && len(validation.Issues) != 0 {
		t.Fatalf("unexpected issues:\n%s", report)
	}
}

func TestLoadWarnsAboutUnportableNames(t *testing.T) {
	root := buildSaga(t, map[string]string{
		"overview.fragment/fragment.json": `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"aux.md"}`,
		"overview.fragment/content.md":    "",
		"overview.fragment/aux.md":        "Story.\n",
	})
	validation, report := loadIssues(t, root)
	if !validation.Valid {
		t.Fatalf("an unportable name is a warning, not an error; issues:\n%s", report)
	}
	if !strings.Contains(report, "reserved Windows device name") {
		t.Fatalf("expected a Windows portability warning; got:\n%s", report)
	}
}

// A symlink is reported as a non-directory by os.ReadDir, so an unguarded
// loader silently skips it and calls the saga valid while an entire chapter is
// invisible.
func TestLoadRejectsSymlinkedEntities(t *testing.T) {
	if _, err := os.Lstat("/"); err != nil {
		t.Skip("no filesystem")
	}
	cases := []struct {
		name string
		link string
	}{
		{name: "chapter", link: "elsewhere.chapter"},
		{name: "fragment", link: "elsewhere.fragment"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := buildSaga(t, nil)
			outside := filepath.Join(filepath.Dir(root), "outside")
			if err := os.MkdirAll(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, testCase.link)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			validation, report := loadIssues(t, root)
			if validation.Valid {
				t.Fatalf("a symlinked %s must not be silently ignored; issues:\n%s", testCase.name, report)
			}
			if !strings.Contains(report, "symlink") {
				t.Fatalf("expected a symlink issue; got:\n%s", report)
			}
		})
	}
}

func TestLoadRejectsSymlinkedThread(t *testing.T) {
	root := buildSaga(t, nil)
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "___review", "threads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "___review", "threads", "t1.thread")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	validation, report := loadIssues(t, root)
	if validation.Valid || !strings.Contains(report, "symlink") {
		t.Fatalf("a symlinked thread must be reported; issues:\n%s", report)
	}
}

// A saga.json written by change-saga init must load without a single issue.
// This is the compatibility floor for every v2 saga already in the wild.
func TestExistingCanonicalManifestStaysClean(t *testing.T) {
	for _, repository := range []string{
		"https://github.com/acme/payments.git",
		"https://example.test/acme/app.git",
		"file:///srv/repos/app",
	} {
		root := buildSaga(t, map[string]string{
			"saga.json": fmt.Sprintf(`{"version":2,"id":"test","title":"A saga","source":{"repository":%q,"base":"main","head":"HEAD"}}`, repository),
		})
		validation, report := loadIssues(t, root)
		if !validation.Valid || len(validation.Issues) != 0 {
			t.Fatalf("repository %q should load cleanly; issues:\n%s", repository, report)
		}
	}
}

func TestNoncanonicalRepositoryIsAWarningNotAnError(t *testing.T) {
	root := buildSaga(t, map[string]string{
		"saga.json": `{"version":2,"id":"test","title":"A saga","source":{"repository":"HTTPS://Example.TEST:443/acme/app.git/","base":"main","head":"HEAD"}}`,
	})
	validation, report := loadIssues(t, root)
	if !validation.Valid {
		t.Fatalf("an existing noncanonical identity must keep loading; issues:\n%s", report)
	}
	if !strings.Contains(report, "not canonical") {
		t.Fatalf("expected a canonicalization warning; got:\n%s", report)
	}
}
