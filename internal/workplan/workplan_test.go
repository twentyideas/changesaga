package workplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/internal/livingid"
	"github.com/twentyideas/changesaga/internal/store"
)

var testNow = time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC)

func TestWavesDoNotCreateImplicitBarriersAndDependenciesFormADAG(t *testing.T) {
	root := newV3Saga(t)
	mutator := testMutator()
	createWave(t, mutator, root, "later", 20)
	createWave(t, mutator, root, "earlier", 10)
	createItem(t, mutator, root, "storage", "later", nil)
	createItem(t, mutator, root, "query", "earlier", nil)
	createItem(t, mutator, root, "viewer", "earlier", nil)

	storage := mustWorkItemURN(t, "test", "storage")
	query := mustWorkItemURN(t, "test", "query")
	viewer := mustWorkItemURN(t, "test", "viewer")
	if _, err := mutator.CreateDependency(root, Dependency{ID: "storage-query", Prerequisite: storage, Dependent: query, Condition: DependencyCondition{Kind: "progress_done"}, Reason: "Query reads storage."}, "dep-1"); err != nil {
		t.Fatalf("cross-wave dependency: %v", err)
	}
	if _, err := mutator.CreateDependency(root, Dependency{ID: "query-viewer", Prerequisite: query, Dependent: viewer, Condition: DependencyCondition{Kind: "merge_integrated"}, Reason: "Viewer consumes query."}, "dep-2"); err != nil {
		t.Fatalf("second dependency: %v", err)
	}
	if _, err := mutator.CreateDependency(root, Dependency{ID: "cycle", Prerequisite: viewer, Dependent: storage, Condition: DependencyCondition{Kind: "progress_done"}, Reason: "Would close the cycle."}, "dep-3"); err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, RootDir, "dependencies", "cycle.dependency")); !os.IsNotExist(err) {
		t.Fatalf("invalid dependency became visible: %v", err)
	}
	plan, validation, err := Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("Load: err=%v validation=%+v", err, validation)
	}
	if len(plan.Dependencies) != 2 {
		t.Fatalf("dependencies = %d, want 2", len(plan.Dependencies))
	}
}

func TestRevisionAndProgressGraphsRetainConcurrentHeadsUntilReconciled(t *testing.T) {
	root := newV3Saga(t)
	mutator := testMutator()
	createItem(t, mutator, root, "core", "", nil)
	plan := mustLoad(t, root)
	item := plan.WorkItems["core"]
	parentRevision := item.Heads[0]
	base := item.Revisions[0]
	for index, id := range []string{"scope-a", "scope-b"} {
		revision := base
		revision.ID = id
		revision.Parents = []string{parentRevision}
		revision.Title = "Concurrent scope " + id
		revision.CreatedAt = testNow.Add(time.Duration(index+1) * time.Minute)
		revision.RequestID, revision.RequestDigest = "", ""
		writeRawRevision(t, root, "core", revision)
	}

	plan = mustLoad(t, root)
	if len(plan.WorkItems["core"].Heads) != 2 || plan.WorkItems["core"].CurrentRevision != nil || !hasConflict(plan, "work_item_revision_heads") {
		t.Fatalf("revision conflict not retained: heads=%v conflicts=%v", plan.WorkItems["core"].Heads, plan.Conflicts)
	}
	reconcile := base
	reconcile.ID = "scope-reconciled"
	reconcile.WorkItem = mustWorkItemURN(t, "test", "core")
	reconcile.Parents = append([]string{}, plan.WorkItems["core"].Heads...)
	reconcile.Title = "Reconciled scope"
	reconcile.CreatedAt = time.Time{}
	reconcile.RequestID, reconcile.RequestDigest = "", ""
	if _, err := mutator.ReviseWorkItem(root, reconcile, "revision-reconcile"); err != nil {
		t.Fatalf("reconcile revision heads: %v", err)
	}

	plan = mustLoad(t, root)
	initialHeads, _ := progressHeads(plan.WorkItems["core"])
	initial := initialHeads[0]
	for index, state := range []string{"blocked", "cancelled"} {
		event := ProgressEvent{EventBase: EventBase{Schema: EventSchema, Version: Version, ID: "branch-" + state, Parents: []string{initial}, CreatedAt: testNow.Add(time.Duration(index+4) * time.Minute)}, WorkItem: mustWorkItemURN(t, "test", "core"), State: state, Reason: "branch decision"}
		writeRawItemEvent(t, root, "core", "progress", event.ID, event)
	}
	plan = mustLoad(t, root)
	heads, _ := progressHeads(plan.WorkItems["core"])
	if len(heads) != 2 || plan.WorkItems["core"].CurrentProgress != nil || !hasConflict(plan, "progress_heads") {
		t.Fatalf("progress conflict not retained: heads=%v conflicts=%v", heads, plan.Conflicts)
	}
	result, err := mutator.RecordProgress(root, "core", ProgressEvent{EventBase: EventBase{Parents: heads}, State: "cancelled", Reason: "reconciled branch outcomes"}, "progress-reconcile")
	if err != nil {
		t.Fatalf("reconcile progress heads: %v", err)
	}
	if len(result.EventIDs) != 1 {
		t.Fatalf("reconciliation event ids = %v", result.EventIDs)
	}
	plan = mustLoad(t, root)
	heads, _ = progressHeads(plan.WorkItems["core"])
	if len(heads) != 1 || hasConflict(plan, "progress_heads") {
		t.Fatalf("progress remains conflicted: heads=%v conflicts=%v", heads, plan.Conflicts)
	}
}

