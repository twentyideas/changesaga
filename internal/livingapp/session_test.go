package livingapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/internal/livingid"
	"github.com/twentyideas/changesaga/internal/readiness"
	"github.com/twentyideas/changesaga/internal/requirements"
	"github.com/twentyideas/changesaga/internal/workplan"
)

var fixtureTime = time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC)

func TestOrdinarySagaIsNotApplicableRatherThanInvalidOrBlocked(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ordinary.saga")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"version": 2, "id": "ordinary", "title": "Ordinary", "source": map[string]string{"repository": "https://example.com/repo.git", "base": "main", "head": "feature"}}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "saga.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	session := openFixture(t, root)
	requirementsResult, err := session.Query(context.Background(), Query{Operation: "requirements"})
	if err != nil || requirementsResult.Page.Total != 0 || len(requirementsResult.Data.(RequirementPage).Requirements) != 0 {
		t.Fatalf("ordinary requirements = %+v, %v", requirementsResult, err)
	}
	readinessResult, err := session.Query(context.Background(), Query{Operation: "readiness"})
	if err != nil {
		t.Fatal(err)
	}
	readiness := readinessResult.Data.(ReadinessPage)
	if readiness.Summary.Status != "not_applicable" || readiness.Summary.PeerReviewReady || readinessResult.Page.Total != 0 {
		t.Fatalf("ordinary readiness = %+v page=%+v", readiness, readinessResult.Page)
	}
}

