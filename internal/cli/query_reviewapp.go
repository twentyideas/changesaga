package cli

import (
	"context"

	"github.com/twentyideas/changesaga/internal/livingapp"
	"github.com/twentyideas/changesaga/internal/reviewapp"
)

type reviewAppQuerySession struct {
	session       reviewapp.Session
	livingSession livingapp.Session
}

func openReviewAppSession(ctx context.Context, options queryOpenOptions) (querySession, error) {
	if isLivingQueryOperation(options.Operation) {
		// The existing review application owns the public saga/source snapshot.
		// Reusing it keeps cursors and envelopes comparable across old and living
		// query operations while livingapp remains a transport-neutral composer.
		reviewSession, err := reviewapp.Open(ctx, reviewapp.OpenOptions{SagaRoot: options.SagaRoot, SourceDir: options.SourceDir, SummaryOnly: true})
		if err != nil {
			return nil, err
		}
		session, err := livingapp.Open(ctx, livingapp.OpenOptions{SagaRoot: options.SagaRoot, Snapshot: reviewSession.Snapshot()})
		if err != nil {
			return nil, err
		}
		return &reviewAppQuerySession{session: reviewSession, livingSession: session}, nil
	}
	session, err := reviewapp.Open(ctx, reviewapp.OpenOptions{SagaRoot: options.SagaRoot, SourceDir: options.SourceDir, SummaryOnly: options.SummaryOnly})
	if err != nil {
		return nil, err
	}
	return &reviewAppQuerySession{session: session}, nil
}

func (s *reviewAppQuerySession) Snapshot() string {
	if s.livingSession != nil {
		return s.livingSession.Snapshot()
	}
	return s.session.Snapshot()
}

func (s *reviewAppQuerySession) Overview(ctx context.Context, _ overviewQuery) (any, error) {
	return s.session.Overview(ctx, reviewapp.OverviewQuery{})
}

func (s *reviewAppQuerySession) Children(ctx context.Context, query childrenQuery) (queryPage, error) {
	value, err := s.session.Children(ctx, reviewapp.ChildrenQuery{Parent: query.Parent, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) ReadFragment(ctx context.Context, query fragmentQuery) (any, error) {
	return s.session.ReadFragment(ctx, reviewapp.FragmentQuery{Target: query.Target, Offset: query.Offset, Limit: query.Limit})
}

func (s *reviewAppQuerySession) FragmentDiffs(ctx context.Context, query fragmentDiffQuery) (queryPage, error) {
	value, err := s.session.FragmentDiffs(ctx, reviewapp.FragmentDiffQuery{Target: query.Target, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) DiffOwners(ctx context.Context, query diffOwnerQuery) (queryPage, error) {
	value, err := s.session.DiffOwners(ctx, reviewapp.DiffOwnerQuery{Diff: query.Diff, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) Reviews(ctx context.Context, query reviewQuery) (queryPage, error) {
	value, err := s.session.Reviews(ctx, reviewapp.ReviewQuery{Target: query.Target, Thread: query.Thread, State: query.State, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) Gaps(ctx context.Context, query gapQuery) (queryPage, error) {
	value, err := s.session.Gaps(ctx, reviewapp.GapQuery{Kind: query.Kind, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) Mappings(ctx context.Context, query mappingQuery) (queryPage, error) {
	value, err := s.session.Mappings(ctx, reviewapp.MappingQuery{Target: query.Target, Sort: query.Sort, MinimumScore: query.MinimumScore, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) Claims(ctx context.Context, query claimQuery) (queryPage, error) {
	value, err := s.session.Claims(ctx, reviewapp.ClaimQuery{Target: query.Target, Status: query.Status, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) Verifications(ctx context.Context, query verificationQuery) (queryPage, error) {
	value, err := s.session.Verifications(ctx, reviewapp.VerificationQuery{Claim: query.Claim, Status: query.Status, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) Living(ctx context.Context, query livingQuery) (queryPage, error) {
	value, err := s.livingSession.Query(ctx, livingapp.Query{Operation: query.Operation, Filters: query.Filters, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value.Data, Page: queryPageFromLiving(value.Page)}, err
}

func queryPageFromApplication(page reviewapp.Page) queryPageEnvelope {
	return queryPageEnvelope{Total: page.Total, Returned: page.Returned, HasMore: page.HasMore, NextCursor: page.NextCursor}
}

func queryPageFromLiving(page livingapp.Page) queryPageEnvelope {
	return queryPageEnvelope{Total: page.Total, Returned: page.Returned, HasMore: page.HasMore, NextCursor: page.NextCursor}
}

func isLivingQueryOperation(operation string) bool {
	switch operation {
	case "requirements", "requirement-history", "citations", "relations", "waves", "work-items", "work-events", "work-conflicts", "traceability", "readiness":
		return true
	default:
		return false
	}
}
