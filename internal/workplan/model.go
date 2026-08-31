// Package workplan implements the storage, validation, and bounded mutations
// for the living-Saga v3 work-plan domain. It deliberately has no CLI, query,
// or server dependencies so those adapters can evolve independently.
package workplan

import "time"

const (
	Version = 3
	RootDir = "___workplan"

	WaveSchema       = "https://changesaga.dev/schema/v3/wave.schema.json"
	WorkItemSchema   = "https://changesaga.dev/schema/v3/work-item.schema.json"
	DependencySchema = "https://changesaga.dev/schema/v3/dependency.schema.json"
	ContractSchema   = "https://changesaga.dev/schema/v3/contract.schema.json"
	RevisionSchema   = "https://changesaga.dev/schema/v3/workplan-revision.schema.json"
	EventSchema      = "https://changesaga.dev/schema/v3/workplan-event.schema.json"

	MaxWaves                = 10_000
	MaxWorkItems            = 20_000
	MaxDependencies         = 50_000
	MaxContracts            = 20_000
	MaxRevisionsPerResource = 10_000
	MaxEventsPerStream      = 50_000
	MaxRecordBytes          = 1 << 20
)

type Identity struct {
	Schema    string    `json:"$schema"`
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type Wave struct {
	Identity
	Revisions       []WaveRevision `json:"-"`
	Heads           []string       `json:"-"`
	CurrentRevision *WaveRevision  `json:"-"`
}

type WaveRevision struct {
	Schema          string    `json:"$schema"`
	Version         int       `json:"version"`
	ID              string    `json:"id"`
	Wave            string    `json:"wave"`
	Parents         []string  `json:"parents"`
	Title           string    `json:"title"`
	Objective       string    `json:"objective"`
	Order           int       `json:"order"`
	EntryConditions []string  `json:"entry_conditions"`
	ExitConditions  []string  `json:"exit_conditions"`
	CreatedAt       time.Time `json:"created_at"`
	RequestID       string    `json:"request_id,omitempty"`
	RequestDigest   string    `json:"request_digest,omitempty"`
}

type WorkItem struct {
	Identity
	Revisions       []WorkItemRevision `json:"-"`
	Heads           []string           `json:"-"`
	Progress        []ProgressEvent    `json:"-"`
	Workspaces      []WorkspaceEvent   `json:"-"`
	Merges          []MergeEvent       `json:"-"`
	CurrentRevision *WorkItemRevision  `json:"-"`
	CurrentProgress *ProgressEvent     `json:"-"`
}

type WorkItemRevision struct {
	Schema             string      `json:"$schema"`
	Version            int         `json:"version"`
	ID                 string      `json:"id"`
	WorkItem           string      `json:"work_item"`
	Parents            []string    `json:"parents"`
	Title              string      `json:"title"`
	Objective          string      `json:"objective"`
	Deliverables       []string    `json:"deliverables"`
	Wave               string      `json:"wave,omitempty"`
	Relations          []string    `json:"relations"`
	Dependencies       []string    `json:"dependencies"`
	Contracts          []string    `json:"contracts"`
	ExpectedTouchAreas []TouchArea `json:"expected_touch_areas"`
	CompletionChecks   []string    `json:"completion_checks"`
	MergeUnits         []MergeUnit `json:"merge_units"`
	CreatedAt          time.Time   `json:"created_at"`
	RequestID          string      `json:"request_id,omitempty"`
	RequestDigest      string      `json:"request_digest,omitempty"`
}

type TouchArea struct {
	Repository string        `json:"repository"`
	Selector   TouchSelector `json:"selector"`
	Intents    []string      `json:"intents"`
	Reason     string        `json:"reason,omitempty"`
}

type TouchSelector struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type MergeUnit struct {
	ID           string `json:"id"`
	Repository   string `json:"repository"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Required     bool   `json:"required"`
}

type Dependency struct {
	Schema        string              `json:"$schema"`
	Version       int                 `json:"version"`
	ID            string              `json:"id"`
	Prerequisite  string              `json:"prerequisite"`
	Dependent     string              `json:"dependent"`
	Condition     DependencyCondition `json:"condition"`
	Reason        string              `json:"reason"`
	CreatedAt     time.Time           `json:"created_at"`
	RequestID     string              `json:"request_id,omitempty"`
	RequestDigest string              `json:"request_digest,omitempty"`
}

type DependencyCondition struct {
	Kind             string `json:"kind"`
	ContractRevision string `json:"contract_revision,omitempty"`
}

type Contract struct {
	Identity
	Revisions       []ContractRevision `json:"-"`
	Heads           []string           `json:"-"`
	Events          []ContractEvent    `json:"-"`
	CurrentRevision *ContractRevision  `json:"-"`
	CurrentEvent    *ContractEvent     `json:"-"`
}

type ContractRevision struct {
	Schema        string    `json:"$schema"`
	Version       int       `json:"version"`
	ID            string    `json:"id"`
	Contract      string    `json:"contract"`
	Parents       []string  `json:"parents"`
	Kind          string    `json:"kind"`
	Provider      string    `json:"provider"`
	Consumer      string    `json:"consumer"`
	Statement     string    `json:"statement"`
	Acceptance    []string  `json:"acceptance"`
	CreatedAt     time.Time `json:"created_at"`
	RequestID     string    `json:"request_id,omitempty"`
	RequestDigest string    `json:"request_digest,omitempty"`
}

// EventBase is shared by append-only state graphs. Parents are event URNs,
// never timestamps. Multiple current heads are retained as an explicit
// conflict; a reconciliation event names every head as its parents.
type EventBase struct {
	Schema        string    `json:"$schema"`
	Version       int       `json:"version"`
	ID            string    `json:"id"`
	Parents       []string  `json:"parents"`
	CreatedAt     time.Time `json:"created_at"`
	Summary       string    `json:"summary,omitempty"`
	RequestID     string    `json:"request_id,omitempty"`
	RequestDigest string    `json:"request_digest,omitempty"`
}

type ProgressEvent struct {
	EventBase
	WorkItem string `json:"work_item"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
}

type WorkspaceEvent struct {
	EventBase
	WorkItem  string    `json:"work_item"`
	Action    string    `json:"action"`
	Role      string    `json:"role"`
	Workspace Workspace `json:"workspace"`
}

type Workspace struct {
	Provider     string `json:"provider"`
	ID           string `json:"id"`
	RepositoryID string `json:"repository_id"`
	Branch       string `json:"branch"`
	SourceBranch string `json:"source_branch,omitempty"`
	Label        string `json:"label,omitempty"`
}

type MergeEvent struct {
	EventBase
	WorkItem  string `json:"work_item"`
	Unit      string `json:"unit"`
	State     string `json:"state"`
	HeadOID   string `json:"head_oid,omitempty"`
	MergeOID  string `json:"merge_oid,omitempty"`
	RevertOID string `json:"revert_oid,omitempty"`
}

type ContractEvent struct {
	EventBase
	Contract string   `json:"contract"`
	State    string   `json:"state"`
	Evidence []string `json:"evidence"`
}

type Plan struct {
	Root         string
	SagaID       string
	Waves        map[string]*Wave
	WorkItems    map[string]*WorkItem
	Dependencies map[string]*Dependency
	Contracts    map[string]*Contract
	Requests     map[string]RequestRecord
	Conflicts    []Conflict
}

type RequestRecord struct {
	Operation string
	Digest    string
	Resource  string
	EventID   string
	Path      string
}

type Conflict struct {
	Kind     string   `json:"kind"`
	Resource string   `json:"resource"`
	Heads    []string `json:"heads"`
}

type Issue struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

type Validation struct {
	Valid  bool    `json:"valid"`
	Issues []Issue `json:"issues"`
}

type MutationResult struct {
	URN      string   `json:"urn,omitempty"`
	Path     string   `json:"path,omitempty"`
	Created  []string `json:"created"`
	EventIDs []string `json:"event_ids"`
	Replayed bool     `json:"replayed"`
}
