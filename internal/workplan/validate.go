package workplan

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/livingid"
)

var (
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
	regexpDigest     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	fullOIDPattern   = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)
	uuidPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

func validID(value string) bool        { return livingid.ValidID(value) }
func validRequestID(value string) bool { return requestIDPattern.MatchString(value) }

func validatePlan(plan *Plan, validation *Validation) {
	for _, id := range sortedKeys(plan.Waves) {
		validateWave(plan, validation, plan.Waves[id])
	}
	for _, id := range sortedKeys(plan.WorkItems) {
		validateWorkItem(plan, validation, plan.WorkItems[id])
	}
	for _, id := range sortedKeys(plan.Contracts) {
		validateContract(plan, validation, plan.Contracts[id])
	}
	for _, id := range sortedKeys(plan.Dependencies) {
		validateDependency(plan, validation, plan.Dependencies[id])
	}
	validateDependencyDAG(plan, validation)
}

func validateIdentity(validation *Validation, path, expectedSchema string, identity Identity) {
	if identity.Schema != expectedSchema || identity.Version != Version || !validID(identity.ID) || !validTime(identity.CreatedAt) {
		addIssue(validation, "error", path, "identity requires the v3 schema, stable id, and UTC creation time")
	}
}

func validateWave(plan *Plan, validation *Validation, wave *Wave) {
	base := RootDir + "/waves/" + wave.ID + ".wave"
	validateIdentity(validation, base+"/wave.json", WaveSchema, wave.Identity)
	refs := make([]revisionNode, 0, len(wave.Revisions))
	for i := range wave.Revisions {
		revision := &wave.Revisions[i]
		path := base + "/revisions/" + revision.ID + ".revision/revision.json"
		if revision.Schema != RevisionSchema || revision.Version != Version || !validID(revision.ID) || !validTime(revision.CreatedAt) || strings.TrimSpace(revision.Title) == "" || strings.TrimSpace(revision.Objective) == "" {
			addIssue(validation, "error", path, "wave revision requires the v3 revision schema, stable id, title, objective, and UTC creation time")
		}
		if !referenceIs(revision.Wave, plan.SagaID, livingid.KindWave, wave.ID) {
			addIssue(validation, "error", path, "wave revision target does not match its package")
		}
		if duplicateStrings(revision.Parents) {
			addIssue(validation, "error", path, "revision parents must be unique")
		}
		refs = append(refs, revisionNode{ID: revisionURN(plan.SagaID, livingid.KindWave, wave.ID, revision.ID), Parents: revision.Parents})
	}
	wave.Heads = validateRevisionGraph(validation, base, refs)
	if len(wave.Heads) > 1 {
		addConflict(plan, "wave_revision_heads", waveURN(plan.SagaID, wave.ID), wave.Heads)
	} else if len(wave.Heads) == 1 {
		wave.CurrentRevision = findWaveRevision(wave, wave.Heads[0])
	}
}

func validateWorkItem(plan *Plan, validation *Validation, item *WorkItem) {
	base := RootDir + "/work-items/" + item.ID + ".work-item"
	validateIdentity(validation, base+"/work-item.json", WorkItemSchema, item.Identity)
	refs := make([]revisionNode, 0, len(item.Revisions))
	for i := range item.Revisions {
		revision := &item.Revisions[i]
		path := base + "/revisions/" + revision.ID + ".revision/revision.json"
		validateWorkItemRevision(plan, validation, item.ID, path, revision)
		refs = append(refs, revisionNode{ID: revisionURN(plan.SagaID, livingid.KindWorkItem, item.ID, revision.ID), Parents: revision.Parents})
	}
	item.Heads = validateRevisionGraph(validation, base, refs)
	if len(item.Heads) > 1 {
		addConflict(plan, "work_item_revision_heads", workItemURN(plan.SagaID, item.ID), item.Heads)
	} else if len(item.Heads) == 1 {
		item.CurrentRevision = findWorkItemRevision(item, item.Heads[0])
	}
	progress := make([]eventNode, 0, len(item.Progress))
	for i := range item.Progress {
		event := &item.Progress[i]
		path := base + "/events/progress/" + event.ID + ".json"
		validateEventBase(validation, path, event.EventBase)
		if !referenceIs(event.WorkItem, plan.SagaID, livingid.KindWorkItem, item.ID) {
			addIssue(validation, "error", path, "progress event work item does not match package")
		}
		if !oneOf(event.State, "planned", "ready", "in_progress", "blocked", "done", "cancelled") {
			addIssue(validation, "error", path, "invalid progress state")
		}
		if (event.State == "blocked" || event.State == "cancelled") && strings.TrimSpace(event.Reason) == "" {
			addIssue(validation, "error", path, "blocked and cancelled progress requires a reason")
		}
		progress = append(progress, eventNode{ID: eventURNForWorkItem(event.WorkItem, "progress-event", event.ID), Parents: event.Parents, State: event.State})
	}
	progressHeads := validateEventGraph(validation, base+"/events/progress", progress, true)
	if len(progressHeads) > 1 {
		addConflict(plan, "progress_heads", workItemURN(plan.SagaID, item.ID), progressHeads)
	} else if len(progressHeads) == 1 {
		item.CurrentProgress = findProgressEvent(item, progressHeads[0])
	}

	validateWorkspaceEvents(plan, validation, item, base)
	validateMergeEvents(plan, validation, item, base)
}

