// Package reviewapp provides the transport-independent application boundary
// for reading a validated Change Saga and its source comparison.
package reviewapp

import (
	"context"
	"time"

	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

const (
	DefaultPageLimit     = 100
	MaxPageLimit         = 1000
	DefaultFragmentLimit = 256 << 10
	MaxFragmentLimit     = 1 << 20
)

type OpenOptions struct {
	SagaRoot  string
	SourceDir string
	// SummaryOnly builds the hierarchy and aggregate coverage/review counts
	// without retaining atom-level indexes used by focused query operations.
	SummaryOnly bool
}

type Session interface {
	Snapshot() string
	Overview(context.Context, OverviewQuery) (Overview, error)
	Children(context.Context, ChildrenQuery) (ChildrenPage, error)
	ReadFragment(context.Context, FragmentQuery) (FragmentContent, error)
	FragmentDiffs(context.Context, FragmentDiffQuery) (FragmentDiffs, error)
	DiffOwners(context.Context, DiffOwnerQuery) (DiffOwnership, error)
	Reviews(context.Context, ReviewQuery) (ReviewPage, error)
	Gaps(context.Context, GapQuery) (GapPage, error)
	Mappings(context.Context, MappingQuery) (MappingPage, error)
	Claims(context.Context, ClaimQuery) (ClaimPage, error)
	Verifications(context.Context, VerificationQuery) (VerificationPage, error)
}

type OverviewQuery struct{}

type ChildrenQuery struct {
	Parent string `json:"parent"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type FragmentQuery struct {
	Target string `json:"target"`
	Offset int64  `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type FragmentDiffQuery struct {
	Target string `json:"target"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type DiffOwnerQuery struct {
	Diff   string `json:"diff"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ReviewQuery struct {
	Target string `json:"target,omitempty"`
	Thread string `json:"thread,omitempty"`
	State  string `json:"state,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type GapQuery struct {
	Kind   string `json:"kind,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type MappingQuery struct {
	Target       string `json:"target,omitempty"`
	Sort         string `json:"sort,omitempty"`
	MinimumScore int    `json:"minimum_score,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type ClaimQuery struct {
	Target string `json:"target,omitempty"`
	Status string `json:"status,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type VerificationQuery struct {
	Claim  string `json:"claim,omitempty"`
	Status string `json:"status,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type Page struct {
	Total      int     `json:"total"`
	Returned   int     `json:"returned"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

type SagaIdentity struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	PR    *saga.PR `json:"pr,omitempty"`
}

type SourceSnapshot struct {
	Repository string `json:"repository"`
	Base       string `json:"base"`
	Head       string `json:"head"`
	BaseOID    string `json:"base_oid"`
	HeadOID    string `json:"head_oid"`
}

type CompactReview struct {
	// LatestState is retained as a wire-compatible name for the aggregate of
	// every reviewer's current decision.
	LatestState    string `json:"latest_state,omitempty"`
	HumanApprovals int    `json:"human_approvals"`
	AIApprovals    int    `json:"ai_approvals"`
	Rejections     int    `json:"rejections"`
	Unspecified    int    `json:"unspecified_decisions"`
	OpenThreads    int    `json:"open_threads"`
}

type CompactDiffs struct {
	Current           int `json:"current"`
	Stale             int `json:"stale"`
	DirectCurrent     int `json:"direct_current"`
	DirectStale       int `json:"direct_stale"`
	DescendantCurrent int `json:"descendant_current"`
	DescendantStale   int `json:"descendant_stale"`
}

type Node struct {
	Kind        string         `json:"kind"`
	Target      string         `json:"target"`
	Parent      string         `json:"parent,omitempty"`
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Order       int            `json:"order,omitempty"`
	HasChildren bool           `json:"has_children"`
	Review      CompactReview  `json:"review"`
	Diffs       CompactDiffs   `json:"diffs"`
	MediaType   string         `json:"media_type,omitempty"`
	Bytes       int64          `json:"bytes,omitempty"`
	Selector    *LandmarkValue `json:"selector,omitempty"`
}

type LandmarkValue struct {
	Type      string  `json:"type"`
	ElementID string  `json:"element_id,omitempty"`
	HeadingID string  `json:"heading_id,omitempty"`
	Exact     string  `json:"exact,omitempty"`
	Prefix    string  `json:"prefix,omitempty"`
	Suffix    string  `json:"suffix,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Width     float64 `json:"width,omitempty"`
	Height    float64 `json:"height,omitempty"`
}

type Overview struct {
	Saga              SagaIdentity     `json:"saga"`
	Source            SourceSnapshot   `json:"source"`
	Root              Node             `json:"root"`
	OverviewFragments []Node           `json:"overview_fragments"`
	Chapters          []ChapterSummary `json:"chapters"`
	Coverage          CoverageSummary  `json:"coverage"`
}

type ChapterSummary struct {
	Node
	ChildCount    int  `json:"child_count"`
	FragmentCount int  `json:"fragment_count"`
	OwnsCurrent   bool `json:"owns_current_diffs"`
	OwnsStale     bool `json:"owns_stale_diffs"`
}

type CoverageSummary struct {
	Complete    bool   `json:"complete"`
	Scope       string `json:"scope"`
	Total       int    `json:"total"`
	Covered     int    `json:"covered"`
	Uncovered   int    `json:"uncovered"`
	Overlapping int    `json:"overlapping"`
	Stale       int    `json:"stale"`
}

type ChildrenPage struct {
	Parent   string `json:"parent"`
	Children []Node `json:"children"`
	Page     Page   `json:"-"`
}

type FragmentChunk struct {
	Encoding   string `json:"encoding"`
	Data       string `json:"data"`
	Offset     int64  `json:"offset"`
	NextOffset *int64 `json:"next_offset"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
}

type AssetSummary struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
}

type FragmentContent struct {
	Target    string             `json:"target"`
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	MediaType string             `json:"media_type"`
	Content   FragmentChunk      `json:"content"`
	Assets    []AssetSummary     `json:"assets"`
	Landmarks []SemanticLandmark `json:"landmarks"`
}

type SemanticLandmark struct {
	Target      string         `json:"target"`
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Selector    *LandmarkValue `json:"selector"`
	Diffs       CompactDiffs   `json:"diffs"`
}

type ResolvedSelector struct {
	URI          string         `json:"uri"`
	Note         string         `json:"note,omitempty"`
	Status       string         `json:"status"`
	Target       string         `json:"target"`
	EvidenceFile string         `json:"evidence_file,omitempty"`
	Atoms        []gitdiff.Atom `json:"atoms,omitempty"`
}

type StaleSelector struct {
	URI          string `json:"uri"`
	Note         string `json:"note,omitempty"`
	Target       string `json:"target"`
	EvidenceFile string `json:"evidence_file,omitempty"`
	Reason       string `json:"reason"`
}

type FragmentDiffs struct {
	Target    string             `json:"target"`
	Selectors []ResolvedSelector `json:"selectors"`
	Atoms     []gitdiff.Atom     `json:"atoms"`
	Stale     []StaleSelector    `json:"stale"`
	Page      Page               `json:"-"`
}

type DiffOwner struct {
	Target       string         `json:"target"`
	Selector     string         `json:"selector"`
	Note         string         `json:"note,omitempty"`
	EvidenceFile string         `json:"evidence_file,omitempty"`
	Mapping      *MappingSignal `json:"mapping,omitempty"`
}

type MappingReason struct {
	Code    string `json:"code"`
	Weight  int    `json:"weight"`
	Message string `json:"message"`
}

// MappingSignal deliberately describes breadth and justification risk, not
// correctness. A high score tells an AI where to scrutinize first; it never
// turns a narrow mapping into proof that the explanation is true.
type MappingSignal struct {
	ScrutinyScore      int             `json:"scrutiny_score"`
	AtomCount          int             `json:"atom_count"`
	AtomsPerNote       float64         `json:"atoms_per_note"`
	FileCount          int             `json:"file_count"`
	TargetAtomCount    int             `json:"target_atom_count"`
	TargetFileCount    int             `json:"target_file_count"`
	StaleSelectorCount int             `json:"stale_selector_count"`
	Reasons            []MappingReason `json:"reasons"`
}

type MappingAssessment struct {
	Target             string          `json:"target"`
	TargetKind         string          `json:"target_kind"`
	EvidenceFile       string          `json:"evidence_file,omitempty"`
	Notes              []string        `json:"notes"`
	SelectorCount      int             `json:"selector_count"`
	StaleSelectorCount int             `json:"stale_selector_count"`
	AtomCount          int             `json:"atom_count"`
	AtomsPerNote       float64         `json:"atoms_per_note"`
	FileCount          int             `json:"file_count"`
	TargetAtomCount    int             `json:"target_atom_count"`
	TargetFileCount    int             `json:"target_file_count"`
	ChangesetShare     float64         `json:"changeset_share"`
	ScrutinyScore      int             `json:"scrutiny_score"`
	Reasons            []MappingReason `json:"reasons"`
}

type MappingPage struct {
	Mappings []MappingAssessment `json:"mappings"`
	Page     Page                `json:"-"`
}

type ClaimEvidence struct {
	URI            string         `json:"uri"`
	Status         string         `json:"status"`
	MappedToTarget bool           `json:"mapped_to_target"`
	Atoms          []gitdiff.Atom `json:"atoms"`
}

type VerificationRecord struct {
	ID          string      `json:"id"`
	Claim       string      `json:"claim"`
	Status      string      `json:"status"`
	Method      string      `json:"method,omitempty"`
	Summary     string      `json:"summary"`
	Command     string      `json:"command,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	Attribution Attribution `json:"attribution"`
}

type ClaimRecord struct {
	ID                 string              `json:"id"`
	Target             string              `json:"target"`
	Kind               string              `json:"kind"`
	Statement          string              `json:"statement"`
	CreatedAt          time.Time           `json:"created_at"`
	Attribution        Attribution         `json:"attribution"`
	Evidence           []ClaimEvidence     `json:"evidence"`
	VerificationStatus string              `json:"verification_status"`
	LatestVerification *VerificationRecord `json:"latest_verification,omitempty"`
}

type ClaimPage struct {
	Claims []ClaimRecord `json:"claims"`
	Page   Page          `json:"-"`
}

type VerificationPage struct {
	Verifications []VerificationRecord `json:"verifications"`
	Page          Page                 `json:"-"`
}

type OwnedAtom struct {
	Atom    gitdiff.Atom   `json:"atom"`
	Owners  []DiffOwner    `json:"owners"`
	Threads []ReviewThread `json:"threads"`
}

type DiffOwnership struct {
	Diff  string      `json:"diff"`
	Kind  string      `json:"kind"`
	Atoms []OwnedAtom `json:"atoms"`
	Page  Page        `json:"-"`
}

type Attribution struct {
	Status      string     `json:"status"`
	Commit      string     `json:"commit,omitempty"`
	Committer   *Committer `json:"committer,omitempty"`
	CommittedAt *time.Time `json:"committed_at,omitempty"`
}

type Committer struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type ReviewFragment struct {
	Target    string `json:"target"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	MediaType string `json:"media_type"`
	Encoding  string `json:"encoding,omitempty"`
	Data      string `json:"data,omitempty"`
	Bytes     int64  `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"`
}

type ReviewMessage struct {
	ID                  string           `json:"id"`
	CreatedAt           time.Time        `json:"created_at"`
	Attribution         Attribution      `json:"attribution"`
	LegacyClaimedAuthor string           `json:"legacy_claimed_author,omitempty"`
	Fragments           []ReviewFragment `json:"fragments"`
}

type ReviewEvent struct {
	ID                  string                 `json:"id"`
	Kind                string                 `json:"kind"`
	Target              string                 `json:"target,omitempty"`
	Diff                string                 `json:"diff,omitempty"`
	State               string                 `json:"state,omitempty"`
	Body                string                 `json:"body,omitempty"`
	Anchor              *saga.Anchor           `json:"anchor,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
	Attribution         Attribution            `json:"attribution"`
	LegacyClaimedAuthor string                 `json:"legacy_claimed_author,omitempty"`
	Reviewer            *saga.ReviewerIdentity `json:"reviewer,omitempty"`
}

type ReviewThread struct {
	ID                  string           `json:"id"`
	Kind                string           `json:"kind"`
	Target              string           `json:"target"`
	Anchor              saga.Anchor      `json:"anchor"`
	Suggestion          *saga.Suggestion `json:"suggestion,omitempty"`
	State               string           `json:"state"`
	CreatedAt           time.Time        `json:"created_at"`
	Attribution         Attribution      `json:"attribution"`
	LegacyClaimedAuthor string           `json:"legacy_claimed_author,omitempty"`
	Messages            []ReviewMessage  `json:"messages"`
	Events              []ReviewEvent    `json:"events"`
}

type ReviewItem struct {
	Kind   string        `json:"kind"`
	Thread *ReviewThread `json:"thread,omitempty"`
	Event  *ReviewEvent  `json:"event,omitempty"`
}

type ReviewPage struct {
	Items []ReviewItem `json:"items"`
	Page  Page         `json:"-"`
}

type UncoveredGap struct {
	Atom gitdiff.Atom `json:"atom"`
}

type OverlapGap struct {
	Atom   gitdiff.Atom `json:"atom"`
	Owners []DiffOwner  `json:"owners"`
}

type Gap struct {
	Kind      string         `json:"kind"`
	Uncovered *UncoveredGap  `json:"uncovered,omitempty"`
	Stale     *StaleSelector `json:"stale,omitempty"`
	Overlap   *OverlapGap    `json:"overlap,omitempty"`
}

type GapPage struct {
	Gaps []Gap `json:"gaps"`
	Page Page  `json:"-"`
}
