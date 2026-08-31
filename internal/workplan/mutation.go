package workplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/livingid"
	"github.com/twentyideas/changesaga/internal/store"
)

// Mutator provides deterministic clocks for tests/adapters while retaining
// the package-level helpers for ordinary callers.
type Mutator struct {
	Now         func() time.Time
	LockTimeout time.Duration
}

func defaultMutator() Mutator { return Mutator{Now: time.Now, LockTimeout: store.DefaultLockTimeout} }

func CreateWave(root, waveID string, revision WaveRevision, requestID string) (MutationResult, error) {
	return defaultMutator().CreateWave(root, waveID, revision, requestID)
}

func ReviseWave(root string, revision WaveRevision, requestID string) (MutationResult, error) {
	return defaultMutator().ReviseWave(root, revision, requestID)
}

func CreateWorkItem(root, itemID string, revision WorkItemRevision, requestID string) (MutationResult, error) {
	return defaultMutator().CreateWorkItem(root, itemID, revision, requestID)
}

func ReviseWorkItem(root string, revision WorkItemRevision, requestID string) (MutationResult, error) {
	return defaultMutator().ReviseWorkItem(root, revision, requestID)
}

func CreateContract(root, contractID string, revision ContractRevision, requestID string) (MutationResult, error) {
	return defaultMutator().CreateContract(root, contractID, revision, requestID)
}

func ReviseContract(root string, revision ContractRevision, requestID string) (MutationResult, error) {
	return defaultMutator().ReviseContract(root, revision, requestID)
}

func CreateDependency(root string, dependency Dependency, requestID string) (MutationResult, error) {
	return defaultMutator().CreateDependency(root, dependency, requestID)
}

func RecordProgress(root, itemID string, event ProgressEvent, requestID string) (MutationResult, error) {
	return defaultMutator().RecordProgress(root, itemID, event, requestID)
}

func RecordWorkspace(root, itemID string, event WorkspaceEvent, requestID string) (MutationResult, error) {
	return defaultMutator().RecordWorkspace(root, itemID, event, requestID)
}

func RecordMerge(root, itemID string, event MergeEvent, requestID string) (MutationResult, error) {
	return defaultMutator().RecordMerge(root, itemID, event, requestID)
}

func RecordContractState(root, contractID string, event ContractEvent, requestID string) (MutationResult, error) {
	return defaultMutator().RecordContractState(root, contractID, event, requestID)
}

func (m Mutator) CreateWave(root, waveID string, revision WaveRevision, requestID string) (MutationResult, error) {
	digest, err := requestDigest("wave-create", struct {
		ID       string
		Revision WaveRevision
	}{waveID, revision})
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "wave-create", digest, func(plan Plan) (MutationResult, error) {
		if !validID(waveID) || !validID(revision.ID) {
			return MutationResult{}, fmt.Errorf("wave and revision ids must be stable identifiers")
		}
		if _, exists := plan.Waves[waveID]; exists {
			return MutationResult{}, fmt.Errorf("wave %q already exists", waveID)
		}
		if len(plan.Waves) >= MaxWaves {
			return MutationResult{}, fmt.Errorf("wave limit of %d reached", MaxWaves)
		}
		now := m.now()
		urn := waveURN(plan.SagaID, waveID)
		revision.Schema, revision.Version, revision.Wave = RevisionSchema, Version, urn
		revision.Parents = emptyStrings(revision.Parents)
		revision.EntryConditions = emptyStrings(revision.EntryConditions)
		revision.ExitConditions = emptyStrings(revision.ExitConditions)
		if len(revision.Parents) != 0 {
			return MutationResult{}, fmt.Errorf("initial wave revision cannot have parents")
		}
		stampRevision(&revision.CreatedAt, &revision.RequestID, &revision.RequestDigest, now, requestID, digest)
		identity := Identity{Schema: WaveSchema, Version: Version, ID: waveID, CreatedAt: now}
		probe := plan
		candidate := &Wave{Identity: identity, Revisions: []WaveRevision{revision}}
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateWave(&probe, &validation, candidate)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid wave: %s", joinIssues(validation))
		}
		final := filepath.Join(plan.Root, RootDir, "waves", waveID+".wave")
		if err := ensureCollection(plan.Root, filepath.Dir(final)); err != nil {
			return MutationResult{}, err
		}
		if err := store.CommitDir(plan.Root, final, func(stage string) error {
			if err := store.WriteJSON(filepath.Join(stage, "wave.json"), identity, true); err != nil {
				return err
			}
			return writeRevisionStage(stage, revision.ID, revision)
		}); err != nil {
			return MutationResult{}, err
		}
		return MutationResult{URN: urn, Path: relative(plan.Root, final), Created: []string{urn}}, nil
	})
}

