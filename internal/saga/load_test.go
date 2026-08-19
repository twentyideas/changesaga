package saga

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/review-saga/review-saga/internal/diffuri"
)

func TestLoadRecursiveFragmentsAndReviewOverlay(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# The whole story\n")
	writeTestFile(t, filepath.Join(root, "backend.chapter", "chapter.json"), `{"version":2,"id":"backend","title":"Backend"}`)
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "section.json"), `{"version":2,"id":"request-flow","title":"Request flow"}`)
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "fragment.json"), `{"version":2,"id":"flow","title":"Flow","media_type":"text/html","entrypoint":"index.html"}`)
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "index.html"), `<button onclick="this.textContent='ok'">Try it</button>`)

	diff, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "line", Path: "api.go", Side: "new", Start: 2, End: 4})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "___diffs", "api.json"), fmt.Sprintf(`{"version":2,"diffs":[{"uri":%q}]}`, diff))
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "thread.json"), `{"version":2,"id":"thread-1","target":"urn:review-saga:test:fragment:flow","anchor":{"type":"region","coordinate_space":"normalized","shapes":[{"type":"rect","x":0.1,"y":0.2,"width":0.3,"height":0.4}]},"created_by":"Ada","created_at":"2026-08-19T12:00:00Z"}`)
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "messages", "message-1.message", "message.json"), `{"version":2,"id":"message-1","author":"Ada","created_at":"2026-08-19T12:00:00Z"}`)
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "messages", "message-1.message", "body.fragment", "fragment.json"), `{"version":2,"id":"message-body","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "messages", "message-1.message", "body.fragment", "content.md"), "Please explain this transition.\n")

	document, validation, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid {
		t.Fatalf("saga should be valid: %#v", validation)
	}
	if len(document.Section.Fragments) != 1 || len(document.Section.Children) != 1 {
		t.Fatalf("unexpected root: %#v", document.Section)
	}
	chapter := document.Section.Children[0]
	if chapter.Kind != "chapter" || chapter.Target != ChapterTarget("test", "backend") || len(chapter.Children) != 1 {
		t.Fatalf("chapter was not loaded as a review boundary: %#v", chapter)
	}
	flow := chapter.Children[0].Fragments[0]
	if flow.MediaType != "text/html" || len(flow.Diffs) != 1 {
		t.Fatalf("interactive fragment was not loaded: %#v", flow)
	}
	if len(document.Threads) != 1 || len(document.Threads[0].Messages) != 1 || document.Threads[0].Target != flow.Target {
		t.Fatalf("review overlay was not loaded: %#v", document.Threads)
	}
	if document.Threads[0].LegacyClaimedAuthor != "Ada" || document.Threads[0].Messages[0].LegacyClaimedAuthor != "Ada" {
		t.Fatalf("legacy claimed identity was not retained: %#v", document.Threads[0])
	}
	if document.Threads[0].Attribution.Status != AttributionHistoryUnavailable || document.Threads[0].Messages[0].Attribution.Status != AttributionHistoryUnavailable {
		t.Fatalf("non-Git history should be reported honestly: %#v", document.Threads[0])
	}
}

func TestLoadAttributesEveryReviewEventToItsIntroducingCommitter(t *testing.T) {
	repo := t.TempDir()
	sagaGit(t, repo, nil, "init", "-b", "main")
	root := filepath.Join(repo, "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Story\n")
	sagaGit(t, repo, nil, "add", ".")
	sagaGit(t, repo, reviewIdentity("Setup", "setup@example.test"), "commit", "-m", "saga")

	threadDir := filepath.Join(root, "___review", "threads", "thread-1.thread")
	writeTestFile(t, filepath.Join(threadDir, "thread.json"), `{"version":2,"id":"thread-1","target":"urn:review-saga:test:fragment:overview","anchor":{"type":"target"},"created_by":"Payload Name","created_at":"2026-08-19T12:00:00Z"}`)
	sagaGit(t, repo, nil, "add", ".")
	sagaGit(t, repo, reviewIdentity("Thread Committer", "thread@example.test"), "commit", "-m", "thread root")

	messageDir := filepath.Join(threadDir, "messages", "message-1.message")
	writeTestFile(t, filepath.Join(messageDir, "message.json"), `{"version":2,"id":"message-1","author":"Another Payload Name","created_at":"2026-08-19T12:01:00Z"}`)
	writeTestFile(t, filepath.Join(messageDir, "body.fragment", "fragment.json"), `{"version":2,"id":"message-body","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(messageDir, "body.fragment", "content.md"), "Comment\n")
	sagaGit(t, repo, nil, "add", ".")
	sagaGit(t, repo, reviewIdentity("Reply Committer", "reply@example.test"), "commit", "-m", "message")

	fileURI, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "file", Path: "app.go"})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(threadDir, "events", "event-1.json"), `{"version":2,"id":"event-1","state":"resolved","created_at":"2026-08-19T12:02:00Z"}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "___approvals", "review-1.json"), `{"version":2,"id":"review-1","state":"approved","created_at":"2026-08-19T12:02:00Z"}`)
	writeTestFile(t, filepath.Join(root, "___review", "diffs", "diff-1.json"), fmt.Sprintf(`{"version":2,"id":"diff-1","uri":%q,"state":"reviewed","created_at":"2026-08-19T12:02:00Z"}`, fileURI))
	sagaGit(t, repo, nil, "add", ".")
	sagaGit(t, repo, reviewIdentity("Event Committer", "events@example.test"), "commit", "-m", "review events")

	document, validation, err := Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load: validation=%#v err=%v", validation, err)
	}
	thread := document.Threads[0]
	assertCommitter(t, thread.Attribution, "Thread Committer", "thread@example.test")
	assertCommitter(t, thread.Messages[0].Attribution, "Reply Committer", "reply@example.test")
	assertCommitter(t, thread.Events[0].Attribution, "Event Committer", "events@example.test")
	assertCommitter(t, document.Section.Fragments[0].Reviews[0].Attribution, "Event Committer", "events@example.test")
	assertCommitter(t, document.DiffReviews[0].Attribution, "Event Committer", "events@example.test")
	if thread.LegacyClaimedAuthor != "Payload Name" || thread.Messages[0].LegacyClaimedAuthor != "Another Payload Name" {
		t.Fatalf("legacy claims were not separated from canonical identity: %#v", thread)
	}
}

func assertCommitter(t *testing.T, attribution Attribution, name, email string) {
	t.Helper()
	if attribution.Status != AttributionCommitted || attribution.Committer == nil || attribution.Committer.Name != name || attribution.Committer.Email != email || attribution.Commit == "" || attribution.CommittedAt == nil || strings.Contains(attribution.Committer.Name, "Git Author") {
		t.Fatalf("attribution = %#v, want %s <%s>", attribution, name, email)
	}
}

func reviewIdentity(name, email string) []string {
	return []string{
		"GIT_AUTHOR_NAME=Git Author", "GIT_AUTHOR_EMAIL=git-author@example.test",
		"GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email,
	}
}

func sagaGit(t *testing.T, dir string, environment []string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func TestLoadRejectsNestedChapter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "outer.chapter", "chapter.json"), `{"version":2,"id":"outer","title":"Outer"}`)
	writeTestFile(t, filepath.Join(root, "outer.chapter", "inner.chapter", "chapter.json"), `{"version":2,"id":"inner","title":"Inner"}`)
	_, validation, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid {
		t.Fatal("nested chapters should be invalid; recurse with sections instead")
	}
}

func TestLoadRejectsUnknownJSONFields(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","surprise":true,"source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	if _, _, err := Load(root); err == nil {
		t.Fatal("expected unknown manifest field to fail")
	}
}

func TestLoadRejectsMissingFragmentEntrypoint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "broken.fragment", "fragment.json"), `{"version":2,"id":"broken","media_type":"text/html","entrypoint":"index.html"}`)
	_, validation, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid {
		t.Fatal("missing entrypoint should invalidate saga")
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
