package sagaref

import "context"

// ResolutionStatus is independent of transport success and containing-Saga
// validity. In particular, unavailable is a valid resolution outcome.
type ResolutionStatus string

const (
	StatusResolved    ResolutionStatus = "resolved"
	StatusStale       ResolutionStatus = "stale"
	StatusUnavailable ResolutionStatus = "unavailable"
)

// Resolution is the transport-neutral result of resolving a portable
// reference. CurrentRevision is useful for a stale result; it never changes
// Reference.Revision or silently retargets the reference.
type Resolution struct {
	Status          ResolutionStatus
	Reference       Reference
	Target          *Target
	CurrentRevision string
	Detail          string
}

// Resolver resolves a portable reference. Implementations may report
// StatusUnavailable when no checkout or external service is available; that
// outcome must not be interpreted as structural invalidity of the reference.
type Resolver interface {
	Resolve(context.Context, Reference) (Resolution, error)
}

const QueryAPIVersion = "change-saga.ai/v1"

// QueryRequest is the transport-neutral request made against the referenced
// Saga's versioned query API. Revision always carries the pinned Git OID;
// TrackingBranch is refresh metadata only.
type QueryRequest struct {
	Schema         string
	SagaPath       string
	SagaID         string
	Revision       string
	TargetURN      string
	TrackingBranch string
}

// QueryResult is the minimum projection a resolver needs from the other
// Saga's query API.
type QueryResult struct {
	Status          ResolutionStatus
	Target          *Target
	CurrentRevision string
	Detail          string
}

// VersionedQueryAPI is the only Saga-content boundary used by checkout-aware
// resolvers. An implementation may execute a local CLI or call another
// transport, but must query Schema and must not read .saga metadata directly.
// A checkout locator belongs outside this package and supplies an
// implementation only when the pinned repository revision is available.
type VersionedQueryAPI interface {
	ResolveTarget(context.Context, QueryRequest) (QueryResult, error)
}

// NewQueryRequest converts a validated reference to a versioned query request.
func NewQueryRequest(reference Reference) (QueryRequest, error) {
	if err := Validate(reference); err != nil {
		return QueryRequest{}, err
	}
	return QueryRequest{
		Schema:         QueryAPIVersion,
		SagaPath:       reference.SagaPath,
		SagaID:         reference.SagaID,
		Revision:       reference.Revision,
		TargetURN:      reference.TargetURN,
		TrackingBranch: reference.TrackingBranch,
	}, nil
}