func (m Mutator) ReviseWave(root string, revision WaveRevision, requestID string) (MutationResult, error) {
	digest, err := requestDigest("wave-revision", revision)
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "wave-revision", digest, func(plan Plan) (MutationResult, error) {
		ref, err := livingid.Parse(revision.Wave)
		if err != nil || ref.Kind != livingid.KindWave || ref.SagaID != plan.SagaID {
			return MutationResult{}, fmt.Errorf("wave must be a wave URN in this Saga")
		}
		wave := plan.Waves[ref.ID]
		if wave == nil {
			return MutationResult{}, fmt.Errorf("wave does not exist")
		}
		if !sameSet(revision.Parents, wave.Heads) {
			return MutationResult{}, headConflict("wave revision", wave.Heads)
		}
		if !validID(revision.ID) {
			return MutationResult{}, fmt.Errorf("revision id is invalid")
		}
		if len(wave.Revisions) >= MaxRevisionsPerResource {
			return MutationResult{}, fmt.Errorf("revision limit of %d reached", MaxRevisionsPerResource)
		}
		now := m.now()
		revision.Schema, revision.Version = RevisionSchema, Version
		revision.Parents = emptyStrings(revision.Parents)
		revision.EntryConditions = emptyStrings(revision.EntryConditions)
		revision.ExitConditions = emptyStrings(revision.ExitConditions)
		stampRevision(&revision.CreatedAt, &revision.RequestID, &revision.RequestDigest, now, requestID, digest)
		candidate := *wave
		candidate.Revisions = append(append([]WaveRevision{}, wave.Revisions...), revision)
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateWave(&plan, &validation, &candidate)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid wave revision: %s", joinIssues(validation))
		}
		final := filepath.Join(plan.Root, RootDir, "waves", ref.ID+".wave", "revisions", revision.ID+".revision")
		if err := store.CommitDir(plan.Root, final, func(stage string) error {
			return store.WriteJSON(filepath.Join(stage, "revision.json"), revision, true)
		}); err != nil {
			return MutationResult{}, err
		}
		created := revisionURN(plan.SagaID, livingid.KindWave, ref.ID, revision.ID)
		return MutationResult{URN: created, Path: relative(plan.Root, final), Created: []string{created}}, nil
	})
}

func (m Mutator) CreateWorkItem(root, itemID string, revision WorkItemRevision, requestID string) (MutationResult, error) {
	digest, err := requestDigest("work-item-create", struct {
		ID       string
		Revision WorkItemRevision
	}{itemID, revision})
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "work-item-create", digest, func(plan Plan) (MutationResult, error) {
		if !validID(itemID) || !validID(revision.ID) {
			return MutationResult{}, fmt.Errorf("work item and revision ids must be stable identifiers")
		}
		if plan.WorkItems[itemID] != nil {
			return MutationResult{}, fmt.Errorf("work item %q already exists", itemID)
		}
		if len(plan.WorkItems) >= MaxWorkItems {
			return MutationResult{}, fmt.Errorf("work-item limit of %d reached", MaxWorkItems)
		}
		now := m.now()
		urn := workItemURN(plan.SagaID, itemID)
		revision.Schema, revision.Version, revision.WorkItem = RevisionSchema, Version, urn
		revision.Parents = emptyStrings(revision.Parents)
		if len(revision.Parents) != 0 {
			return MutationResult{}, fmt.Errorf("initial work-item revision cannot have parents")
		}
		normalizeWorkItemRevision(&revision)
		stampRevision(&revision.CreatedAt, &revision.RequestID, &revision.RequestDigest, now, requestID, digest)
		eventID := store.EventID(now)
		progress := ProgressEvent{EventBase: EventBase{Schema: EventSchema, Version: Version, ID: eventID, Parents: []string{}, CreatedAt: now, RequestID: requestID, RequestDigest: digest}, WorkItem: urn, State: "planned"}
		identity := Identity{Schema: WorkItemSchema, Version: Version, ID: itemID, CreatedAt: now}
		probe := plan
		probe.WorkItems = copyWorkItems(plan.WorkItems)
		candidate := &WorkItem{Identity: identity, Revisions: []WorkItemRevision{revision}, Progress: []ProgressEvent{progress}, Workspaces: []WorkspaceEvent{}, Merges: []MergeEvent{}}
		probe.WorkItems[itemID] = candidate
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateWorkItem(&probe, &validation, candidate)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid work item: %s", joinIssues(validation))
		}
		final := filepath.Join(plan.Root, RootDir, "work-items", itemID+".work-item")
		if err := ensureCollection(plan.Root, filepath.Dir(final)); err != nil {
			return MutationResult{}, err
		}
		if err := store.CommitDir(plan.Root, final, func(stage string) error {
			if err := store.WriteJSON(filepath.Join(stage, "work-item.json"), identity, true); err != nil {
				return err
			}
			if err := writeRevisionStage(stage, revision.ID, revision); err != nil {
				return err
			}
			dir := filepath.Join(stage, "events", "progress")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(dir, eventID+".json"), progress, true)
		}); err != nil {
			return MutationResult{}, err
		}
		return MutationResult{URN: urn, Path: relative(plan.Root, final), Created: []string{urn}, EventIDs: []string{eventID}}, nil
	})
}