func validateWorkItemRevision(plan *Plan, validation *Validation, itemID, path string, revision *WorkItemRevision) {
	if revision.Schema != RevisionSchema || revision.Version != Version || !validID(revision.ID) || !validTime(revision.CreatedAt) || strings.TrimSpace(revision.Title) == "" || strings.TrimSpace(revision.Objective) == "" || len(revision.Deliverables) == 0 {
		addIssue(validation, "error", path, "work-item revision requires the v3 revision schema, stable id, title, objective, deliverables, and UTC creation time")
	}
	if !referenceIs(revision.WorkItem, plan.SagaID, livingid.KindWorkItem, itemID) {
		addIssue(validation, "error", path, "work-item revision target does not match its package")
	}
	if duplicateStrings(revision.Parents) {
		addIssue(validation, "error", path, "revision parents must be unique")
	}
	if revision.Wave != "" {
		ref, err := livingid.Parse(revision.Wave)
		if err != nil || ref.SagaID != plan.SagaID || ref.Kind != livingid.KindWave {
			addIssue(validation, "error", path, "wave must be a wave URN in this Saga")
		} else if _, ok := plan.Waves[ref.ID]; !ok {
			addIssue(validation, "error", path, "wave does not exist")
		}
	}
	validateStringList(validation, path, "deliverables", revision.Deliverables, true)
	validateStringList(validation, path, "completion checks", revision.CompletionChecks, false)
	for _, dependency := range revision.Dependencies {
		ref, err := livingid.Parse(dependency)
		if err != nil || ref.SagaID != plan.SagaID || ref.Kind != livingid.KindDependency {
			addIssue(validation, "error", path, "dependency references must be dependency URNs in this Saga")
		} else if _, ok := plan.Dependencies[ref.ID]; !ok {
			addIssue(validation, "error", path, "dependency reference does not exist")
		}
	}
	for _, contract := range revision.Contracts {
		ref, err := livingid.Parse(contract)
		if err != nil || ref.SagaID != plan.SagaID || (ref.Kind != livingid.KindContract && !(ref.Kind == livingid.KindRevision && ref.ParentKind == livingid.KindContract)) {
			addIssue(validation, "error", path, "contract references must be contract or contract-revision URNs in this Saga")
		} else if _, ok := plan.Contracts[ref.ID]; ref.Kind == livingid.KindContract && !ok {
			addIssue(validation, "error", path, "contract reference does not exist")
		} else if ref.Kind == livingid.KindRevision {
			resource := plan.Contracts[ref.ParentID]
			if resource == nil || !contractHasRevision(resource, ref.ID) {
				addIssue(validation, "error", path, "contract revision reference does not exist")
			}
		}
	}
	for index := range revision.ExpectedTouchAreas {
		validateTouchArea(validation, path, index, revision.ExpectedTouchAreas[index])
	}
	unitIDs := map[string]bool{}
	for index, unit := range revision.MergeUnits {
		if !validID(unit.ID) || unitIDs[unit.ID] || !validRepository(unit.Repository) || strings.TrimSpace(unit.SourceBranch) == "" || strings.TrimSpace(unit.TargetBranch) == "" {
			addIssue(validation, "error", path, fmt.Sprintf("merge unit %d requires a unique id, repository, source branch, and target branch", index))
		}
		unitIDs[unit.ID] = true
	}
}

