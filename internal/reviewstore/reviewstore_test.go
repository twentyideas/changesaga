package reviewstore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

func TestReviewRecordsAreAppendOnlyAndFileGranular(t *testing.T) {
	root := newTestSaga(t)
	target := "urn:change-saga:test:fragment:overview"
	first, err := AddThread(root, target, "First comment", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	threadDir := filepath.Join(root, "___review", "threads", first+".thread")
	threadBefore := readReviewFile(t, filepath.Join(threadDir, "thread.json"))
	messagesBefore, err := os.ReadDir(filepath.Join(threadDir, "messages"))
	if err != nil || len(messagesBefore) != 1 {
		t.Fatalf("initial comment should have one message directory: entries=%d err=%v", len(messagesBefore), err)
	}
	firstMessage := filepath.Join(threadDir, "messages", messagesBefore[0].Name(), "message.json")
	messageBefore := readReviewFile(t, firstMessage)

	runConcurrently(t,
		func() error { _, err := AddReply(root, first, "Reply one", nil); return err },
		func() error { _, err := AddReply(root, first, "Reply two", nil); return err },
		func() error {
			_, err := AddThread(root, target, "Second comment", saga.Anchor{Type: "target"}, "comment", "", nil)
			return err
		},
		func() error {
			_, err := AddThread(root, target, "Third comment", saga.Anchor{Type: "target"}, "comment", "", nil)
			return err
		},
	)

	if !bytes.Equal(threadBefore, readReviewFile(t, filepath.Join(threadDir, "thread.json"))) || !bytes.Equal(messageBefore, readReviewFile(t, firstMessage)) {
		t.Fatal("adding comments or replies rewrote an existing record")
	}
	threads, err := os.ReadDir(filepath.Join(root, "___review", "threads"))
	if err != nil || len(threads) != 3 {
		t.Fatalf("comments should use three independent thread directories: entries=%d err=%v", len(threads), err)
	}
	messages, err := os.ReadDir(filepath.Join(threadDir, "messages"))
	if err != nil || len(messages) != 3 {
		t.Fatalf("comment and replies should use three independent message directories: entries=%d err=%v", len(messages), err)
	}
	commentFiles := 0
	err = filepath.WalkDir(filepath.Join(root, "___review", "threads"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && filepath.Base(path) == "content.md" {
			commentFiles++
		}
		return err
	})
	if err != nil || commentFiles != 5 {
		t.Fatalf("each of five comments/replies should have its own content file: files=%d err=%v", commentFiles, err)
	}

	fileURI, err := diffuri.Build(diffuri.Reference{Repository: testRepository, Base: "aaa", Head: "bbb", Kind: "file", Path: "app.go"})
	if err != nil {
		t.Fatal(err)
	}
	runConcurrently(t,
		func() error { return AddReview(root, root, "approved", "Looks good") },
		func() error { return AddReview(root, root, "rejected", "One concern") },
		func() error { return AddDiffReview(root, fileURI, "reviewed") },
		func() error { return AddDiffReview(root, fileURI, "unreviewed") },
	)
	assertEntryCount(t, filepath.Join(root, "___approvals"), 2)
	assertEntryCount(t, filepath.Join(root, "___review", "diffs"), 2)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data := readReviewFile(t, path)
		if bytes.Contains(data, []byte(`"author"`)) || bytes.Contains(data, []byte(`"created_by"`)) {
			t.Fatalf("new review event duplicated editable identity in %s: %s", path, data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestThreadUndoAndRedoAppendStateEvents(t *testing.T) {
	root := newTestSaga(t)
	threadID, err := AddThread(root, "urn:change-saga:test:fragment:overview", "Keep this history", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	threadDir := filepath.Join(root, "___review", "threads", threadID+".thread")
	threadPath := filepath.Join(threadDir, "thread.json")
	threadBefore := readReviewFile(t, threadPath)
	messages, err := os.ReadDir(filepath.Join(threadDir, "messages"))
	if err != nil || len(messages) != 1 {
		t.Fatalf("initial thread messages: entries=%d err=%v", len(messages), err)
	}
	messagePath := filepath.Join(threadDir, "messages", messages[0].Name(), "message.json")
	messageBefore := readReviewFile(t, messagePath)

	if err := SetState(root, threadID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	if err := SetState(root, threadID, "open"); err != nil {
		t.Fatal(err)
	}
	if err := SetAnchor(root, threadID, saga.Anchor{Type: "region", Coordinate: "normalized", Shapes: []saga.Shape{{Type: "rect", X: .2, Y: .3, Width: .4, Height: .2}}}); err != nil {
		t.Fatal(err)
	}
	assertEntryCount(t, filepath.Join(threadDir, "events"), 3)
	if !bytes.Equal(threadBefore, readReviewFile(t, threadPath)) || !bytes.Equal(messageBefore, readReviewFile(t, messagePath)) {
		t.Fatal("undo or redo rewrote the original thread")
	}
	if err := SetState(root, threadID, "deleted"); err == nil {
		t.Fatal("unsupported thread state was accepted")
	}
	if err := SetAnchor(root, threadID, saga.Anchor{Type: "region"}); err == nil {
		t.Fatal("invalid anchor edit was accepted")
	}
}

func TestConcurrentStickyNotesStayFileGranular(t *testing.T) {
	root := newTestSaga(t)
	target := "urn:change-saga:test:fragment:overview"
	note := func(text string, x, y float64) saga.Anchor {
		return saga.Anchor{Type: "note", Coordinate: "normalized", Note: &saga.NoteSelector{Text: text, X: x, Y: y, Color: "#f2bd4b"}}
	}
	first, err := AddThread(root, target, "Rename this helper", note("Rename this helper", .25, .5), "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	threadDir := filepath.Join(root, "___review", "threads", first+".thread")
	threadBefore := readReviewFile(t, filepath.Join(threadDir, "thread.json"))

	runConcurrently(t,
		func() error { return SetAnchor(root, first, note("Rename this helper", .4, .6)) },
		func() error { return SetAnchor(root, first, note("Renamed already", .25, .5)) },
		func() error {
			_, err := AddThread(root, target, "Second note", note("Second note", .8, .1), "comment", "", nil)
			return err
		},
		func() error {
			_, err := AddThread(root, target, "Third note", note("Third note", .1, .9), "comment", "", nil)
			return err
		},
	)

	assertEntryCount(t, filepath.Join(root, "___review", "threads"), 3)
	assertEntryCount(t, filepath.Join(threadDir, "events"), 2)
	if !bytes.Equal(threadBefore, readReviewFile(t, filepath.Join(threadDir, "thread.json"))) {
		t.Fatal("moving or rewording a sticky note rewrote its original record")
	}
	if _, err := AddThread(root, target, "Blank", note("   ", .5, .5), "comment", "", nil); err == nil {
		t.Fatal("a sticky note without visible text was accepted")
	}
	if err := SetAnchor(root, first, note("Off canvas", 1.5, .5)); err == nil {
		t.Fatal("an off-canvas sticky note placement was accepted")
	}
}

func TestFailedThreadAndReplyLeaveNoPartialEntity(t *testing.T) {
	root := newTestSaga(t)
	mutationFaultHook = func(step string) error {
		if step == "after-message-manifest" {
			return fmt.Errorf("injected message failure")
		}
		return nil
	}
	t.Cleanup(func() { mutationFaultHook = nil })
	if _, err := AddThread(root, "urn:change-saga:test:fragment:overview", "partial", saga.Anchor{Type: "target"}, "comment", "", nil); err == nil {
		t.Fatal("AddThread succeeded despite injected failure")
	}
	assertNoCommittedOrTemporaryEntries(t, filepath.Join(root, "___review", "threads"))

	mutationFaultHook = nil
	threadID, err := AddThread(root, "urn:change-saga:test:fragment:overview", "complete", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	messagesDir := filepath.Join(root, "___review", "threads", threadID+".thread", "messages")
	before, err := os.ReadDir(messagesDir)
	if err != nil {
		t.Fatal(err)
	}
	mutationFaultHook = func(step string) error {
		if step == "after-message-manifest" {
			return fmt.Errorf("injected reply failure")
		}
		return nil
	}
	if _, err := AddReply(root, threadID, "partial reply", nil); err == nil {
		t.Fatal("AddReply succeeded despite injected failure")
	}
	after, err := os.ReadDir(messagesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("failed reply left a committed message: before=%d after=%d", len(before), len(after))
	}
	for _, entry := range after {
		if strings.HasPrefix(entry.Name(), ".change-saga-stage-") {
			t.Fatalf("failed reply left temporary state %q", entry.Name())
		}
	}
}

func TestMutationRefusesForeignRepositoryDiffIdentityWithoutSideEffect(t *testing.T) {
	root := newTestSaga(t)
	target := "urn:change-saga:test:fragment:overview"
	foreign := func(kind, path string, line int) string {
		reference := diffuri.Reference{Repository: "https://example.test/other.git", Base: "aaa", Head: "bbb", Kind: kind, Path: path}
		if kind == "line" {
			reference.Side, reference.Start, reference.End = "new", line, line
		}
		uri, err := diffuri.Build(reference)
		if err != nil {
			t.Fatal(err)
		}
		return uri
	}
	own, err := AddThread(root, target, "Anchor me", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, root)

	foreignAnchor := saga.Anchor{Type: "diff", Diff: &saga.DiffSelector{URI: foreign("line", "app.go", 4)}}
	if err := AddDiffReview(root, foreign("file", "app.go", 0), "reviewed"); err == nil || !strings.Contains(err.Error(), "does not match the saga source repository") {
		t.Fatalf("foreign diff review error = %v, want repository mismatch", err)
	}
	if _, err := AddThread(root, target, "Foreign anchor", foreignAnchor, "comment", "", nil); err == nil {
		t.Fatal("thread anchored to a foreign repository was accepted")
	}
	if err := SetAnchor(root, own, foreignAnchor); err == nil {
		t.Fatal("re-anchoring a thread to a foreign repository was accepted")
	}
	if after := treeSnapshot(t, root); after != before {
		t.Fatalf("rejected foreign diff identity changed the saga:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMutationRefusesStructurallyInvalidSagaWithoutSideEffect(t *testing.T) {
	root := newTestSaga(t)
	manifest := saga.Manifest{Schema: saga.SchemaURL, Version: 999, ID: "test", Title: "Test", Source: saga.Source{Repository: "https://example.test/repo.git", Base: "main", Head: "HEAD"}}
	if err := store.WriteJSON(filepath.Join(root, "saga.json"), manifest, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddReview(root, root, "approved", "should not exist"); err == nil || !strings.Contains(err.Error(), "structurally invalid") {
		t.Fatalf("AddReview error = %v, want invalid saga refusal", err)
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("invalid saga mutation changed root entries: before=%d after=%d", len(before), len(after))
	}
	if _, err := os.Stat(filepath.Join(root, ".change-saga.lock")); !os.IsNotExist(err) {
		t.Fatalf("invalid saga mutation created writer lock: %v", err)
	}
}

func TestMutationValidationDoesNotParseCoverageMappings(t *testing.T) {
	root := newTestSaga(t)
	// This file is deliberately not valid coverage JSON. Review mutations are
	// guarded by the manifest/package skeleton and review-only state; parsing
	// every mapping here would put a 529k-record saga back on the comment path.
	if err := os.MkdirAll(filepath.Join(root, "overview.fragment", "___diffs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "overview.fragment", "___diffs", "large.json"), []byte("not parsed by review mutation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddReview(root, root, "approved", "Review state is independent."); err != nil {
		t.Fatalf("review mutation parsed the coverage generation: %v", err)
	}
	assertEntryCount(t, filepath.Join(root, "___approvals"), 1)
}

func TestReservedSymlinkRejectionHasZeroOutsideSideEffects(t *testing.T) {
	root := newTestSaga(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "___review")); err != nil {
		t.Fatal(err)
	}
	_, err := AddThread(root, "urn:change-saga:test:fragment:overview", "escape", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err == nil {
		t.Fatal("AddThread accepted symlinked reserved metadata directory")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected symlink caused %d outside side effects, want zero", len(entries))
	}
}

func runConcurrently(t *testing.T, operations ...func() error) {
	t.Helper()
	var group sync.WaitGroup
	errors := make(chan error, len(operations))
	for _, operation := range operations {
		group.Add(1)
		go func(operation func() error) {
			defer group.Done()
			errors <- operation()
		}(operation)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func readReviewFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertEntryCount(t *testing.T, path string, want int) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != want {
		t.Fatalf("%s has %d entries, want %d (err=%v)", path, len(entries), want, err)
	}
}

func assertNoCommittedOrTemporaryEntries(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed entity left entries in %s: %v", path, entries)
	}
}

// testRepository is the source repository every test saga declares, so diff
// identities in these tests are the saga's own unless a test deliberately
// crosses repositories.
const testRepository = "https://example.test/repo.git"

// treeSnapshot renders every path and file size under root so a test can prove a
// rejected mutation created, removed, or rewrote nothing at all.
func treeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if entry.IsDir() {
			lines = append(lines, rel+"/")
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		lines = append(lines, fmt.Sprintf("%s %d", rel, info.Size()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func newTestSaga(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "test.saga")
	fragmentDir := filepath.Join(root, "overview.fragment")
	if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := saga.Manifest{Schema: saga.SchemaURL, Version: saga.CurrentVersion, ID: "test", Title: "Test", Source: saga.Source{Repository: testRepository, Base: "main", Head: "HEAD"}}
	if err := store.WriteJSON(filepath.Join(root, "saga.json"), manifest, true); err != nil {
		t.Fatal(err)
	}
	fragment := saga.FragmentManifest{Version: saga.CurrentVersion, ID: "overview", Title: "Overview", MediaType: "text/markdown", Entrypoint: "content.md"}
	if err := store.WriteJSON(filepath.Join(fragmentDir, "fragment.json"), fragment, true); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile(filepath.Join(fragmentDir, "content.md"), []byte("Overview\n"), 0o644, true); err != nil {
		t.Fatal(err)
	}
	return root
}