func (m Mutator) ReviseWorkItem(root string, revision WorkItemRevision, requestID string) (MutationResult, error) {
	digest, err := requestDigest("work-item-revision", revision)
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "work-item-revision", digest, func(plan Plan) (MutationResult, error) {
		ref, err := livingid.Parse(revision.WorkItem)
		if err != nil || ref.Kind != livingid.KindWorkItem || ref.SagaID != plan.SagaID {
			return MutationResult{}, fmt.Errorf("work_item must be a work-item URN in this Saga")
		}
		item := plan.WorkItems[ref.ID]
		if item == nil {
			return MutationResult{}, fmt.Errorf("work item does not exist")
		}
		if !sameSet(revision.Parents, item.Heads) {
			return MutationResult{}, headConflict("work-item revision", item.Heads)
		}
		if !validID(revision.ID) {
			return MutationResult{}, fmt.Errorf("revision id is invalid")
		}
		if len(item.Revisions) >= MaxRevisionsPerResource {
			return MutationResult{}, fmt.Errorf("revision limit of %d reached", MaxRevisionsPerResource)
		}
		now := m.now()
		revision.Schema, revision.Version = RevisionSchema, Version
		revision.Parents = emptyStrings(revision.Parents)
		normalizeWorkItemRevision(&revision)
		stampRevision(&revision.CreatedAt, &revision.RequestID, &revision.RequestDigest, now, requestID, digest)
		candidate := *item
		candidate.Revisions = append(append([]WorkItemRevision{}, item.Revisions...), revision)
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateWorkItem(&plan, &validation, &candidate)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid work-item revision: %s", joinIssues(validation))
		}
		final := filepath.Join(plan.Root, RootDir, "work-items", ref.ID+".work-item", "revisions", revision.ID+".revision")
		if err := store.CommitDir(plan.Root, final, func(stage string) error {
			return store.WriteJSON(filepath.Join(stage, "revision.json"), revision, true)
		}); err != nil {
			return MutationResult{}, err
		}
		created := revisionURN(plan.SagaID, livingid.KindWorkItem, ref.ID, revision.ID)
		return MutationResult{URN: created, Path: relative(plan.Root, final), Created: []string{created}}, nil
	})
}

