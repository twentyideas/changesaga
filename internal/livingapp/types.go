// Package livingapp is the transport-neutral application boundary for living
// Saga reads. It composes the requirements and work-plan projections without
// teaching either leaf domain about the other.
package livingapp

import (
	"context"
	"time"

	"github.com/twentyideas/changesaga/internal/readiness"
	"github.com/twentyideas/changesaga/internal/requirements"
	"github.com/twentyideas/changesaga/internal/workplan"
)

const (
	DefaultPageLimit = 100
	MaxPageLimit     = 1000
)

type OpenOptions struct {
	SagaRoot string
	// Snapshot binds this composed projection to an enclosing read session.
	// Transport adapters that already own the established saga/source snapshot
	// pass it here so all operations expose one public snapshot namespace.
	Snapshot string
}

type Query struct {
	Operation string
	Filters   Filters
	Cursor    string
	Limit     int
}

type Filters struct {
	Requirement string
	State       string
	Kind        string
	Citation    string
	Relation    string
	From        string
	To          string
	Wave        string
	Item        string
	Status      string
}

type Page struct {
	Total      int     `json:"total"`
	Returned   int     `json:"returned"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

type Result struct {
	Data any
	Page Page
}

type Session interface {
	Snapshot() string
	Query(context.Context, Query) (Result, error)
}

type RequirementPage struct {
	Requirements []Requirement `json:"requirements"`
}
type Requirement struct {
	Requirement       string                       `json:"requirement"`
	ID                string                       `json:"id"`
	CreatedAt         time.Time                    `json:"created_at"`
	RevisionHeads     []string                     `json:"revision_heads"`
	LifecycleHeads    []string                     `json:"lifecycle_heads"`
	CurrentRevision   *requirements.Revision       `json:"current_revision"`
	CurrentLifecycle  *requirements.LifecycleEvent `json:"current_lifecycle"`
	RevisionConflict  bool                         `json:"revision_conflict"`
	LifecycleConflict bool                         `json:"lifecycle_conflict"`
}

type HistoryPage struct {
	Events []HistoryEvent `json:"events"`
}
type HistoryEvent struct {
	Kind      string                       `json:"kind"`
	Resource  string                       `json:"resource"`
	Depth     int                          `json:"graph_depth"`
	CreatedAt time.Time                    `json:"created_at"`
	Revision  *requirements.Revision       `json:"revision,omitempty"`
	Lifecycle *requirements.LifecycleEvent `json:"lifecycle,omitempty"`
}

type CitationPage struct {
	Citations []Citation `json:"citations"`
}
type Citation struct {
	URN string `json:"urn"`
	requirements.Citation
}

type RelationPage struct {
	Relations []Relation `json:"relations"`
}
type Relation struct {
	URN          string   `json:"urn"`
	Stale        bool     `json:"stale"`
	StaleReasons []string `json:"stale_reasons"`
	requirements.Relation
}

type WavePage struct {
	Waves []Wave `json:"waves"`
}
type Wave struct {
	Wave            string                 `json:"wave"`
	ID              string                 `json:"id"`
	Heads           []string               `json:"heads"`
	CurrentRevision *workplan.WaveRevision `json:"current_revision"`
	Conflicted      bool                   `json:"conflicted"`
	ItemCounts      map[string]int         `json:"item_counts"`
}

type WorkItemPage struct {
	Items []WorkItem `json:"items"`
}
type WorkItem struct {
	WorkItem         string                     `json:"work_item"`
	ID               string                     `json:"id"`
	Heads            []string                   `json:"heads"`
	CurrentRevision  *workplan.WorkItemRevision `json:"current_revision"`
	CurrentProgress  *workplan.ProgressEvent    `json:"current_progress"`
	Conflicted       bool                       `json:"conflicted"`
	Ready            bool                       `json:"ready"`
	Blockers         []DependencyBlocker        `json:"blockers"`
	ActiveWorkspaces []workplan.Workspace       `json:"active_workspaces"`
	MergeEvidence    []string                   `json:"merge_evidence"`
}

type DependencyBlocker struct {
	Dependency string   `json:"dependency"`
	Reason     string   `json:"reason"`
	Path       []string `json:"path"`
}

type WorkEventPage struct {
	Events []WorkEvent `json:"events"`
}
type WorkEvent struct {
	Kind      string                   `json:"kind"`
	Resource  string                   `json:"resource"`
	Item      string                   `json:"item,omitempty"`
	CreatedAt time.Time                `json:"created_at"`
	Progress  *workplan.ProgressEvent  `json:"progress,omitempty"`
	Workspace *workplan.WorkspaceEvent `json:"workspace,omitempty"`
	Merge     *workplan.MergeEvent     `json:"merge,omitempty"`
	Contract  *workplan.ContractEvent  `json:"contract,omitempty"`
}

type ConflictPage struct {
	Conflicts []Conflict `json:"conflicts"`
}
type Conflict struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Severity string   `json:"severity"`
	Resource string   `json:"resource"`
	Heads    []string `json:"heads"`
}

type TraceabilityPage struct {
	Criteria []Traceability `json:"criteria"`
}
type Traceability struct {
	Criterion              string                  `json:"criterion"`
	Story                  string                  `json:"story"`
	Revision               string                  `json:"revision"`
	Design                 []string                `json:"design"`
	WorkItems              []string                `json:"work_items"`
	Evidence               []string                `json:"evidence"`
	Paths                  [][]string              `json:"paths"`
	Blockers               []readiness.Blocker     `json:"blockers"`
	TransitiveBlockerPaths []readiness.BlockerPath `json:"transitive_blocker_paths"`
	Delivered              bool                    `json:"delivered"`
}

type ReadinessPage struct {
	Summary      ReadinessSummary       `json:"summary"`
	Requirements []RequirementReadiness `json:"requirements"`
}
type ReadinessSummary struct {
	Status              string         `json:"status"`
	PeerReviewReady     bool           `json:"peer_review_ready"`
	AcceptedCriteria    int            `json:"accepted_criteria"`
	RequirementCoverage readiness.Axis `json:"requirement_coverage"`
	PlanCoverage        readiness.Axis `json:"plan_coverage"`
	DeliveryCoverage    readiness.Axis `json:"delivery_coverage"`
}
type RequirementReadiness struct {
	Requirement string                      `json:"requirement"`
	Status      string                      `json:"status"`
	Criteria    []readiness.CriterionResult `json:"criteria"`
}

type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeInvalidSaga     ErrorCode = "invalid_saga"
	CodeStaleSnapshot   ErrorCode = "stale_snapshot"
	CodeInternal        ErrorCode = "internal"
)

type Error struct {
	Code      ErrorCode      `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
	cause     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.cause }
