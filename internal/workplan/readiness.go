package workplan

import "strings"

// DependencySatisfied evaluates only persisted work-plan facts. Wave order,
// wave lifecycle, workspace ancestry, and process liveness are intentionally
// absent from every condition.
func DependencySatisfied(plan Plan, dependencyID string) (bool, string) {
	dependency := plan.Dependencies[dependencyID]
	if dependency == nil {
		return false, "dependency_not_found"
	}
	refID := workItemID(dependency.Prerequisite)
	item := plan.WorkItems[refID]
	if item == nil {
		return false, "prerequisite_not_found"
	}
	switch dependency.Condition.Kind {
	case "progress_done":
		heads, states := progressHeads(item)
		if len(heads) != 1 {
			return false, "progress_conflicted"
		}
		if states[heads[0]] != "done" {
			return false, "progress_not_done"
		}
		return true, ""
	case "merge_integrated":
		units := currentMergeUnits(item)
		required := 0
		for id, unit := range units {
			if !unit.Required {
				continue
			}
			required++
			heads, states := mergeHeads(item, id)
			if len(heads) != 1 {
				return false, "merge_conflicted"
			}
			if states[heads[0]] != "integrated" {
				return false, "merge_not_integrated"
			}
		}
		if required == 0 {
			return false, "required_merge_not_planned"
		}
		return true, ""
	case "contract_fulfilled":
		contractID, revisionID := contractRevisionIDs(dependency.Condition.ContractRevision)
		contract := plan.Contracts[contractID]
		if contract == nil {
			return false, "contract_not_found"
		}
		if len(contract.Heads) != 1 || !strings.HasSuffix(contract.Heads[0], ":revision:"+revisionID) {
			return false, "contract_revision_stale"
		}
		heads, states := contractHeads(contract)
		if len(heads) != 1 {
			return false, "contract_conflicted"
		}
		if states[heads[0]] != "fulfilled" {
			return false, "contract_not_fulfilled"
		}
		return true, ""
	default:
		return false, "invalid_condition"
	}
}

func workItemID(urn string) string {
	const marker = ":work-item:"
	if index := strings.LastIndex(urn, marker); index >= 0 {
		return urn[index+len(marker):]
	}
	return ""
}

func contractRevisionIDs(urn string) (string, string) {
	const contractMarker = ":contract:"
	const revisionMarker = ":revision:"
	contract := strings.LastIndex(urn, contractMarker)
	revision := strings.LastIndex(urn, revisionMarker)
	if contract < 0 || revision <= contract {
		return "", ""
	}
	return urn[contract+len(contractMarker) : revision], urn[revision+len(revisionMarker):]
}