func (m Mutator) CreateContract(root, contractID string, revision ContractRevision, requestID string) (MutationResult, error) {
	digest, err := requestDigest("contract-create", struct {
		ID       string
		Revision ContractRevision
	}{contractID, revision})
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "contract-create", digest, func(plan Plan) (MutationResult, error) {
		if !validID(contractID) || !validID(revision.ID) {
			return MutationResult{}, fmt.Errorf("contract and revision ids must be stable identifiers")
		}
		if plan.Contracts[contractID] != nil {
			return MutationResult{}, fmt.Errorf("contract %q already exists", contractID)
		}
		if len(plan.Contracts) >= MaxContracts {
			return MutationResult{}, fmt.Errorf("contract limit of %d reached", MaxContracts)
		}
		now := m.now()
		urn := contractURN(plan.SagaID, contractID)
		revision.Schema, revision.Version, revision.Contract = RevisionSchema, Version, urn
		revision.Parents = emptyStrings(revision.Parents)
		revision.Acceptance = emptyStrings(revision.Acceptance)
		if len(revision.Parents) != 0 {
			return MutationResult{}, fmt.Errorf("initial contract revision cannot have parents")
		}
		stampRevision(&revision.CreatedAt, &revision.RequestID, &revision.RequestDigest, now, requestID, digest)
		eventID := store.EventID(now)
		event := ContractEvent{EventBase: EventBase{Schema: EventSchema, Version: Version, ID: eventID, Parents: []string{}, CreatedAt: now, RequestID: requestID, RequestDigest: digest}, Contract: urn, State: "proposed", Evidence: []string{}}
		identity := Identity{Schema: ContractSchema, Version: Version, ID: contractID, CreatedAt: now}
		probe := plan
		probe.Contracts = copyContracts(plan.Contracts)
		candidate := &Contract{Identity: identity, Revisions: []ContractRevision{revision}, Events: []ContractEvent{event}}
		probe.Contracts[contractID] = candidate
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateContract(&probe, &validation, candidate)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid contract: %s", joinIssues(validation))
		}
		final := filepath.Join(plan.Root, RootDir, "contracts", contractID+".contract")
		if err := ensureCollection(plan.Root, filepath.Dir(final)); err != nil {
			return MutationResult{}, err
		}
		if err := store.CommitDir(plan.Root, final, func(stage string) error {
			if err := store.WriteJSON(filepath.Join(stage, "contract.json"), identity, true); err != nil {
				return err
			}
			if err := writeRevisionStage(stage, revision.ID, revision); err != nil {
				return err
			}
			dir := filepath.Join(stage, "events")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(dir, eventID+".json"), event, true)
		}); err != nil {
			return MutationResult{}, err
		}
		return MutationResult{URN: urn, Path: relative(plan.Root, final), Created: []string{urn}, EventIDs: []string{eventID}}, nil
	})
}

func (m Mutator) ReviseContract(root string, revision ContractRevision, requestID string) (MutationResult, error) {
	digest, err := requestDigest("contract-revision", revision)
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "contract-revision", digest, func(plan Plan) (MutationResult, error) {
		ref, err := livingid.Parse(revision.Contract)
		if err != nil || ref.Kind != livingid.KindContract || ref.SagaID != plan.SagaID {
			return MutationResult{}, fmt.Errorf("contract must be a contract URN in this Saga")
		}
		contract := plan.Contracts[ref.ID]
		if contract == nil {
			return MutationResult{}, fmt.Errorf("contract does not exist")
		}
		if !sameSet(revision.Parents, contract.Heads) {
			return MutationResult{}, headConflict("contract revision", contract.Heads)
		}
		if !validID(revision.ID) {
			return MutationResult{}, fmt.Errorf("revision id is invalid")
		}
		if len(contract.Revisions) >= MaxRevisionsPerResource {
			return MutationResult{}, fmt.Errorf("revision limit of %d reached", MaxRevisionsPerResource)
		}
		now := m.now()
		revision.Schema, revision.Version = RevisionSchema, Version
		revision.Parents = emptyStrings(revision.Parents)
		revision.Acceptance = emptyStrings(revision.Acceptance)
		stampRevision(&revision.CreatedAt, &revision.RequestID, &revision.RequestDigest, now, requestID, digest)
		candidate := *contract
		candidate.Revisions = append(append([]ContractRevision{}, contract.Revisions...), revision)
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateContract(&plan, &validation, &candidate)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid contract revision: %s", joinIssues(validation))
		}
		final := filepath.Join(plan.Root, RootDir, "contracts", ref.ID+".contract", "revisions", revision.ID+".revision")
		if err := store.CommitDir(plan.Root, final, func(stage string) error {
			return store.WriteJSON(filepath.Join(stage, "revision.json"), revision, true)
		}); err != nil {
			return MutationResult{}, err
		}
		created := revisionURN(plan.SagaID, livingid.KindContract, ref.ID, revision.ID)
		return MutationResult{URN: created, Path: relative(plan.Root, final), Created: []string{created}}, nil
	})
}

