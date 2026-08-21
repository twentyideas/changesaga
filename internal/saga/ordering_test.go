package saga

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"
	"time"
)

// SPEC.md resolves thread state, approval state, and reviewed state from the
// latest record by created_at. Two records may legitimately share a timestamp —
// hand-authored history often has second granularity — so "latest" is only well
// defined once ties break on the record id. Before that rule existed, the file
// name decided the answer, which meant two checkouts of the same commit could
// disagree about whether a thread was resolved.

func threadWithEvents(t *testing.T, events map[string]string) string {
	t.Helper()
	files := map[string]string{
		"___review/threads/t1.thread/thread.json":                                     `{"version":2,"id":"t1","target":"urn:change-saga:test:fragment:overview","anchor":{"type":"target"},"created_at":"2026-08-19T12:00:00Z"}`,
		"___review/threads/t1.thread/messages/m1.message/message.json":                `{"version":2,"id":"m1","created_at":"2026-08-19T12:00:00Z"}`,
		"___review/threads/t1.thread/messages/m1.message/body.fragment/fragment.json": `{"version":2,"id":"m1-body","media_type":"text/markdown","entrypoint":"content.md"}`,
		"___review/threads/t1.thread/messages/m1.message/body.fragment/content.md":    "hi\n",
	}
	for name, body := range events {
		files["___review/threads/t1.thread/events/"+name] = body
	}
	return buildSaga(t, files)
}

func TestThreadStateTieBreaksOnEventIDNotFilename(t *testing.T) {
	const stamp = "2026-08-19T12:05:00Z"
	event := func(id, state string) string {
		return fmt.Sprintf(`{"version":2,"id":%q,"state":%q,"created_at":%q}`, id, state, stamp)
	}
	// Identical content, opposite file names. The resolved state must not move.
	cases := []map[string]string{
		{"aaa.json": event("zz-open", "open"), "zzz.json": event("aa-resolved", "resolved")},
		{"zzz.json": event("zz-open", "open"), "aaa.json": event("aa-resolved", "resolved")},
		{"m.json": event("aa-resolved", "resolved"), "n.json": event("zz-open", "open")},
	}
	for i, events := range cases {
		document, validation, err := Load(threadWithEvents(t, events))
		if err != nil {
			t.Fatal(err)
		}
		if !validation.Valid {
			t.Fatalf("case %d: %#v", i, validation.Issues)
		}
		// zz-open sorts last among equal timestamps, so the thread is open.
		if state := document.Threads[0].State; state != "open" {
			t.Fatalf("case %d: thread state %q; the highest event id must win a created_at tie", i, state)
		}
		if got := document.Threads[0].Events[len(document.Threads[0].Events)-1].ID; got != "zz-open" {
			t.Fatalf("case %d: last event %q", i, got)
		}
	}
}

func TestAnchorEditTieBreaksDeterministically(t *testing.T) {
	const stamp = "2026-08-19T12:05:00Z"
	anchorEvent := func(id string, x float64) string {
		return fmt.Sprintf(`{"version":2,"id":%q,"anchor":{"type":"region","coordinate_space":"normalized","shapes":[{"type":"rect","x":%v,"y":0.1,"width":0.1,"height":0.1}]},"created_at":%q}`, id, x, stamp)
	}
	for i, events := range []map[string]string{
		{"a.json": anchorEvent("e-1", 0.1), "b.json": anchorEvent("e-2", 0.9)},
		{"b.json": anchorEvent("e-1", 0.1), "a.json": anchorEvent("e-2", 0.9)},
	} {
		document, validation, err := Load(threadWithEvents(t, events))
		if err != nil {
			t.Fatal(err)
		}
		if !validation.Valid {
			t.Fatalf("case %d: %#v", i, validation.Issues)
		}
		if x := document.Threads[0].Anchor.Shapes[0].X; x != 0.9 {
			t.Fatalf("case %d: anchor x %v; the highest event id must win a created_at tie", i, x)
		}
	}
}