func TestDependencyConditionsUseProgressMergeAndPinnedContractFacts(t *testing.T) {
	root := newV3Saga(t)
	mutator := testMutator()
	unit := MergeUnit{ID: "primary", Repository: "https://example.com/repo.git", SourceBranch: "feature/core", TargetBranch: "main", Required: true}
	createItem(t, mutator, root, "provider", "", []MergeUnit{unit})
	createItem(t, mutator, root, "consumer", "", nil)
	provider := mustWorkItemURN(t, "test", "provider")
	consumer := mustWorkItemURN(t, "test", "consumer")

	createDependency(t, mutator, root, Dependency{ID: "progress", Prerequisite: provider, Dependent: consumer, Condition: DependencyCondition{Kind: "progress_done"}, Reason: "Wait for completion."}, "condition-progress")
	createDependency(t, mutator, root, Dependency{ID: "merge", Prerequisite: provider, Dependent: consumer, Condition: DependencyCondition{Kind: "merge_integrated"}, Reason: "Wait for integration."}, "condition-merge")
	plan := mustLoad(t, root)
	progressHeads, _ := progressHeads(plan.WorkItems["provider"])
	progressHead := progressHeads[0]
	progress := recordProgress(t, mutator, root, "provider", progressHead, "in_progress", "")
	recordProgress(t, mutator, root, "provider", progress, "done", "")
	plan = mustLoad(t, root)
	if ok, reason := DependencySatisfied(plan, "progress"); !ok || reason != "" {
		t.Fatalf("progress dependency = %v %q", ok, reason)
	}
	if ok, reason := DependencySatisfied(plan, "merge"); ok || reason != "merge_conflicted" && reason != "merge_not_integrated" {
		t.Fatalf("unintegrated merge dependency = %v %q", ok, reason)
	}

	planned := recordMerge(t, mutator, root, "provider", "primary", nil, "planned", "", "")
	ready := recordMerge(t, mutator, root, "provider", "primary", []string{planned}, "ready", strings.Repeat("a", 40), "")
	recordMerge(t, mutator, root, "provider", "primary", []string{ready}, "integrated", strings.Repeat("a", 40), strings.Repeat("b", 40))
	plan = mustLoad(t, root)
	if ok, reason := DependencySatisfied(plan, "merge"); !ok || reason != "" {
		t.Fatalf("merge dependency = %v %q", ok, reason)
	}

	contractRevision := ContractRevision{ID: "v1", Kind: "interface", Provider: provider, Consumer: consumer, Statement: "Provider emits the stable envelope.", Acceptance: []string{"Consumer tests pass."}}
	if _, err := mutator.CreateContract(root, "handoff", contractRevision, "contract-create"); err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	contractRevisionURN := mustRevisionURN(t, "test", livingid.KindContract, "handoff", "v1")
	createDependency(t, mutator, root, Dependency{ID: "contract", Prerequisite: provider, Dependent: consumer, Condition: DependencyCondition{Kind: "contract_fulfilled", ContractRevision: contractRevisionURN}, Reason: "Wait for handoff."}, "condition-contract")
	plan = mustLoad(t, root)
	contractHead, _ := contractHeads(plan.Contracts["handoff"])
	accepted := recordContract(t, mutator, root, "handoff", contractHead, "accepted", "")
	recordContract(t, mutator, root, "handoff", []string{accepted}, "fulfilled", "Reproducible contract checks pass.")
	plan = mustLoad(t, root)
	if ok, reason := DependencySatisfied(plan, "contract"); !ok || reason != "" {
		t.Fatalf("fulfilled contract dependency = %v %q", ok, reason)
	}
	current := plan.Contracts["handoff"]
	revised := current.Revisions[0]
	revised.ID = "v2"
	revised.Parents = append([]string{}, current.Heads...)
	revised.Statement = "Provider emits an extended envelope."
	revised.CreatedAt = time.Time{}
	revised.RequestID, revised.RequestDigest = "", ""
	if _, err := mutator.ReviseContract(root, revised, "contract-v2"); err != nil {
		t.Fatalf("ReviseContract: %v", err)
	}
	plan = mustLoad(t, root)
	if ok, reason := DependencySatisfied(plan, "contract"); ok || reason != "contract_revision_stale" {
		t.Fatalf("stale contract dependency = %v %q", ok, reason)
	}
}