func (m Mutator) CreateDependency(root string, dependency Dependency, requestID string) (MutationResult, error) {
	digest, err := requestDigest("dependency-create", dependency)
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "dependency-create", digest, func(plan Plan) (MutationResult, error) {
		if !validID(dependency.ID) {
			return MutationResult{}, fmt.Errorf("dependency id is invalid")
		}
		if plan.Dependencies[dependency.ID] != nil {
			return MutationResult{}, fmt.Errorf("dependency %q already exists", dependency.ID)
		}
		if len(plan.Dependencies) >= MaxDependencies {
			return MutationResult{}, fmt.Errorf("dependency limit of %d reached", MaxDependencies)
		}
		dependency.Schema, dependency.Version = DependencySchema, Version
		dependency.CreatedAt = m.now()
		dependency.RequestID, dependency.RequestDigest = requestID, digest
		probe := plan
		probe.Dependencies = copyDependencies(plan.Dependencies)
		probe.Dependencies[dependency.ID] = &dependency
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateDependency(&probe, &validation, &dependency)
		validateDependencyDAG(&probe, &validation)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid dependency: %s", joinIssues(validation))
		}
		urn := dependencyURN(plan.SagaID, dependency.ID)
		final := filepath.Join(plan.Root, RootDir, "dependencies", dependency.ID+".dependency")
		if err := ensureCollection(plan.Root, filepath.Dir(final)); err != nil {
			return MutationResult{}, err
		}
		if err := store.CommitDir(plan.Root, final, func(stage string) error {
			return store.WriteJSON(filepath.Join(stage, "dependency.json"), dependency, true)
		}); err != nil {
			return MutationResult{}, err
		}
		return MutationResult{URN: urn, Path: relative(plan.Root, final), Created: []string{urn}}, nil
	})
}

func (m Mutator) RecordProgress(root, itemID string, event ProgressEvent, requestID string) (MutationResult, error) {
	digest, err := requestDigest("progress", struct {
		Item  string
		Event ProgressEvent
	}{itemID, event})
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "progress", digest, func(plan Plan) (MutationResult, error) {
		item := plan.WorkItems[itemID]
		if item == nil {
			return MutationResult{}, fmt.Errorf("work item does not exist")
		}
		if len(item.Progress) >= MaxEventsPerStream {
			return MutationResult{}, fmt.Errorf("progress event limit of %d reached", MaxEventsPerStream)
		}
		heads, states := progressHeads(item)
		if !sameSet(event.Parents, heads) {
			return MutationResult{}, headConflict("progress", heads)
		}
		if len(heads) == 1 && !progressTransition(states[heads[0]], event.State) {
			return MutationResult{}, fmt.Errorf("invalid progress transition %s -> %s", states[heads[0]], event.State)
		}
		if len(heads) == 0 {
			return MutationResult{}, fmt.Errorf("work item has no initial progress event")
		}
		event.WorkItem = workItemURN(plan.SagaID, itemID)
		stampEvent(&event.EventBase, m.now(), requestID, digest)
		if (event.State == "blocked" || event.State == "cancelled") && strings.TrimSpace(event.Reason) == "" {
			return MutationResult{}, fmt.Errorf("%s requires a reason", event.State)
		}
		candidate := *item
		candidate.Progress = append(append([]ProgressEvent{}, item.Progress...), event)
		if err := validateCandidateItem(&plan, &candidate); err != nil {
			return MutationResult{}, err
		}
		return writeItemEvent(plan, itemID, "progress", eventURNForWorkItem(event.WorkItem, "progress-event", event.ID), event.ID, event)
	})
}

func (m Mutator) RecordWorkspace(root, itemID string, event WorkspaceEvent, requestID string) (MutationResult, error) {
	digest, err := requestDigest("workspace", struct {
		Item  string
		Event WorkspaceEvent
	}{itemID, event})
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "workspace", digest, func(plan Plan) (MutationResult, error) {
		item := plan.WorkItems[itemID]
		if item == nil {
			return MutationResult{}, fmt.Errorf("work item does not exist")
		}
		if len(item.Workspaces) >= MaxEventsPerStream {
			return MutationResult{}, fmt.Errorf("workspace event limit of %d reached", MaxEventsPerStream)
		}
		heads, states := workspaceHeads(item, event.Workspace.ID, event.Role)
		if !sameSet(event.Parents, heads) {
			return MutationResult{}, headConflict("workspace assignment", heads)
		}
		if len(heads) == 0 && event.Action != "assigned" {
			return MutationResult{}, fmt.Errorf("initial workspace event must assign")
		}
		if len(heads) == 1 && states[heads[0]] == event.Action {
			return MutationResult{}, fmt.Errorf("workspace is already %s", event.Action)
		}
		event.WorkItem = workItemURN(plan.SagaID, itemID)
		stampEvent(&event.EventBase, m.now(), requestID, digest)
		candidate := *item
		candidate.Workspaces = append(append([]WorkspaceEvent{}, item.Workspaces...), event)
		if err := validateCandidateItem(&plan, &candidate); err != nil {
			return MutationResult{}, err
		}
		return writeItemEvent(plan, itemID, "workspaces", eventURNForWorkItem(event.WorkItem, "workspace-event", event.ID), event.ID, event)
	})
}