func validateContract(plan *Plan, validation *Validation, contract *Contract) {
	base := RootDir + "/contracts/" + contract.ID + ".contract"
	validateIdentity(validation, base+"/contract.json", ContractSchema, contract.Identity)
	refs := make([]revisionNode, 0, len(contract.Revisions))
	for i := range contract.Revisions {
		revision := &contract.Revisions[i]
		path := base + "/revisions/" + revision.ID + ".revision/revision.json"
		if revision.Schema != RevisionSchema || revision.Version != Version || !validID(revision.ID) || !validTime(revision.CreatedAt) || !oneOf(revision.Kind, "deliverable", "interface", "handoff", "quality_gate") || strings.TrimSpace(revision.Statement) == "" || len(revision.Acceptance) == 0 {
			addIssue(validation, "error", path, "contract revision requires the v3 revision schema, valid kind, statement, acceptance checks, and UTC creation time")
		}
		if !referenceIs(revision.Contract, plan.SagaID, livingid.KindContract, contract.ID) {
			addIssue(validation, "error", path, "contract revision target does not match its package")
		}
		if !workItemReference(plan, revision.Provider) || !workItemReference(plan, revision.Consumer) {
			addIssue(validation, "error", path, "contract provider and consumer must be existing work items")
		}
		validateStringList(validation, path, "acceptance", revision.Acceptance, true)
		refs = append(refs, revisionNode{ID: revisionURN(plan.SagaID, livingid.KindContract, contract.ID, revision.ID), Parents: revision.Parents})
	}
	contract.Heads = validateRevisionGraph(validation, base, refs)
	if len(contract.Heads) > 1 {
		addConflict(plan, "contract_revision_heads", contractURN(plan.SagaID, contract.ID), contract.Heads)
	} else if len(contract.Heads) == 1 {
		contract.CurrentRevision = findContractRevision(contract, contract.Heads[0])
	}
	events := make([]eventNode, 0, len(contract.Events))
	for i := range contract.Events {
		event := &contract.Events[i]
		path := base + "/events/" + event.ID + ".json"
		validateEventBase(validation, path, event.EventBase)
		if !referenceIs(event.Contract, plan.SagaID, livingid.KindContract, contract.ID) {
			addIssue(validation, "error", path, "contract event target does not match package")
		}
		if !oneOf(event.State, "proposed", "accepted", "fulfilled", "violated", "waived") {
			addIssue(validation, "error", path, "invalid contract state")
		}
		if oneOf(event.State, "fulfilled", "violated", "waived") && strings.TrimSpace(event.Summary) == "" {
			addIssue(validation, "error", path, "fulfilled, violated, and waived contract events require a summary")
		}
		events = append(events, eventNode{ID: eventURNForContract(event.Contract, event.ID), Parents: event.Parents, State: event.State})
	}
	heads := validateEventGraph(validation, base+"/events", events, true)
	if len(heads) > 1 {
		addConflict(plan, "contract_event_heads", contractURN(plan.SagaID, contract.ID), heads)
	} else if len(heads) == 1 {
		contract.CurrentEvent = findContractEvent(contract, heads[0])
	}
}