func TestRequestReplayIsIdempotentAndPayloadReuseConflicts(t *testing.T) {
	root := newV3Saga(t)
	mutator := testMutator()
	revision := WaveRevision{ID: "v1", Title: "Foundation", Objective: "Build storage.", Order: 10}
	first, err := mutator.CreateWave(root, "foundation", revision, "same-request")
	if err != nil {
		t.Fatalf("first CreateWave: %v", err)
	}
	second, err := mutator.CreateWave(root, "foundation", revision, "same-request")
	if err != nil || !second.Replayed {
		t.Fatalf("replayed CreateWave: result=%+v err=%v", second, err)
	}
	if len(second.Created) != 1 || second.Created[0] != first.Created[0] {
		t.Fatalf("replay created = %v, want %v", second.Created, first.Created)
	}
	revision.Objective = "Different payload."
	if _, err := mutator.CreateWave(root, "foundation", revision, "same-request"); err == nil || !strings.Contains(err.Error(), "different payload") {
		t.Fatalf("request reuse error = %v", err)
	}
	if got := len(mustLoad(t, root).Waves); got != 1 {
		t.Fatalf("waves after replay = %d", got)
	}
}

func TestWorkspaceAssignmentsPersistPortableIdentityAndUseParentURNs(t *testing.T) {
	root := newV3Saga(t)
	mutator := testMutator()
	createItem(t, mutator, root, "assigned", "", nil)
	workspace := Workspace{Provider: "devswarm", ID: "f9f72560-988a-44fd-8d06-1d020bae9854", RepositoryID: "08fb259f-ceb1-4a3d-9121-a0b1866edd7b", Branch: "feature/work", SourceBranch: "main", Label: "Work"}
	assigned, err := mutator.RecordWorkspace(root, "assigned", WorkspaceEvent{EventBase: EventBase{Parents: []string{}}, Action: "assigned", Role: "owner", Workspace: workspace}, "workspace-assign")
	if err != nil {
		t.Fatalf("assign workspace: %v", err)
	}
	if !strings.Contains(assigned.URN, ":workspace-event:") {
		t.Fatalf("assignment URN = %q", assigned.URN)
	}
	released, err := mutator.RecordWorkspace(root, "assigned", WorkspaceEvent{EventBase: EventBase{Parents: []string{assigned.URN}}, Action: "released", Role: "owner", Workspace: workspace}, "workspace-release")
	if err != nil {
		t.Fatalf("release workspace: %v", err)
	}
	if released.Replayed || released.URN == assigned.URN {
		t.Fatalf("release result = %+v", released)
	}
	plan := mustLoad(t, root)
	if got := plan.WorkItems["assigned"].Workspaces[1].Parents; len(got) != 1 || got[0] != assigned.URN {
		t.Fatalf("release parents = %v", got)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(released.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "worktree") || strings.Contains(string(data), "terminal") || strings.Contains(string(data), "pane") {
		t.Fatalf("workspace event persisted local/transient identity: %s", data)
	}
}

func TestLoaderRejectsSymlinksAndUnknownFieldsWithoutFollowingThem(t *testing.T) {
	root := newV3Saga(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, RootDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, RootDir, "waves")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, validation, err := Load(root)
	if err != nil || validation.Valid || !issueContains(validation, "real directory") {
		t.Fatalf("symlink validation: err=%v validation=%+v", err, validation)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("loader followed or modified symlink target: entries=%v err=%v", entries, err)
	}
}

func TestSchemasAreClosedVersionedV3Contracts(t *testing.T) {
	for _, name := range []string{"wave.schema.json", "work-item.schema.json", "dependency.schema.json", "contract.schema.json", "workplan-revision.schema.json", "workplan-event.schema.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "schema", "v3", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || !strings.HasSuffix(schema["$id"].(string), "/v3/"+name) {
			t.Errorf("%s is not published as a v3 draft-2020-12 schema", name)
		}
	}
}

func newV3Saga(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "test.saga")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"$schema": "https://changesaga.dev/schema/v3/saga.schema.json", "version": 3, "id": "test", "title": "Test", "source": map[string]string{"repository": "https://example.com/repo.git", "base": "main", "head": "feature"}}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "saga.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func testMutator() Mutator {
	sequence := 0
	return Mutator{Now: func() time.Time { sequence++; return testNow.Add(time.Duration(sequence) * time.Second) }, LockTimeout: time.Second}
}

