package reviewstore

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/saga"
)

func TestReviewRecordsAreAppendOnlyAndFileGranular(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := "urn:review-saga:test:fragment:overview"
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

	fileURI, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/a.git", Base: "aaa", Head: "bbb", Kind: "file", Path: "app.go"})
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
	root := filepath.Join(t.TempDir(), "test.saga")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	threadID, err := AddThread(root, "urn:review-saga:test:fragment:overview", "Keep this history", saga.Anchor{Type: "target"}, "comment", "", nil)
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
	root := filepath.Join(t.TempDir(), "test.saga")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := "urn:review-saga:test:fragment:overview"
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
