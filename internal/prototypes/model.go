// Package prototypes owns the optional v3 prototype capability of a living
// Change Saga. It is deliberately independent of the Saga loader, CLI, query
// registry, server, and requirements package; cross-domain checks happen only
// through Compose.
package prototypes

import "time"

const (
	Version = 3

	IdentitySchemaURL   = "https://changesaga.dev/schema/v3/prototype.schema.json"
	RevisionSchemaURL   = "https://changesaga.dev/schema/v3/prototype-revision.schema.json"
	AnnotationSchemaURL = "https://changesaga.dev/schema/v3/prototype-annotation.schema.json"

	MaxPrototypes            = 10_000
	MaxRevisionsPerPrototype = 10_000
	MaxAnnotations           = 50_000
	MaxRecordBytes           = 1 << 20
	MaxHTMLFiles             = 2_000
	MaxHTMLFileBytes         = 8 << 20
	MaxHTMLPackageBytes      = 64 << 20
)

type Identity struct {
	Schema    string    `json:"$schema"`
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	RequestID string    `json:"request_id,omitempty"`
}

type State string

const (
	StateDraft   State = "draft"
	StateReady   State = "ready"
	StateRetired State = "retired"
)

type SourceKind string

const (
	SourceHTML     SourceKind = "html"
	SourceExternal SourceKind = "external"
	SourceEmbed    SourceKind = "embed"
)

// Source is a closed discriminated union. HTML paths are relative to their
// immutable revision package. Embed allowlisting is persisted explicitly so a
// viewer never infers permission merely from a recognizable hostname.
type Source struct {
	Kind          SourceKind         `json:"kind"`
	Entrypoint    string             `json:"entrypoint,omitempty"`
	ContentDigest string             `json:"content_digest,omitempty"`
	URL           string             `json:"url,omitempty"`
	EmbedURL      string             `json:"embed_url,omitempty"`
	FallbackURL   string             `json:"fallback_url,omitempty"`
	Allowlist     *ProviderAllowlist `json:"allowlist,omitempty"`
}

type ProviderAllowlist struct {
	Provider    string   `json:"provider"`
	EmbedOrigin string   `json:"embed_origin"`
	Sandbox     []string `json:"sandbox"`
	Permissions []string `json:"permissions"`
}

type StyleSource struct {
	Path             string      `json:"path"`
	Digest           string      `json:"digest"`
	CustomProperties []string    `json:"custom_properties"`
	Roles            []StyleRole `json:"roles"`

	Stale       bool   `json:"-"`
	StaleReason string `json:"-"`
}

type StyleRole struct {
	Role  string `json:"role"`
	Class string `json:"class"`
}

// Revision is a complete snapshot. Source files for an HTML revision are
// immutable children of the same <id>.revision package as revision.json.
type Revision struct {
	Schema    string        `json:"$schema"`
	Version   int           `json:"version"`
	ID        string        `json:"id"`
	Prototype string        `json:"prototype"`
	Parents   []string      `json:"parents"`
	Title     string        `json:"title"`
	State     State         `json:"state"`
	Source    Source        `json:"source"`
	Styles    []StyleSource `json:"styles"`
	CreatedAt time.Time     `json:"created_at"`
	RequestID string        `json:"request_id,omitempty"`
}

type SelectorKind string

const (
	SelectorElement  SelectorKind = "element"
	SelectorText     SelectorKind = "text"
	SelectorRegion   SelectorKind = "region"
	SelectorProvider SelectorKind = "provider"
)

type Selector struct {
	Kind       SelectorKind `json:"kind"`
	ElementID  string       `json:"element_id,omitempty"`
	ExactText  string       `json:"exact_text,omitempty"`
	Region     *Region      `json:"region,omitempty"`
	ProviderID string       `json:"provider_id,omitempty"`
	DeepLink   string       `json:"deep_link,omitempty"`
}

type Region struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Annotation is the single persisted edge between a prototype and a story or
// criterion. Endpoint existence is intentionally not a record-level rule.
type Annotation struct {
	Schema                 string    `json:"$schema"`
	Version                int       `json:"version"`
	ID                     string    `json:"id"`
	Prototype              string    `json:"prototype"`
	Target                 string    `json:"target"`
	Rationale              string    `json:"rationale"`
	PrototypeRevision      string    `json:"prototype_revision,omitempty"`
	PrototypeContentDigest string    `json:"prototype_content_digest,omitempty"`
	StoryRevision          string    `json:"story_revision"`
	Selector               Selector  `json:"selector"`
	CreatedAt              time.Time `json:"created_at"`
	RequestID              string    `json:"request_id,omitempty"`
}

type Prototype struct {
	Identity        Identity
	Revisions       []Revision
	RevisionHeads   []string
	CurrentRevision *Revision
}

func (p Prototype) RevisionConflict() bool { return len(p.RevisionHeads) > 1 }

type Document struct {
	Root        string
	SagaID      string
	Adopted     bool
	Prototypes  []Prototype
	Annotations []Annotation
}

type LoadOptions struct {
	// RepositoryRoot is optional. When supplied, declared repository CSS is
	// re-read safely and its projection is marked stale if its pin changed.
	RepositoryRoot string
}

type MutationResult struct {
	URN      string
	Path     string
	Replayed bool
}

type AddHTMLInput struct {
	ID, RevisionID, Title string
	State                 State
	SourcePath            string
	CreatedAt             time.Time
	RequestID             string
}

type AddExternalInput struct {
	ID, RevisionID, Title string
	State                 State
	URL                   string
	EmbedURL              string
	FallbackURL           string
	Allowlist             *ProviderAllowlist
	CreatedAt             time.Time
	RequestID             string
}

type ReviseInput struct {
	Prototype      string
	ID             string
	Parents        []string
	Title          string
	State          State
	Source         Source
	HTMLSourcePath string
	Styles         []StyleSource
	CreatedAt      time.Time
	RequestID      string
}

type AddAnnotationInput struct {
	ID, Prototype, Target, Rationale string
	PrototypeRevision                string
	PrototypeContentDigest           string
	StoryRevision                    string
	Selector                         Selector
	CreatedAt                        time.Time
	RequestID                        string
}

type AddStyleInput struct {
	Prototype        string
	ID               string
	Parents          []string
	RepositoryRoot   string
	Path             string
	CustomProperties []string
	Roles            []StyleRole
	CreatedAt        time.Time
	RequestID        string
}

// CompositionInputs are owned by the caller so this package does not import
// the requirements domain. They describe only the current requirement heads.
type CompositionInputs struct {
	Stories []StoryInput
}

type StoryInput struct {
	URN               string
	CurrentRevision   string
	Criteria          []string
	PrototypeRequired bool
}

type QualityGap struct {
	Kind     string `json:"kind"`
	Resource string `json:"resource"`
	Message  string `json:"message"`
}

type AnnotationProjection struct {
	Annotation        Annotation `json:"annotation"`
	PrototypeResolved bool       `json:"prototype_resolved"`
	TargetResolved    bool       `json:"target_resolved"`
	Current           bool       `json:"current"`
	StaleReasons      []string   `json:"stale_reasons"`
}

type ResourceCoverage struct {
	Resource    string   `json:"resource"`
	Status      string   `json:"status"`
	Annotations []string `json:"annotations"`
}

type CoverageProjection struct {
	Capability  string                 `json:"capability"`
	Annotations []AnnotationProjection `json:"annotations"`
	Prototypes  []ResourceCoverage     `json:"prototypes"`
	Stories     []ResourceCoverage     `json:"stories"`
	Gaps        []QualityGap           `json:"gaps"`
}
