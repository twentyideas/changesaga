// Package requirements owns the v3 requirements-domain records of a living
// Change Saga. It intentionally has no dependency on the central saga loader,
// query registry, or CLI dispatcher so those integration points can evolve
// independently.
package requirements

import "time"

const (
	Version = 3

	StorySchemaURL          = "https://changesaga.dev/schema/v3/story.schema.json"
	RevisionSchemaURL       = "https://changesaga.dev/schema/v3/story-revision.schema.json"
	LifecycleEventSchemaURL = "https://changesaga.dev/schema/v3/story-event.schema.json"
	CitationSchemaURL       = "https://changesaga.dev/schema/v3/citation.schema.json"
	RelationSchemaURL       = "https://changesaga.dev/schema/v3/relation.schema.json"

	MaxStories           = 10_000
	MaxRevisionsPerStory = 10_000
	MaxEventsPerStory    = 10_000
	MaxCitations         = 20_000
	MaxRelations         = 50_000
	MaxRecordBytes       = 1 << 20
)

type StoryIdentity struct {
	Schema    string    `json:"$schema"`
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	RequestID string    `json:"request_id,omitempty"`
}

type Criterion struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

// Revision is a complete story snapshot. A reader never inherits omitted
// values from a parent, which keeps branch merges deterministic.
type Revision struct {
	Schema             string      `json:"$schema"`
	Version            int         `json:"version"`
	ID                 string      `json:"id"`
	Story              string      `json:"story"`
	Parents            []string    `json:"parents"`
	Title              string      `json:"title"`
	Statement          string      `json:"statement"`
	Priority           string      `json:"priority"`
	Citations          []string    `json:"citations"`
	AcceptanceCriteria []Criterion `json:"acceptance_criteria"`
	CreatedAt          time.Time   `json:"created_at"`
	RequestID          string      `json:"request_id,omitempty"`
}

type LifecycleState string

const (
	StateProposed LifecycleState = "proposed"
	StateAccepted LifecycleState = "accepted"
	StateDeferred LifecycleState = "deferred"
	StateRejected LifecycleState = "rejected"
	StateRetired  LifecycleState = "retired"
)

type LifecycleEvent struct {
	Schema    string         `json:"$schema"`
	Version   int            `json:"version"`
	ID        string         `json:"id"`
	Story     string         `json:"story"`
	Parents   []string       `json:"parents"`
	State     LifecycleState `json:"state"`
	Reason    string         `json:"reason,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	RequestID string         `json:"request_id,omitempty"`
}

type CitationKind string

const (
	CitationURL              CitationKind = "url"
	CitationRepositoryCommit CitationKind = "repository_commit"
	CitationIssue            CitationKind = "issue"
	CitationDocument         CitationKind = "document"
	CitationDecision         CitationKind = "decision"
)

// Citation is immutable. Reference is the authoritative locator: an absolute
// URL, repository-and-commit notation, issue key/URL, document identity, or
// recorded-decision identity according to Kind.
type Citation struct {
	Schema    string       `json:"$schema"`
	Version   int          `json:"version"`
	ID        string       `json:"id"`
	Kind      CitationKind `json:"kind"`
	Title     string       `json:"title"`
	Reference string       `json:"reference"`
	CreatedAt time.Time    `json:"created_at"`
	RequestID string       `json:"request_id,omitempty"`
}

type RelationType string

const (
	RelationRefines       RelationType = "refines"
	RelationAddresses     RelationType = "addresses"
	RelationImplements    RelationType = "implements"
	RelationVerifies      RelationType = "verifies"
	RelationSupersedes    RelationType = "supersedes"
	RelationConflictsWith RelationType = "conflicts_with"
)

type RelationState string

const (
	RelationActive     RelationState = "active"
	RelationSuperseded RelationState = "superseded"
)

// Relation pins mutable endpoints separately from their stable identities.
// Computed stale fields are projections and are never persisted.
type Relation struct {
	Schema             string        `json:"$schema"`
	Version            int           `json:"version"`
	ID                 string        `json:"id"`
	Type               RelationType  `json:"type"`
	From               string        `json:"from"`
	To                 string        `json:"to"`
	Rationale          string        `json:"rationale"`
	FromRevision       string        `json:"from_revision,omitempty"`
	ToRevision         string        `json:"to_revision,omitempty"`
	FromContentDigest  string        `json:"from_content_digest,omitempty"`
	ToContentDigest    string        `json:"to_content_digest,omitempty"`
	State              RelationState `json:"state"`
	CreatedAt          time.Time     `json:"created_at"`
	RequestID          string        `json:"request_id,omitempty"`
	SupersededAt       *time.Time    `json:"superseded_at,omitempty"`
	SupersedeRequestID string        `json:"supersede_request_id,omitempty"`

	Stale        bool     `json:"-"`
	StaleReasons []string `json:"-"`
}

type Story struct {
	Identity  StoryIdentity
	Revisions []Revision
	Events    []LifecycleEvent

	RevisionHeads    []string
	LifecycleHeads   []string
	CurrentRevision  *Revision
	CurrentLifecycle *LifecycleEvent
}

func (story Story) RevisionConflict() bool  { return len(story.RevisionHeads) > 1 }
func (story Story) LifecycleConflict() bool { return len(story.LifecycleHeads) > 1 }

type Document struct {
	Root      string
	SagaID    string
	Stories   []Story
	Citations []Citation
	Relations []Relation
}

// StaleInputs supplies current values owned outside this package. Keys are
// stable endpoint URNs. Missing marks identities known to have disappeared;
// an absent map entry alone means "unknown", not stale.
type StaleInputs struct {
	CurrentRevisions      map[string]string
	CurrentContentDigests map[string]string
	Missing               map[string]bool
}

type LoadOptions struct {
	StaleInputs StaleInputs
}

type AddStoryInput struct {
	ID                 string
	RevisionID         string
	EventID            string
	Title              string
	Statement          string
	Priority           string
	Citations          []string
	AcceptanceCriteria []Criterion
	CreatedAt          time.Time
	RequestID          string
}

type ReviseStoryInput struct {
	Story              string
	ID                 string
	Parents            []string
	Title              string
	Statement          string
	Priority           string
	Citations          []string
	AcceptanceCriteria []Criterion
	CreatedAt          time.Time
	RequestID          string
}

type SetStoryStateInput struct {
	Story     string
	ID        string
	Parents   []string
	State     LifecycleState
	Reason    string
	CreatedAt time.Time
	RequestID string
}

type AddCitationInput struct {
	ID        string
	Kind      CitationKind
	Title     string
	Reference string
	CreatedAt time.Time
	RequestID string
}

type AddRelationInput struct {
	ID                string
	Type              RelationType
	From              string
	To                string
	Rationale         string
	FromRevision      string
	ToRevision        string
	FromContentDigest string
	ToContentDigest   string
	CreatedAt         time.Time
	RequestID         string
}

type MutationResult struct {
	URN      string
	Path     string
	Replayed bool
}