func createWave(t *testing.T, mutator Mutator, root, id string, order int) {
	t.Helper()
	if _, err := mutator.CreateWave(root, id, WaveRevision{ID: "v1", Title: id, Objective: "Coordinate " + id + ".", Order: order}, "wave-"+id); err != nil {
		t.Fatalf("CreateWave(%s): %v", id, err)
	}
}

func createItem(t *testing.T, mutator Mutator, root, id, wave string, units []MergeUnit) {
	t.Helper()
	waveURN := ""
	if wave != "" {
		waveURN, _ = livingid.Wave("test", wave)
	}
	revision := WorkItemRevision{ID: "v1", Title: id, Objective: "Deliver " + id + ".", Deliverables: []string{id + " implementation"}, Wave: waveURN, ExpectedTouchAreas: []TouchArea{{Repository: "https://example.com/repo.git", Selector: TouchSelector{Kind: "directory", Value: "internal/" + id}, Intents: []string{"modify", "test"}}}, CompletionChecks: []string{"go test"}, MergeUnits: units}
	if _, err := mutator.CreateWorkItem(root, id, revision, "item-"+id); err != nil {
		t.Fatalf("CreateWorkItem(%s): %v", id, err)
	}
}

func createDependency(t *testing.T, mutator Mutator, root string, dependency Dependency, request string) {
	t.Helper()
	if _, err := mutator.CreateDependency(root, dependency, request); err != nil {
		t.Fatalf("CreateDependency(%s): %v", dependency.ID, err)
	}
}

func recordProgress(t *testing.T, mutator Mutator, root, item, parent, state, reason string) string {
	t.Helper()
	result, err := mutator.RecordProgress(root, item, ProgressEvent{EventBase: EventBase{Parents: []string{parent}}, State: state, Reason: reason}, "progress-"+item+"-"+state)
	if err != nil {
		t.Fatalf("RecordProgress(%s): %v", state, err)
	}
	return result.URN
}

func recordMerge(t *testing.T, mutator Mutator, root, item, unit string, parents []string, state, head, merge string) string {
	t.Helper()
	result, err := mutator.RecordMerge(root, item, MergeEvent{EventBase: EventBase{Parents: parents}, Unit: unit, State: state, HeadOID: head, MergeOID: merge}, "merge-"+item+"-"+state)
	if err != nil {
		t.Fatalf("RecordMerge(%s): %v", state, err)
	}
	return result.URN
}

func recordContract(t *testing.T, mutator Mutator, root, contract string, parents []string, state, summary string) string {
	t.Helper()
	result, err := mutator.RecordContractState(root, contract, ContractEvent{EventBase: EventBase{Parents: parents, Summary: summary}, State: state}, "contract-"+contract+"-"+state)
	if err != nil {
		t.Fatalf("RecordContractState(%s): %v", state, err)
	}
	return result.URN
}

func writeRawRevision(t *testing.T, root, itemID string, revision WorkItemRevision) {
	t.Helper()
	dir := filepath.Join(root, RootDir, "work-items", itemID+".work-item", "revisions", revision.ID+".revision")
	if err := store.CommitDir(root, dir, func(stage string) error {
		return store.WriteJSON(filepath.Join(stage, "revision.json"), revision, true)
	}); err != nil {
		t.Fatal(err)
	}
}

func writeRawItemEvent(t *testing.T, root, itemID, stream, eventID string, event any) {
	t.Helper()
	dir := filepath.Join(root, RootDir, "work-items", itemID+".work-item", "events", stream)
	if _, err := store.EnsureDirWithin(root, dir); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteJSON(filepath.Join(dir, eventID+".json"), event, true); err != nil {
		t.Fatal(err)
	}
}

func mustLoad(t *testing.T, root string) Plan {
	t.Helper()
	plan, validation, err := Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("Load: err=%v validation=%+v", err, validation)
	}
	return plan
}

func mustWorkItemURN(t *testing.T, sagaID, id string) string {
	t.Helper()
	value, err := livingid.WorkItem(sagaID, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustRevisionURN(t *testing.T, sagaID string, kind livingid.Kind, parent, id string) string {
	t.Helper()
	value, err := livingid.DefinitionRevision(sagaID, kind, parent, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func hasConflict(plan Plan, kind string) bool {
	for _, conflict := range plan.Conflicts {
		if conflict.Kind == kind {
			return true
		}
	}
	return false
}

func issueContains(validation Validation, text string) bool {
	for _, issue := range validation.Issues {
		if strings.Contains(issue.Message, text) {
			return true
		}
	}
	return false
}