func (m Mutator) RecordMerge(root, itemID string, event MergeEvent, requestID string) (MutationResult, error) {
	digest, err := requestDigest("merge", struct {
		Item  string
		Event MergeEvent
	}{itemID, event})
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "merge", digest, func(plan Plan) (MutationResult, error) {
		item := plan.WorkItems[itemID]
		if item == nil {
			return MutationResult{}, fmt.Errorf("work item does not exist")
		}
		if len(item.Merges) >= MaxEventsPerStream {
			return MutationResult{}, fmt.Errorf("merge event limit of %d reached", MaxEventsPerStream)
		}
		if _, ok := currentMergeUnits(item)[event.Unit]; !ok {
			return MutationResult{}, fmt.Errorf("merge unit does not exist in the current work-item revision")
		}
		heads, states := mergeHeads(item, event.Unit)
		if !sameSet(event.Parents, heads) {
			return MutationResult{}, headConflict("merge", heads)
		}
		if len(heads) == 0 && event.State != "planned" {
			return MutationResult{}, fmt.Errorf("initial merge event must be planned")
		}
		if len(heads) == 1 && !mergeTransition(states[heads[0]], event.State) {
			return MutationResult{}, fmt.Errorf("invalid merge transition %s -> %s", states[heads[0]], event.State)
		}
		event.WorkItem = workItemURN(plan.SagaID, itemID)
		stampEvent(&event.EventBase, m.now(), requestID, digest)
		candidate := *item
		candidate.Merges = append(append([]MergeEvent{}, item.Merges...), event)
		if err := validateCandidateItem(&plan, &candidate); err != nil {
			return MutationResult{}, err
		}
		return writeItemEvent(plan, itemID, "merges", eventURNForWorkItem(event.WorkItem, "merge-event", event.ID), event.ID, event)
	})
}

func (m Mutator) RecordContractState(root, contractID string, event ContractEvent, requestID string) (MutationResult, error) {
	digest, err := requestDigest("contract-state", struct {
		Contract string
		Event    ContractEvent
	}{contractID, event})
	if err != nil {
		return MutationResult{}, err
	}
	return m.mutate(root, requestID, "contract-state", digest, func(plan Plan) (MutationResult, error) {
		contract := plan.Contracts[contractID]
		if contract == nil {
			return MutationResult{}, fmt.Errorf("contract does not exist")
		}
		if len(contract.Events) >= MaxEventsPerStream {
			return MutationResult{}, fmt.Errorf("contract event limit of %d reached", MaxEventsPerStream)
		}
		heads, states := contractHeads(contract)
		if !sameSet(event.Parents, heads) {
			return MutationResult{}, headConflict("contract state", heads)
		}
		if len(heads) == 1 && !contractTransition(states[heads[0]], event.State) {
			return MutationResult{}, fmt.Errorf("invalid contract transition %s -> %s", states[heads[0]], event.State)
		}
		event.Contract = contractURN(plan.SagaID, contractID)
		event.Evidence = emptyStrings(event.Evidence)
		stampEvent(&event.EventBase, m.now(), requestID, digest)
		candidate := *contract
		candidate.Events = append(append([]ContractEvent{}, contract.Events...), event)
		validation := Validation{Valid: true, Issues: []Issue{}}
		validateContract(&plan, &validation, &candidate)
		if hasErrors(validation) {
			return MutationResult{}, fmt.Errorf("invalid contract event: %s", joinIssues(validation))
		}
		dir := filepath.Join(plan.Root, RootDir, "contracts", contractID+".contract", "events")
		if _, err := store.EnsureDirWithin(plan.Root, dir); err != nil {
			return MutationResult{}, err
		}
		if err := store.WriteJSON(filepath.Join(dir, event.ID+".json"), event, true); err != nil {
			return MutationResult{}, err
		}
		path := filepath.Join(dir, event.ID+".json")
		return MutationResult{URN: eventURNForContract(event.Contract, event.ID), Path: relative(plan.Root, path), EventIDs: []string{event.ID}}, nil
	})
}

