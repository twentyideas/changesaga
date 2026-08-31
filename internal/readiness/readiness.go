// Package readiness computes living-Saga delivery projections from an already
// validated graph. It deliberately has no filesystem or transport dependency.
package readiness

import "sort"

// Criterion is the current accepted criterion input to the projection.
type Criterion struct {
	URN            string
	Designed       bool
	Planned        bool
	Evidence       []string
	DirectBlockers []Blocker
	UpstreamPaths  []BlockerPath
}

// Blocker describes one fact preventing a current delivery path.
type Blocker struct {
	Code     string   `json:"code"`
	Resource string   `json:"resource,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Path     []string `json:"path,omitempty"`
}

// BlockerPath retains the dependency chain from a criterion's work item to an
// unsatisfied prerequisite. Callers can explain a transitive result without
// reimplementing graph traversal.
type BlockerPath struct {
	Path    []string `json:"path"`
	Blocker Blocker  `json:"blocker"`
}

type CriterionResult struct {
	Criterion       string        `json:"criterion"`
	Designed        bool          `json:"designed"`
	Planned         bool          `json:"planned"`
	Delivered       bool          `json:"delivered"`
	Evidence        []string      `json:"evidence"`
	Blockers        []Blocker     `json:"blockers"`
	TransitivePaths []BlockerPath `json:"transitive_blocker_paths"`
}

type Axis struct {
	Total   int `json:"total"`
	Covered int `json:"covered"`
	Missing int `json:"missing"`
}

type Projection struct {
	PeerReviewReady     bool              `json:"peer_review_ready"`
	RequirementCoverage Axis              `json:"requirement_coverage"`
	PlanCoverage        Axis              `json:"plan_coverage"`
	DeliveryCoverage    Axis              `json:"delivery_coverage"`
	Criteria            []CriterionResult `json:"criteria"`
}

// Evaluate keeps the three coverage axes independent. In particular, a work
// item's authored progress state is never an evidence input and therefore can
// never make Delivered true.
func Evaluate(criteria []Criterion) Projection {
	ordered := append([]Criterion(nil), criteria...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].URN < ordered[j].URN })
	result := Projection{Criteria: make([]CriterionResult, 0, len(ordered))}
	for _, input := range ordered {
		evidence := uniqueSorted(input.Evidence)
		blockers := append([]Blocker(nil), input.DirectBlockers...)
		paths := append([]BlockerPath(nil), input.UpstreamPaths...)
		sort.Slice(blockers, func(i, j int) bool {
			if blockers[i].Code != blockers[j].Code {
				return blockers[i].Code < blockers[j].Code
			}
			return blockers[i].Resource < blockers[j].Resource
		})
		sort.Slice(paths, func(i, j int) bool { return pathKey(paths[i]) < pathKey(paths[j]) })
		delivered := len(evidence) > 0 && !hasDeliveryBlocker(blockers) && len(paths) == 0
		item := CriterionResult{
			Criterion: input.URN, Designed: input.Designed, Planned: input.Planned,
			Delivered: delivered, Evidence: evidence, Blockers: blockers, TransitivePaths: paths,
		}
		result.Criteria = append(result.Criteria, item)
		if input.Designed {
			result.RequirementCoverage.Covered++
		}
		if input.Planned {
			result.PlanCoverage.Covered++
		}
		if delivered {
			result.DeliveryCoverage.Covered++
		}
	}
	result.RequirementCoverage.Total = len(ordered)
	result.PlanCoverage.Total = len(ordered)
	result.DeliveryCoverage.Total = len(ordered)
	result.RequirementCoverage.Missing = len(ordered) - result.RequirementCoverage.Covered
	result.PlanCoverage.Missing = len(ordered) - result.PlanCoverage.Covered
	result.DeliveryCoverage.Missing = len(ordered) - result.DeliveryCoverage.Covered
	result.PeerReviewReady = len(ordered) > 0 && result.DeliveryCoverage.Missing == 0
	return result
}

// Missing design or work-plan coverage is still useful planning guidance, but
// those optional axes do not veto peer-review readiness when a criterion has a
// complete immutable delivery path. Other blockers and transitive dependency
// paths describe a broken delivery path and therefore still prevent delivery.
func hasDeliveryBlocker(blockers []Blocker) bool {
	for _, blocker := range blockers {
		if blocker.Code != "design_missing" && blocker.Code != "work_item_missing" {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func pathKey(value BlockerPath) string {
	key := value.Blocker.Code + "\x00" + value.Blocker.Resource
	for _, node := range value.Path {
		key += "\x00" + node
	}
	return key
}