func TestLivingQueriesAreDeterministicSnapshotBoundAndProgressIsNotProof(t *testing.T) {
	root := livingFixture(t)
	addAcceptedStory(t, root, "checkout", "fast")
	addAcceptedStory(t, root, "refund", "clear")
	createDeliveryItem(t, root, "checkout", "fast")

	session := openFixture(t, root)
	first, err := session.Query(context.Background(), Query{Operation: "requirements", Limit: 1})
	if err != nil || first.Page.Total != 2 || first.Page.NextCursor == nil {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := session.Query(context.Background(), Query{Operation: "requirements", Limit: 1, Cursor: *first.Page.NextCursor})
	if err != nil || second.Page.Returned != 1 || second.Page.HasMore {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	tampered := *first.Page.NextCursor + "A"
	_, err = session.Query(context.Background(), Query{Operation: "requirements", Limit: 1, Cursor: tampered})
	if !errorHasCode(err, CodeInvalidArgument) {
		t.Fatalf("tampered cursor error = %#v", err)
	}
	_, err = session.Query(context.Background(), Query{Operation: "waves", Limit: 1, Cursor: *first.Page.NextCursor})
	if !errorHasCode(err, CodeInvalidArgument) {
		t.Fatalf("cross-operation cursor error = %#v", err)
	}
	_, err = session.Query(context.Background(), Query{Operation: "requirements", Filters: Filters{State: "accepted"}, Limit: 1, Cursor: *first.Page.NextCursor})
	if !errorHasCode(err, CodeInvalidArgument) {
		t.Fatalf("changed-filter cursor error = %#v", err)
	}
	traceResult, err := session.Query(context.Background(), Query{Operation: "traceability"})
	if err != nil {
		t.Fatal(err)
	}
	traces := traceResult.Data.(TraceabilityPage).Criteria
	var checkout Traceability
	for _, trace := range traces {
		if strings.HasSuffix(trace.Criterion, ":fast") {
			checkout = trace
		}
	}
	if checkout.Delivered || !hasBlocker(checkout.Blockers, "immutable_evidence_missing") {
		t.Fatalf("done/planned work was treated as proof: %+v", checkout)
	}

	recordIntegratedMerge(t, root, "checkout")
	reopened := openFixture(t, root)
	_, err = reopened.Query(context.Background(), Query{Operation: "requirements", Limit: 1, Cursor: *first.Page.NextCursor})
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Code != CodeStaleSnapshot || !domainErr.Retryable {
		t.Fatalf("old cursor error = %#v", err)
	}
	traceResult, err = reopened.Query(context.Background(), Query{Operation: "traceability", Filters: Filters{Requirement: "checkout"}})
	if err != nil {
		t.Fatal(err)
	}
	checkout = traceResult.Data.(TraceabilityPage).Criteria[0]
	if !checkout.Delivered || len(checkout.Evidence) != 1 || !strings.HasPrefix(checkout.Evidence[0], "git-oid:") {
		t.Fatalf("integrated immutable evidence did not complete the path: %+v", checkout)
	}
}

func TestTransitiveDependencyBlockerPathIsExposed(t *testing.T) {
	root := livingFixture(t)
	addAcceptedStory(t, root, "checkout", "fast")
	createDeliveryItem(t, root, "checkout", "fast")
	createPlainItem(t, root, "middle")
	createPlainItem(t, root, "foundation")
	checkout, _ := livingid.WorkItem("test", "checkout")
	middle, _ := livingid.WorkItem("test", "middle")
	foundation, _ := livingid.WorkItem("test", "foundation")
	if _, err := workplan.CreateDependency(root, workplan.Dependency{ID: "middle-checkout", Prerequisite: middle, Dependent: checkout, Condition: workplan.DependencyCondition{Kind: "progress_done"}, Reason: "middle first"}, "dep-middle"); err != nil {
		t.Fatal(err)
	}
	if _, err := workplan.CreateDependency(root, workplan.Dependency{ID: "foundation-middle", Prerequisite: foundation, Dependent: middle, Condition: workplan.DependencyCondition{Kind: "merge_integrated"}, Reason: "foundation first"}, "dep-foundation"); err != nil {
		t.Fatal(err)
	}
	result, err := openFixture(t, root).Query(context.Background(), Query{Operation: "traceability"})
	if err != nil {
		t.Fatal(err)
	}
	trace := result.Data.(TraceabilityPage).Criteria[0]
	if len(trace.TransitiveBlockerPaths) < 2 {
		t.Fatalf("transitive paths = %+v", trace.TransitiveBlockerPaths)
	}
	deep := false
	for _, blocker := range trace.TransitiveBlockerPaths {
		deep = deep || len(blocker.Path) >= 3
	}
	if !deep {
		t.Fatalf("deep path missing: %+v", trace.TransitiveBlockerPaths)
	}
}

func TestMissingCrossDomainEndpointsMakeRelationsAndPathsStale(t *testing.T) {
	root := livingFixture(t)
	addAcceptedStory(t, root, "checkout", "fast")
	createDeliveryItem(t, root, "checkout", "fast")
	if err := os.RemoveAll(filepath.Join(root, "checkout-design.fragment")); err != nil {
		t.Fatal(err)
	}
	session := openFixture(t, root)
	result, err := session.Query(context.Background(), Query{Operation: "relations", Filters: Filters{State: "stale"}})
	if err != nil {
		t.Fatal(err)
	}
	relations := result.Data.(RelationPage).Relations
	if len(relations) != 2 {
		t.Fatalf("stale relations = %+v", relations)
	}
	traceResult, err := session.Query(context.Background(), Query{Operation: "traceability"})
	if err != nil {
		t.Fatal(err)
	}
	trace := traceResult.Data.(TraceabilityPage).Criteria[0]
	if len(trace.Design) != 0 || !hasBlocker(trace.Blockers, "design_missing") {
		t.Fatalf("stale design still participated in traceability: %+v", trace)
	}
}

func livingFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "test.saga")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"___requirements", "___design", "___workplan"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := map[string]any{"$schema": "https://changesaga.dev/schema/v3/saga.schema.json", "version": 3, "id": "test", "title": "Test", "source": map[string]string{"repository": "https://example.com/repo.git", "base": "main", "head": "feature"}}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "saga.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func addAcceptedStory(t *testing.T, root, id, criterion string) {
	t.Helper()
	if _, err := requirements.AddStory(root, "test", requirements.AddStoryInput{ID: id, RevisionID: "r1", EventID: "proposed", Title: id, Statement: "Deliver " + id, Priority: "high", Citations: []string{}, AcceptanceCriteria: []requirements.Criterion{{ID: criterion, Statement: "Criterion " + criterion}}, CreatedAt: fixtureTime, RequestID: "story-" + id}); err != nil {
		t.Fatal(err)
	}
	story, _ := livingid.Story("test", id)
	parent, _ := requirements.StoryEventURN("test", id, "proposed")
	if _, err := requirements.SetStoryState(root, "test", requirements.SetStoryStateInput{Story: story, ID: "accepted", Parents: []string{parent}, State: requirements.StateAccepted, CreatedAt: fixtureTime.Add(time.Minute), RequestID: "accept-" + id}); err != nil {
		t.Fatal(err)
	}
}