func validateDependency(plan *Plan, validation *Validation, dependency *Dependency) {
	path := RootDir + "/dependencies/" + dependency.ID + ".dependency/dependency.json"
	if dependency.Schema != DependencySchema || dependency.Version != Version || !validID(dependency.ID) || !validTime(dependency.CreatedAt) || strings.TrimSpace(dependency.Reason) == "" {
		addIssue(validation, "error", path, "dependency requires the v3 schema, stable id, reason, and UTC creation time")
	}
	if !workItemReference(plan, dependency.Prerequisite) || !workItemReference(plan, dependency.Dependent) {
		addIssue(validation, "error", path, "dependency endpoints must be existing work items")
	}
	if dependency.Prerequisite == dependency.Dependent {
		addIssue(validation, "error", path, "dependency cannot be a self-edge")
	}
	switch dependency.Condition.Kind {
	case "progress_done", "merge_integrated":
		if dependency.Condition.ContractRevision != "" {
			addIssue(validation, "error", path, "only contract_fulfilled names a contract revision")
		}
	case "contract_fulfilled":
		ref, err := livingid.Parse(dependency.Condition.ContractRevision)
		if err != nil || ref.SagaID != plan.SagaID || ref.Kind != livingid.KindRevision || ref.ParentKind != livingid.KindContract {
			addIssue(validation, "error", path, "contract_fulfilled requires an exact contract revision URN")
		} else if contract, ok := plan.Contracts[ref.ParentID]; !ok {
			addIssue(validation, "error", path, "condition contract does not exist")
		} else {
			var found *ContractRevision
			for i := range contract.Revisions {
				if contract.Revisions[i].ID == ref.ID {
					found = &contract.Revisions[i]
					break
				}
			}
			if found == nil {
				addIssue(validation, "error", path, "condition contract revision does not exist")
			} else if found.Provider != dependency.Prerequisite || found.Consumer != dependency.Dependent {
				addIssue(validation, "error", path, "condition contract provider/consumer must match dependency endpoints")
			}
			if found != nil && !contains(contract.Heads, dependency.Condition.ContractRevision) {
				addIssue(validation, "warning", path, "condition pins a stale contract revision")
			}
		}
	default:
		addIssue(validation, "error", path, "dependency condition must be progress_done, merge_integrated, or contract_fulfilled")
	}
}