func TestApprovalsAndDiffReviewsAreOrderedDeterministically(t *testing.T) {
	const stamp = "2026-08-19T12:05:00Z"
	uri := "saga-diff://v1/file?base=aaa&head=bbb&path=api.go&repository=https%3A%2F%2Fexample.test%2Facme%2Fapp.git"
	for i, names := range [][2]string{{"z.json", "a.json"}, {"a.json", "z.json"}} {
		root := buildSaga(t, map[string]string{
			"___approvals/" + names[0]:    fmt.Sprintf(`{"version":2,"id":"zz-second","state":"approved","created_at":%q}`, stamp),
			"___approvals/" + names[1]:    fmt.Sprintf(`{"version":2,"id":"aa-first","state":"rejected","created_at":%q}`, stamp),
			"___review/diffs/" + names[0]: fmt.Sprintf(`{"version":2,"id":"zz-second","uri":%q,"state":"reviewed","created_at":%q}`, uri, stamp),
			"___review/diffs/" + names[1]: fmt.Sprintf(`{"version":2,"id":"aa-first","uri":%q,"state":"unreviewed","created_at":%q}`, uri, stamp),
		})
		document, validation, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if !validation.Valid {
			t.Fatalf("case %d: %#v", i, validation.Issues)
		}
		reviews := document.Section.Reviews
		if len(reviews) != 2 || reviews[0].ID != "aa-first" || reviews[1].ID != "zz-second" {
			t.Fatalf("case %d: approvals ordered %v", i, reviews)
		}
		diffReviews := document.DiffReviews
		if len(diffReviews) != 2 || diffReviews[0].ID != "aa-first" || diffReviews[1].ID != "zz-second" {
			t.Fatalf("case %d: diff reviews ordered %v", i, diffReviews)
		}
	}
}

// TestLoadOrderIsIndependentOfRecordFilenames shuffles the file names of a
// larger overlay many times and asserts the loaded document is byte-identical
// each round. It is a property check over the whole load path rather than one
// sort comparator.
func TestLoadOrderIsIndependentOfRecordFilenames(t *testing.T) {
	random := rand.New(rand.NewSource(20260820))
	base := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	type record struct{ id, body string }
	var events []record
	for i := 0; i < 12; i++ {
		state := []string{"open", "resolved", "withdrawn"}[i%3]
		// Deliberately coarse timestamps so most records collide.
		stamp := base.Add(time.Duration(i/4) * time.Minute).Format(time.RFC3339)
		id := fmt.Sprintf("event-%02d", i)
		events = append(events, record{id: id, body: fmt.Sprintf(`{"version":2,"id":%q,"state":%q,"created_at":%q}`, id, state, stamp)})
	}

	var reference string
	for round := 0; round < 25; round++ {
		names := random.Perm(len(events))
		overlay := map[string]string{}
		for i, event := range events {
			overlay[fmt.Sprintf("%03d-%s.json", names[i], event.id)] = event.body
		}
		document, validation, err := Load(threadWithEvents(t, overlay))
		if err != nil {
			t.Fatal(err)
		}
		if !validation.Valid {
			t.Fatalf("round %d: %#v", round, validation.Issues)
		}
		var rendered string
		for _, event := range document.Threads[0].Events {
			rendered += event.ID + ":" + event.State + ";"
		}
		rendered += "state=" + document.Threads[0].State
		if round == 0 {
			reference = rendered
			continue
		}
		if rendered != reference {
			t.Fatalf("round %d loaded %q, first round loaded %q", round, rendered, reference)
		}
	}
	if reference == "" {
		t.Fatal("no events were loaded")
	}
}

func TestFragmentsAndSectionsSortStablyOnEqualOrder(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"zebra", "alpha", "middle"} {
		files["one.chapter/"+name+".fragment/fragment.json"] = fmt.Sprintf(`{"version":2,"id":%q,"title":%q,"media_type":"text/markdown","entrypoint":"content.md","order":5}`, name, name)
		files["one.chapter/"+name+".fragment/content.md"] = "Body.\n"
		files["one.chapter/"+name+"/section.json"] = fmt.Sprintf(`{"version":2,"id":%q,"title":%q,"order":5}`, "s-"+name, name)
	}
	files["one.chapter/chapter.json"] = `{"version":2,"id":"one","title":"One"}`
	root := buildSaga(t, files)
	for round := 0; round < 5; round++ {
		document, validation, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if !validation.Valid {
			t.Fatalf("%#v", validation.Issues)
		}
		chapter := document.Section.Children[0]
		var fragments, sections string
		for _, fragment := range chapter.Fragments {
			fragments += filepath.Base(fragment.Path) + ","
		}
		for _, child := range chapter.Children {
			sections += filepath.Base(child.Path) + ","
		}
		if fragments != "alpha.fragment,middle.fragment,zebra.fragment," {
			t.Fatalf("equal-order fragments must fall back to path order, got %q", fragments)
		}
		if sections != "alpha,middle,zebra," {
			t.Fatalf("equal-order sections must fall back to path order, got %q", sections)
		}
	}
}
