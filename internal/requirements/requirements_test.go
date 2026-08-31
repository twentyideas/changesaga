package requirements

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/internal/store"
)

var testTime = time.Date(2026, 8, 31, 19, 0, 0, 0, time.UTC)

func TestRevisionDAGPreservesMergeConflictUntilAllHeadsReconciled(t *testing.T) {
	root := newSaga(t)
	added, err := AddStory(root, "test", storyInput("checkout", "r1", "created", []Criterion{{ID: "fast", Statement: "Checkout finishes quickly"}}))
	if err != nil {
		t.Fatal(err)
	}
	if added.URN != "urn:change-saga:test:story:checkout" {
		t.Fatalf("story URN = %q", added.URN)
	}
	parent := "urn:change-saga:test:story:checkout:revision:r1"
	story := "urn:change-saga:test:story:checkout"
	_, err = ReviseStory(root, "test", ReviseStoryInput{
		Story: story, ID: "r2-a", Parents: []string{parent}, Title: "Checkout A", Statement: "As a buyer, I check out", Priority: "high",
		AcceptanceCriteria: []Criterion{{ID: "fast", Statement: "Checkout finishes in two seconds"}}, CreatedAt: testTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate Git merging a concurrent branch. Both branch-local writers saw r1
	// as the only head, so both children are valid and neither wins by timestamp.
	concurrent := Revision{
		Schema: RevisionSchemaURL, Version: Version, ID: "r2-b", Story: story, Parents: []string{parent},
		Title: "Checkout B", Statement: "As a buyer, I can purchase", Priority: "critical", Citations: []string{},
		AcceptanceCriteria: []Criterion{{ID: "fast", Statement: "Checkout finishes promptly"}}, CreatedAt: testTime.Add(2 * time.Minute),
	}
	path := filepath.Join(root, "___requirements", "stories", "checkout.story", "revisions", "r2-b.json")
	if err := store.WriteJSON(path, concurrent, true); err != nil {
		t.Fatal(err)
	}

	document, err := Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	loaded := document.Stories[0]
	if !loaded.RevisionConflict() || loaded.CurrentRevision != nil || len(loaded.RevisionHeads) != 2 {
		t.Fatalf("conflict projection = heads %v current %#v", loaded.RevisionHeads, loaded.CurrentRevision)
	}

	_, err = ReviseStory(root, "test", ReviseStoryInput{
		Story: story, ID: "r3", Parents: loaded.RevisionHeads, Title: "Reconciled checkout", Statement: "As a buyer, I purchase", Priority: "high",
		AcceptanceCriteria: []Criterion{{ID: "fast", Statement: "Checkout finishes promptly"}}, CreatedAt: testTime.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err = Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	loaded = document.Stories[0]
	if loaded.RevisionConflict() || loaded.CurrentRevision == nil || loaded.CurrentRevision.ID != "r3" {
		t.Fatalf("reconciled projection = heads %v current %#v", loaded.RevisionHeads, loaded.CurrentRevision)
	}
}

func TestStoryCreationIsAtomicAndRequestReplayIsIdempotent(t *testing.T) {
	root := newSaga(t)
	input := storyInput("checkout", "r1", "created", []Criterion{{ID: "works", Statement: "It works"}})
	first, err := AddStory(root, "test", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AddStory(root, "test", input)
	if err != nil || !second.Replayed || second.URN != first.URN {
		t.Fatalf("story replay = %#v, %v", second, err)
	}
	_, err = ReviseStory(root, "test", ReviseStoryInput{
		Story: first.URN, ID: "r2", Parents: []string{"urn:change-saga:test:story:checkout:revision:r1"},
		Title: "Checkout revised", Statement: "The story evolved", Priority: "high",
		AcceptanceCriteria: []Criterion{{ID: "works", Statement: "It still works"}}, CreatedAt: testTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err = AddStory(root, "test", input)
	if err != nil || !second.Replayed {
		t.Fatalf("story replay after later revision = %#v, %v", second, err)
	}

	missingCitation := storyInput("invalid", "r1", "created", nil)
	missingCitation.Citations = []string{"urn:change-saga:test:citation:missing"}
	if _, err := AddStory(root, "test", missingCitation); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing citation error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "___requirements", "stories", "invalid.story")); !os.IsNotExist(err) {
		t.Fatalf("failed package mutation was visible: %v", err)
	}
}

func TestRemovedCriterionIDCannotBeReused(t *testing.T) {
	root := newSaga(t)
	_, err := AddStory(root, "test", storyInput("checkout", "r1", "created", []Criterion{{ID: "stable", Statement: "Original meaning"}}))
	if err != nil {
		t.Fatal(err)
	}
	story := "urn:change-saga:test:story:checkout"
	_, err = ReviseStory(root, "test", ReviseStoryInput{
		Story: story, ID: "r2", Parents: []string{"urn:change-saga:test:story:checkout:revision:r1"},
		Title: "Checkout", Statement: "Changed", Priority: "high", AcceptanceCriteria: []Criterion{}, CreatedAt: testTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReviseStory(root, "test", ReviseStoryInput{
		Story: story, ID: "r3", Parents: []string{"urn:change-saga:test:story:checkout:revision:r2"},
		Title: "Checkout", Statement: "Changed again", Priority: "high",
		AcceptanceCriteria: []Criterion{{ID: "stable", Statement: "A different meaning"}}, CreatedAt: testTime.Add(2 * time.Minute),
	})
	if err == nil || !strings.Contains(err.Error(), "reuses removed criterion") {
		t.Fatalf("reuse error = %v", err)
	}
}

func TestLifecycleIsAppendOnlyAndAcceptedRequiresCriteria(t *testing.T) {
	root := newSaga(t)
	_, err := AddStory(root, "test", storyInput("empty", "r1", "created", nil))
	if err != nil {
		t.Fatal(err)
	}
	story := "urn:change-saga:test:story:empty"
	created := "urn:change-saga:test:story:empty:event:created"
	_, err = SetStoryState(root, "test", SetStoryStateInput{
		Story: story, ID: "accepted", Parents: []string{created}, State: StateAccepted, CreatedAt: testTime.Add(time.Minute),
	})
	if err == nil || !strings.Contains(err.Error(), "accepted story") {
		t.Fatalf("accept empty story error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "___requirements", "stories", "empty.story", "events", "accepted.json")); !os.IsNotExist(statErr) {
		t.Fatalf("rejected lifecycle mutation left a record: %v", statErr)
	}
}

func TestLifecycleMergeConflictRequiresEveryHead(t *testing.T) {
	root := newSaga(t)
	_, err := AddStory(root, "test", storyInput("checkout", "r1", "created", []Criterion{{ID: "works", Statement: "It works"}}))
	if err != nil {
		t.Fatal(err)
	}
	story := "urn:change-saga:test:story:checkout"
	created := "urn:change-saga:test:story:checkout:event:created"
	_, err = SetStoryState(root, "test", SetStoryStateInput{
		Story: story, ID: "accepted", Parents: []string{created}, State: StateAccepted, CreatedAt: testTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	concurrent := LifecycleEvent{
		Schema: LifecycleEventSchemaURL, Version: Version, ID: "rejected", Story: story, Parents: []string{created},
		State: StateRejected, Reason: "A concurrent terminal decision", CreatedAt: testTime.Add(2 * time.Minute),
	}
	path := filepath.Join(root, "___requirements", "stories", "checkout.story", "events", "rejected.json")
	if err := store.WriteJSON(path, concurrent, true); err != nil {
		t.Fatal(err)
	}
	document, err := Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	loaded := document.Stories[0]
	if !loaded.LifecycleConflict() || loaded.CurrentLifecycle != nil {
		t.Fatalf("lifecycle conflict projection = heads %v current %#v", loaded.LifecycleHeads, loaded.CurrentLifecycle)
	}
	_, err = SetStoryState(root, "test", SetStoryStateInput{
		Story: story, ID: "reconciled", Parents: loaded.LifecycleHeads, State: StateAccepted,
		Reason: "Reconcile both decisions", CreatedAt: testTime.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err = Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	loaded = document.Stories[0]
	if loaded.LifecycleConflict() || loaded.CurrentLifecycle == nil || loaded.CurrentLifecycle.ID != "reconciled" {
		t.Fatalf("reconciled lifecycle = heads %v current %#v", loaded.LifecycleHeads, loaded.CurrentLifecycle)
	}
}

func TestCitationIsImmutableAndRequestReplayIsIdempotent(t *testing.T) {
	root := newSaga(t)
	input := AddCitationInput{ID: "policy", Kind: CitationURL, Title: "Policy", Reference: "https://example.com/policy", CreatedAt: testTime, RequestID: "request-1"}
	first, err := AddCitation(root, "test", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AddCitation(root, "test", input)
	if err != nil || !second.Replayed || first.URN != second.URN {
		t.Fatalf("replay = %#v, %v", second, err)
	}
	input.Title = "Rewritten"
	if _, err := AddCitation(root, "test", input); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("citation rewrite error = %v", err)
	}
}

func TestTypedRelationPinsExposeStaleInputsAndSupersede(t *testing.T) {
	root := newSaga(t)
	_, err := AddStory(root, "test", storyInput("checkout", "r1", "created", []Criterion{{ID: "fast", Statement: "Fast"}}))
	if err != nil {
		t.Fatal(err)
	}
	design := "urn:change-saga:test:fragment:checkout-flow"
	criterion := "urn:change-saga:test:story:checkout:criterion:fast"
	revision := "urn:change-saga:test:story:checkout:revision:r1"
	digest := "sha256:" + strings.Repeat("a", 64)
	result, err := AddRelation(root, "test", AddRelationInput{
		ID: "designs-fast", Type: RelationAddresses, From: design, To: criterion, Rationale: "The flow addresses latency.",
		FromContentDigest: digest, ToRevision: revision, CreatedAt: testTime, RequestID: "relation-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := LoadWithOptions(root, "test", LoadOptions{StaleInputs: StaleInputs{
		CurrentContentDigests: map[string]string{design: "sha256:" + strings.Repeat("b", 64)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !document.Relations[0].Stale || !strings.Contains(strings.Join(document.Relations[0].StaleReasons, ","), "content digest changed") {
		t.Fatalf("stale projection = %#v", document.Relations[0])
	}

	_, err = ReviseStory(root, "test", ReviseStoryInput{
		Story: "urn:change-saga:test:story:checkout", ID: "r2", Parents: []string{revision}, Title: "Checkout", Statement: "Updated", Priority: "high",
		AcceptanceCriteria: []Criterion{{ID: "fast", Statement: "Still fast"}}, CreatedAt: testTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err = Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !document.Relations[0].Stale || !strings.Contains(strings.Join(document.Relations[0].StaleReasons, ","), "revision changed") {
		t.Fatalf("revision stale projection = %#v", document.Relations[0])
	}
	superseded, err := SupersedeRelation(root, "test", result.URN, testTime.Add(2*time.Minute), "supersede-1")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := SupersedeRelation(root, "test", result.URN, time.Time{}, "supersede-1")
	if err != nil || !replayed.Replayed || superseded.URN != replayed.URN {
		t.Fatalf("supersede replay = %#v, %v", replayed, err)
	}
	document, err = Load(root, "test")
	if err != nil || document.Relations[0].State != RelationSuperseded || document.Relations[0].Stale {
		t.Fatalf("superseded relation = %#v, %v", document.Relations, err)
	}
}

func TestIdentityRelationAllowsOptionalPins(t *testing.T) {
	root := newSaga(t)
	_, err := AddStory(root, "test", storyInput("parent", "r1", "created", nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = AddStory(root, "test", storyInput("child", "r1", "created", nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = AddRelation(root, "test", AddRelationInput{
		ID: "child-refines-parent", Type: RelationRefines,
		From: "urn:change-saga:test:story:child", To: "urn:change-saga:test:story:parent",
		Rationale: "The child makes the broad obligation concrete.", CreatedAt: testTime,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRelationToRemovedCriterionIsStale(t *testing.T) {
	root := newSaga(t)
	_, err := AddStory(root, "test", storyInput("checkout", "r1", "created", []Criterion{{ID: "fast", Statement: "Fast"}}))
	if err != nil {
		t.Fatal(err)
	}
	design := "urn:change-saga:test:fragment:checkout-flow"
	digest := "sha256:" + strings.Repeat("a", 64)
	_, err = AddRelation(root, "test", AddRelationInput{
		ID: "designs-fast", Type: RelationAddresses, From: design,
		To: "urn:change-saga:test:story:checkout:criterion:fast", Rationale: "Designs latency.",
		FromContentDigest: digest, ToRevision: "urn:change-saga:test:story:checkout:revision:r1", CreatedAt: testTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReviseStory(root, "test", ReviseStoryInput{
		Story: "urn:change-saga:test:story:checkout", ID: "r2",
		Parents: []string{"urn:change-saga:test:story:checkout:revision:r1"}, Title: "Checkout", Statement: "Updated",
		Priority: "high", AcceptanceCriteria: []Criterion{}, CreatedAt: testTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !document.Relations[0].Stale || !strings.Contains(strings.Join(document.Relations[0].StaleReasons, ","), "criterion is absent") {
		t.Fatalf("removed criterion projection = %#v", document.Relations[0])
	}
}

func TestRelationTypeMatrixRejectsInvalidEndpoints(t *testing.T) {
	root := newSaga(t)
	_, err := AddRelation(root, "test", AddRelationInput{
		ID: "invalid", Type: RelationAddresses,
		From: "urn:change-saga:test:work-item:one", To: "urn:change-saga:test:work-item:two",
		Rationale: "Wrong kinds", FromRevision: "urn:change-saga:test:work-item:one:revision:r1",
		ToRevision: "urn:change-saga:test:work-item:two:revision:r1", CreatedAt: testTime,
	})
	if err == nil || !strings.Contains(err.Error(), "addresses relation") {
		t.Fatalf("invalid relation error = %v", err)
	}
}

func TestStrictLoadingRejectsSymlinkWithoutMutationSideEffects(t *testing.T) {
	root := newSaga(t)
	out := t.TempDir()
	if err := os.Symlink(out, filepath.Join(root, "___requirements")); err != nil {
		t.Fatal(err)
	}
	_, err := AddCitation(root, "test", AddCitationInput{
		ID: "policy", Kind: CitationDecision, Title: "Decision", Reference: "ADR-1", CreatedAt: testTime,
	})
	if err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("symlink mutation error = %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside directory changed: entries=%v err=%v", entries, err)
	}
}

func TestLoaderRejectsUnknownFieldsAndOversizedRecord(t *testing.T) {
	root := newSaga(t)
	_, err := AddCitation(root, "test", AddCitationInput{ID: "policy", Kind: CitationDecision, Title: "Decision", Reference: "ADR-1", CreatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "___requirements", "citations", "policy.json")
	var value map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["unexpected"] = true
	data, _ = json.Marshal(value)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "test"); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %v", err)
	}
}

func storyInput(id, revision, event string, criteria []Criterion) AddStoryInput {
	return AddStoryInput{
		ID: id, RevisionID: revision, EventID: event, Title: "Checkout", Statement: "As a buyer, I can check out", Priority: "high",
		Citations: []string{}, AcceptanceCriteria: criteria, CreatedAt: testTime, RequestID: "request-" + id,
	}
}

func newSaga(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "test.saga")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"$schema": "https://changesaga.dev/schema/v3/saga.schema.json",
		"version": 3, "id": "test", "title": "Test",
		"source": map[string]any{"repository": "https://example.com/repo.git", "base": "main", "head": "feature"},
	}
	if err := store.WriteJSON(filepath.Join(root, "saga.json"), manifest, true); err != nil {
		t.Fatal(err)
	}
	return root
}