func (m Mutator) mutate(root, requestID, operation, digest string, apply func(Plan) (MutationResult, error)) (MutationResult, error) {
	if !validRequestID(requestID) {
		return MutationResult{}, fmt.Errorf("request_id must be a stable 1-256 character identifier")
	}
	if _, validation, err := Load(root); err != nil {
		return MutationResult{}, err
	} else if !validation.Valid {
		return MutationResult{}, fmt.Errorf("cannot mutate invalid work plan: %s", joinIssues(validation))
	}
	timeout := m.LockTimeout
	if timeout <= 0 {
		timeout = store.DefaultLockTimeout
	}
	var result MutationResult
	err := store.WithSagaLock(root, timeout, func() error {
		plan, validation, err := Load(root)
		if err != nil {
			return err
		}
		if !validation.Valid {
			return fmt.Errorf("cannot mutate invalid work plan: %s", joinIssues(validation))
		}
		if prior, exists := plan.Requests[requestID]; exists {
			if prior.Digest != digest {
				return fmt.Errorf("request_id %q was already used with a different payload", requestID)
			}
			result = MutationResult{URN: prior.Resource, Path: prior.Path, Replayed: true}
			if prior.Resource != "" && prior.EventID == "" {
				result.Created = []string{prior.Resource}
			}
			if prior.EventID != "" {
				result.EventIDs = []string{prior.EventID}
			}
			return nil
		}
		result, err = apply(plan)
		return err
	})
	return result, err
}

func (m Mutator) now() time.Time {
	if m.Now == nil {
		return time.Now().UTC()
	}
	return m.Now().UTC()
}
func requestDigest(operation string, payload any) (string, error) {
	data, err := json.Marshal(struct {
		Operation string `json:"operation"`
		Payload   any    `json:"payload"`
	}{operation, payload})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func stampRevision(createdAt *time.Time, requestID, requestDigest *string, now time.Time, id, digest string) {
	if createdAt.IsZero() {
		*createdAt = now
	} else {
		*createdAt = createdAt.UTC()
	}
	*requestID = id
	*requestDigest = digest
}
func stampEvent(event *EventBase, now time.Time, requestID, digest string) {
	event.Schema, event.Version = EventSchema, Version
	if event.ID == "" {
		event.ID = store.EventID(now)
	}
	event.Parents = emptyStrings(event.Parents)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	} else {
		event.CreatedAt = event.CreatedAt.UTC()
	}
	event.RequestID, event.RequestDigest = requestID, digest
}
func normalizeWorkItemRevision(revision *WorkItemRevision) {
	revision.Deliverables = emptyStrings(revision.Deliverables)
	revision.Relations = emptyStrings(revision.Relations)
	revision.Dependencies = emptyStrings(revision.Dependencies)
	revision.Contracts = emptyStrings(revision.Contracts)
	revision.ExpectedTouchAreas = emptyTouchAreas(revision.ExpectedTouchAreas)
	revision.CompletionChecks = emptyStrings(revision.CompletionChecks)
	revision.MergeUnits = emptyMergeUnits(revision.MergeUnits)
}
func emptyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
func emptyTouchAreas(values []TouchArea) []TouchArea {
	if values == nil {
		return []TouchArea{}
	}
	for i := range values {
		values[i].Intents = emptyStrings(values[i].Intents)
	}
	return values
}
func emptyMergeUnits(values []MergeUnit) []MergeUnit {
	if values == nil {
		return []MergeUnit{}
	}
	return values
}
func ensureCollection(root, dir string) error { _, err := store.EnsureDirWithin(root, dir); return err }
func writeRevisionStage(stage, id string, value any) error {
	dir := filepath.Join(stage, "revisions", id+".revision")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return store.WriteJSON(filepath.Join(dir, "revision.json"), value, true)
}
func writeItemEvent(plan Plan, itemID, stream, urn, eventID string, value any) (MutationResult, error) {
	dir := filepath.Join(plan.Root, RootDir, "work-items", itemID+".work-item", "events", stream)
	if _, err := store.EnsureDirWithin(plan.Root, dir); err != nil {
		return MutationResult{}, err
	}
	path := filepath.Join(dir, eventID+".json")
	if err := store.WriteJSON(path, value, true); err != nil {
		return MutationResult{}, err
	}
	return MutationResult{URN: urn, Path: relative(plan.Root, path), EventIDs: []string{eventID}}, nil
}

func sameSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a := append([]string{}, left...)
	b := append([]string{}, right...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func headConflict(kind string, heads []string) error {
	return fmt.Errorf("%s parent-head conflict: expected parents %v", kind, heads)
}
func progressTransition(from, to string) bool {
	return map[string]map[string]bool{"planned": {"ready": true, "in_progress": true, "blocked": true, "cancelled": true}, "ready": {"in_progress": true, "blocked": true, "cancelled": true}, "in_progress": {"blocked": true, "done": true, "cancelled": true}, "blocked": {"ready": true, "in_progress": true, "cancelled": true}, "done": {"in_progress": true}}[from][to]
}
func mergeTransition(from, to string) bool {
	return map[string]map[string]bool{"planned": {"ready": true, "abandoned": true}, "ready": {"integrated": true, "abandoned": true}, "integrated": {"reverted": true}, "reverted": {"ready": true, "abandoned": true}}[from][to]
}
func contractTransition(from, to string) bool {
	return map[string]map[string]bool{"proposed": {"accepted": true, "waived": true}, "accepted": {"fulfilled": true, "violated": true, "waived": true}, "fulfilled": {"violated": true}, "violated": {"accepted": true, "waived": true}}[from][to]
}

func progressHeads(item *WorkItem) ([]string, map[string]string) {
	nodes := make([]eventNode, 0, len(item.Progress))
	states := map[string]string{}
	for _, event := range item.Progress {
		id := eventURNForWorkItem(event.WorkItem, "progress-event", event.ID)
		nodes = append(nodes, eventNode{ID: id, Parents: event.Parents})
		states[id] = event.State
	}
	return eventHeads(nodes), states
}
func workspaceHeads(item *WorkItem, workspaceID, role string) ([]string, map[string]string) {
	nodes := []eventNode{}
	states := map[string]string{}
	for _, event := range item.Workspaces {
		if event.Workspace.ID == workspaceID && event.Role == role {
			id := eventURNForWorkItem(event.WorkItem, "workspace-event", event.ID)
			nodes = append(nodes, eventNode{ID: id, Parents: event.Parents})
			states[id] = event.Action
		}
	}
	return eventHeads(nodes), states
}
func mergeHeads(item *WorkItem, unit string) ([]string, map[string]string) {
	nodes := []eventNode{}
	states := map[string]string{}
	for _, event := range item.Merges {
		if event.Unit == unit {
			id := eventURNForWorkItem(event.WorkItem, "merge-event", event.ID)
			nodes = append(nodes, eventNode{ID: id, Parents: event.Parents})
			states[id] = event.State
		}
	}
	return eventHeads(nodes), states
}
func contractHeads(contract *Contract) ([]string, map[string]string) {
	nodes := []eventNode{}
	states := map[string]string{}
	for _, event := range contract.Events {
		id := eventURNForContract(event.Contract, event.ID)
		nodes = append(nodes, eventNode{ID: id, Parents: event.Parents})
		states[id] = event.State
	}
	return eventHeads(nodes), states
}
func eventHeads(nodes []eventNode) []string {
	parents := map[string]bool{}
	ids := map[string]bool{}
	for _, node := range nodes {
		ids[node.ID] = true
		for _, parent := range node.Parents {
			parents[parent] = true
		}
	}
	heads := []string{}
	for id := range ids {
		if !parents[id] {
			heads = append(heads, id)
		}
	}
	sort.Strings(heads)
	return heads
}
func copyDependencies(source map[string]*Dependency) map[string]*Dependency {
	result := make(map[string]*Dependency, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func copyWorkItems(source map[string]*WorkItem) map[string]*WorkItem {
	result := make(map[string]*WorkItem, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func copyContracts(source map[string]*Contract) map[string]*Contract {
	result := make(map[string]*Contract, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func validateCandidateItem(plan *Plan, candidate *WorkItem) error {
	validation := Validation{Valid: true, Issues: []Issue{}}
	validateWorkItem(plan, &validation, candidate)
	if hasErrors(validation) {
		return fmt.Errorf("invalid work-item event: %s", joinIssues(validation))
	}
	return nil
}
func joinIssues(validation Validation) string {
	messages := []string{}
	for _, issue := range validation.Issues {
		if issue.Severity == "error" {
			messages = append(messages, issue.Path+": "+issue.Message)
		}
	}
	if len(messages) > 3 {
		messages = append(messages[:3], fmt.Sprintf("and %d more", len(messages)-3))
	}
	return strings.Join(messages, "; ")
}