func createPlainItem(t *testing.T, root, id string) {
	t.Helper()
	revision := workplan.WorkItemRevision{ID: "r1", Title: id, Objective: "Deliver " + id, Deliverables: []string{id}, Relations: []string{}, Dependencies: []string{}, Contracts: []string{}, ExpectedTouchAreas: []workplan.TouchArea{}, CompletionChecks: []string{}, MergeUnits: []workplan.MergeUnit{{ID: "main", Repository: "https://example.com/repo.git", SourceBranch: "feature/" + id, TargetBranch: "main", Required: true}}}
	if _, err := workplan.CreateWorkItem(root, id, revision, "item-"+id); err != nil {
		t.Fatal(err)
	}
}

func createDeliveryItem(t *testing.T, root, storyID, criterionID string) {
	t.Helper()
	createPlainItem(t, root, storyID)
	criterion, _ := livingid.Criterion("test", storyID, criterionID)
	revision, _ := livingid.Revision("test", storyID, "r1")
	design, _ := livingid.Design("test", livingid.DesignFragment, storyID+"-design")
	designDir := filepath.Join(root, storyID+"-design.fragment")
	if err := os.MkdirAll(designDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"version": 2, "id": storyID + "-design", "media_type": "text/plain", "entrypoint": "content.txt"}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(designDir, "fragment.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designDir, "content.txt"), []byte("current design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	item, _ := livingid.WorkItem("test", storyID)
	itemRevision, _ := livingid.DefinitionRevision("test", livingid.KindWorkItem, storyID, "r1")
	digest := "sha256:" + strings.Repeat("a", 64)
	if _, err := requirements.AddRelation(root, "test", requirements.AddRelationInput{ID: "addresses-" + storyID, Type: requirements.RelationAddresses, From: design, To: criterion, Rationale: "current design", FromContentDigest: digest, ToRevision: revision, CreatedAt: fixtureTime, RequestID: "addresses-" + storyID}); err != nil {
		t.Fatal(err)
	}
	if _, err := requirements.AddRelation(root, "test", requirements.AddRelationInput{ID: "implements-" + storyID, Type: requirements.RelationImplements, From: item, To: design, Rationale: "current plan", FromRevision: itemRevision, ToContentDigest: digest, CreatedAt: fixtureTime, RequestID: "implements-" + storyID}); err != nil {
		t.Fatal(err)
	}
}

func recordIntegratedMerge(t *testing.T, root, item string) {
	t.Helper()
	planned, err := workplan.RecordMerge(root, item, workplan.MergeEvent{EventBase: workplan.EventBase{Parents: []string{}}, Unit: "main", State: "planned"}, "merge-planned")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := workplan.RecordMerge(root, item, workplan.MergeEvent{EventBase: workplan.EventBase{Parents: []string{planned.URN}}, Unit: "main", State: "ready", HeadOID: strings.Repeat("a", 40)}, "merge-ready")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workplan.RecordMerge(root, item, workplan.MergeEvent{EventBase: workplan.EventBase{Parents: []string{ready.URN}}, Unit: "main", State: "integrated", HeadOID: strings.Repeat("a", 40), MergeOID: strings.Repeat("b", 40)}, "merge-integrated"); err != nil {
		t.Fatal(err)
	}
}

func openFixture(t *testing.T, root string) Session {
	t.Helper()
	session, err := Open(context.Background(), OpenOptions{SagaRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	return session
}
func hasBlocker(values []readiness.Blocker, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

func errorHasCode(err error, code ErrorCode) bool {
	var domainErr *Error
	return errors.As(err, &domainErr) && domainErr.Code == code
}