func validateDependencyDAG(plan *Plan, validation *Validation) {
	graph := map[string][]string{}
	edges := map[string]string{}
	for _, id := range sortedKeys(plan.Dependencies) {
		dependency := plan.Dependencies[id]
		key := dependency.Prerequisite + "\x00" + dependency.Dependent + "\x00" + dependency.Condition.Kind + "\x00" + dependency.Condition.ContractRevision
		if prior, ok := edges[key]; ok {
			addIssue(validation, "error", RootDir+"/dependencies/"+id+".dependency/dependency.json", "duplicates dependency "+prior)
		} else {
			edges[key] = id
		}
		graph[dependency.Prerequisite] = append(graph[dependency.Prerequisite], dependency.Dependent)
	}
	for node := range graph {
		sort.Strings(graph[node])
	}
	state := map[string]int{}
	stack := []string{}
	var visit func(string) bool
	visit = func(node string) bool {
		state[node] = 1
		stack = append(stack, node)
		for _, next := range graph[node] {
			if state[next] == 1 {
				start := 0
				for stack[start] != next {
					start++
				}
				cycle := append(append([]string{}, stack[start:]...), next)
				addIssue(validation, "error", RootDir+"/dependencies", "dependency cycle: "+strings.Join(cycle, " -> "))
				return true
			}
			if state[next] == 0 && visit(next) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
		return false
	}
	for _, node := range sortedKeys(graph) {
		if state[node] == 0 && visit(node) {
			return
		}
	}
}

func validateWorkspaceEvents(plan *Plan, validation *Validation, item *WorkItem, base string) {
	streams := map[string][]eventNode{}
	activeOwners := 0
	for i := range item.Workspaces {
		event := &item.Workspaces[i]
		path := base + "/events/workspaces/" + event.ID + ".json"
		validateEventBase(validation, path, event.EventBase)
		if !referenceIs(event.WorkItem, plan.SagaID, livingid.KindWorkItem, item.ID) {
			addIssue(validation, "error", path, "workspace event work item does not match package")
		}
		if !oneOf(event.Action, "assigned", "released") || !oneOf(event.Role, "owner", "contributor", "observer") {
			addIssue(validation, "error", path, "invalid workspace action or role")
		}
		if event.Workspace.Provider != "devswarm" || !uuidPattern.MatchString(event.Workspace.ID) || strings.TrimSpace(event.Workspace.RepositoryID) == "" || strings.TrimSpace(event.Workspace.Branch) == "" {
			addIssue(validation, "error", path, "DevSwarm workspace id, repository id, and branch are required")
		}
		key := event.Workspace.ID + "\x00" + event.Role
		streams[key] = append(streams[key], eventNode{ID: eventURNForWorkItem(event.WorkItem, "workspace-event", event.ID), Parents: event.Parents, State: event.Action})
	}
	for key, events := range streams {
		heads := validateEventGraph(validation, base+"/events/workspaces", events, false)
		if len(heads) > 1 {
			addConflict(plan, "workspace_assignment_heads", workItemURN(plan.SagaID, item.ID), heads)
		}
		if len(heads) == 1 {
			for _, event := range events {
				if event.ID == heads[0] && event.State == "assigned" && strings.HasSuffix(key, "\x00owner") {
					activeOwners++
				}
			}
		}
	}
	if activeOwners > 1 {
		addConflict(plan, "multiple_active_owners", workItemURN(plan.SagaID, item.ID), activeWorkspaceOwnerHeads(item))
	}
}

func validateMergeEvents(plan *Plan, validation *Validation, item *WorkItem, base string) {
	units := currentMergeUnits(item)
	streams := map[string][]eventNode{}
	for i := range item.Merges {
		event := &item.Merges[i]
		path := base + "/events/merges/" + event.ID + ".json"
		validateEventBase(validation, path, event.EventBase)
		if !referenceIs(event.WorkItem, plan.SagaID, livingid.KindWorkItem, item.ID) {
			addIssue(validation, "error", path, "merge event work item does not match package")
		}
		if _, ok := units[event.Unit]; !ok {
			addIssue(validation, "error", path, "merge event names an unknown current merge unit")
		}
		if !oneOf(event.State, "planned", "ready", "integrated", "reverted", "abandoned") {
			addIssue(validation, "error", path, "invalid merge state")
		}
		if (event.State == "ready" || event.State == "integrated") && !fullOIDPattern.MatchString(event.HeadOID) {
			addIssue(validation, "error", path, "ready and integrated merge events require a full head_oid")
		}
		if event.State == "integrated" && !fullOIDPattern.MatchString(event.MergeOID) {
			addIssue(validation, "error", path, "integrated merge events require a full merge_oid")
		}
		if event.State == "reverted" && (!fullOIDPattern.MatchString(event.MergeOID) || !fullOIDPattern.MatchString(event.RevertOID)) {
			addIssue(validation, "error", path, "reverted merge events require full merge_oid and revert_oid evidence")
		}
		streams[event.Unit] = append(streams[event.Unit], eventNode{ID: eventURNForWorkItem(event.WorkItem, "merge-event", event.ID), Parents: event.Parents, State: event.State})
	}
	for unit, events := range streams {
		heads := validateEventGraph(validation, base+"/events/merges", events, false)
		if len(heads) > 1 {
			addConflict(plan, "merge_event_heads", workItemURN(plan.SagaID, item.ID)+"#"+unit, heads)
		}
	}
}

type revisionNode struct {
	ID      string
	Parents []string
}
type eventNode struct {
	ID      string
	Parents []string
	State   string
}

func validateRevisionGraph(validation *Validation, path string, nodes []revisionNode) []string {
	if len(nodes) == 0 {
		addIssue(validation, "error", path, "resource must have at least one definition revision")
		return nil
	}
	ids := map[string]bool{}
	children := map[string]bool{}
	initials := 0
	for _, node := range nodes {
		if ids[node.ID] {
			addIssue(validation, "error", path, "duplicate revision id")
		}
		ids[node.ID] = true
		if len(node.Parents) == 0 {
			initials++
		}
	}
	for _, node := range nodes {
		for _, parent := range node.Parents {
			if !ids[parent] {
				addIssue(validation, "error", path, "revision parent does not exist: "+parent)
			}
			children[parent] = true
		}
	}
	if initials != 1 {
		addIssue(validation, "error", path, "revision graph must have exactly one initial revision")
	}
	if graphCycleRevisions(nodes) {
		addIssue(validation, "error", path, "revision graph must be acyclic")
	}
	heads := []string{}
	for id := range ids {
		if !children[id] {
			heads = append(heads, id)
		}
	}
	sort.Strings(heads)
	return heads
}

func validateEventGraph(validation *Validation, path string, nodes []eventNode, requireInitial bool) []string {
	if len(nodes) == 0 {
		if requireInitial {
			addIssue(validation, "error", path, "event graph requires an initial event")
		}
		return nil
	}
	ids := map[string]bool{}
	children := map[string]bool{}
	initials := 0
	for _, node := range nodes {
		if ids[node.ID] {
			addIssue(validation, "error", path, "duplicate event id")
		}
		ids[node.ID] = true
		if len(node.Parents) == 0 {
			initials++
		}
	}
	for _, node := range nodes {
		if duplicateStrings(node.Parents) {
			addIssue(validation, "error", path, "event parents must be unique")
		}
		for _, parent := range node.Parents {
			if !ids[parent] {
				addIssue(validation, "error", path, "event parent does not exist: "+parent)
			}
			children[parent] = true
		}
	}
	if requireInitial && initials != 1 {
		addIssue(validation, "error", path, "event graph must have exactly one initial event")
	}
	if !requireInitial && initials != 1 {
		addIssue(validation, "error", path, "each event stream must have exactly one initial event")
	}
	if graphCycleEvents(nodes) {
		addIssue(validation, "error", path, "event graph must be acyclic")
	}
	heads := []string{}
	for id := range ids {
		if !children[id] {
			heads = append(heads, id)
		}
	}
	sort.Strings(heads)
	return heads
}

func graphCycleRevisions(nodes []revisionNode) bool {
	graph := map[string][]string{}
	for _, n := range nodes {
		graph[n.ID] = n.Parents
	}
	return graphCycle(graph)
}
func graphCycleEvents(nodes []eventNode) bool {
	graph := map[string][]string{}
	for _, n := range nodes {
		graph[n.ID] = n.Parents
	}
	return graphCycle(graph)
}
func graphCycle(graph map[string][]string) bool {
	state := map[string]int{}
	var visit func(string) bool
	visit = func(n string) bool {
		if state[n] == 1 {
			return true
		}
		if state[n] == 2 {
			return false
		}
		state[n] = 1
		for _, p := range graph[n] {
			if visit(p) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	for n := range graph {
		if visit(n) {
			return true
		}
	}
	return false
}

func validateEventBase(validation *Validation, path string, event EventBase) {
	if event.Schema != EventSchema || event.Version != Version || !validID(event.ID) || !validTime(event.CreatedAt) {
		addIssue(validation, "error", path, "event requires the v3 event schema, stable id, and UTC creation time")
	}
}

func validateTouchArea(validation *Validation, recordPath string, index int, area TouchArea) {
	if !validRepository(area.Repository) || !oneOf(area.Selector.Kind, "file", "directory", "glob", "logical") || len(area.Intents) == 0 {
		addIssue(validation, "error", recordPath, fmt.Sprintf("expected touch area %d has an invalid repository, selector, or intents", index))
		return
	}
	if duplicateStrings(area.Intents) {
		addIssue(validation, "error", recordPath, "touch-area intents must be unique")
	}
	for _, intent := range area.Intents {
		if !oneOf(intent, "read", "add", "modify", "delete", "rename", "test", "document") {
			addIssue(validation, "error", recordPath, "invalid touch-area intent")
		}
	}
	value := area.Selector.Value
	if area.Selector.Kind == "logical" {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`).MatchString(value) {
			addIssue(validation, "error", recordPath, "logical touch-area selector is invalid")
		}
		return
	}
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") || strings.Contains(value, "//") {
		addIssue(validation, "error", recordPath, "touch-area selector must be a normalized repository-relative slash path")
		return
	}
	if area.Selector.Kind != "glob" && strings.ContainsAny(value, "*?[]{}!") {
		addIssue(validation, "error", recordPath, "file and directory touch areas cannot contain glob syntax")
	}
	if area.Selector.Kind == "glob" && (strings.ContainsAny(value, "[]{}!") || strings.Contains(value, "***")) {
		addIssue(validation, "error", recordPath, "glob touch areas allow only *, ?, and **")
	}
}

func validRepository(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() && parsed.User == nil && parsed.Host != ""
}
func validTime(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func duplicateStrings(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func validateStringList(validation *Validation, path, name string, values []string, required bool) {
	if required && len(values) == 0 {
		addIssue(validation, "error", path, name+" must not be empty")
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			addIssue(validation, "error", path, name+" cannot contain empty values")
		}
	}
}

func referenceIs(value, sagaID string, kind livingid.Kind, id string) bool {
	ref, err := livingid.Parse(value)
	return err == nil && ref.SagaID == sagaID && ref.Kind == kind && ref.ID == id
}
func workItemReference(plan *Plan, value string) bool {
	ref, err := livingid.Parse(value)
	if err != nil || ref.SagaID != plan.SagaID || ref.Kind != livingid.KindWorkItem {
		return false
	}
	_, ok := plan.WorkItems[ref.ID]
	return ok
}
func waveURN(sagaID, id string) string     { value, _ := livingid.Wave(sagaID, id); return value }
func workItemURN(sagaID, id string) string { value, _ := livingid.WorkItem(sagaID, id); return value }
func contractURN(sagaID, id string) string { value, _ := livingid.Contract(sagaID, id); return value }
func dependencyURN(sagaID, id string) string {
	value, _ := livingid.Dependency(sagaID, id)
	return value
}
func revisionURN(sagaID string, kind livingid.Kind, parent, id string) string {
	value, _ := livingid.DefinitionRevision(sagaID, kind, parent, id)
	return value
}
func addConflict(plan *Plan, kind, resource string, heads []string) {
	copied := append([]string{}, heads...)
	sort.Strings(copied)
	plan.Conflicts = append(plan.Conflicts, Conflict{Kind: kind, Resource: resource, Heads: copied})
}

func currentMergeUnits(item *WorkItem) map[string]MergeUnit {
	result := map[string]MergeUnit{}
	if len(item.Heads) != 1 {
		return result
	}
	head := item.Heads[0]
	for _, revision := range item.Revisions {
		if strings.HasSuffix(head, ":revision:"+revision.ID) {
			for _, unit := range revision.MergeUnits {
				result[unit.ID] = unit
			}
			break
		}
	}
	return result
}

func activeWorkspaceOwnerHeads(item *WorkItem) []string {
	parents := map[string]bool{}
	for _, event := range item.Workspaces {
		for _, parent := range event.Parents {
			parents[parent] = true
		}
	}
	heads := []string{}
	for _, event := range item.Workspaces {
		id := eventURNForWorkItem(event.WorkItem, "workspace-event", event.ID)
		if event.Role == "owner" && event.Action == "assigned" && !parents[id] {
			heads = append(heads, id)
		}
	}
	sort.Strings(heads)
	return heads
}

func contractHasRevision(contract *Contract, revisionID string) bool {
	for _, revision := range contract.Revisions {
		if revision.ID == revisionID {
			return true
		}
	}
	return false
}

func findWaveRevision(wave *Wave, urn string) *WaveRevision {
	for index := range wave.Revisions {
		if strings.HasSuffix(urn, ":revision:"+wave.Revisions[index].ID) {
			return &wave.Revisions[index]
		}
	}
	return nil
}

func findWorkItemRevision(item *WorkItem, urn string) *WorkItemRevision {
	for index := range item.Revisions {
		if strings.HasSuffix(urn, ":revision:"+item.Revisions[index].ID) {
			return &item.Revisions[index]
		}
	}
	return nil
}

func findProgressEvent(item *WorkItem, urn string) *ProgressEvent {
	for index := range item.Progress {
		if eventURNForWorkItem(item.Progress[index].WorkItem, "progress-event", item.Progress[index].ID) == urn {
			return &item.Progress[index]
		}
	}
	return nil
}

func findContractRevision(contract *Contract, urn string) *ContractRevision {
	for index := range contract.Revisions {
		if strings.HasSuffix(urn, ":revision:"+contract.Revisions[index].ID) {
			return &contract.Revisions[index]
		}
	}
	return nil
}

func findContractEvent(contract *Contract, urn string) *ContractEvent {
	for index := range contract.Events {
		if eventURNForContract(contract.Events[index].Contract, contract.Events[index].ID) == urn {
			return &contract.Events[index]
		}
	}
	return nil
}
