package cli

import (
	"context"
	"strings"

	"github.com/twentyideas/changesaga/internal/livingapp"
	"github.com/twentyideas/changesaga/internal/reviewapp"
)

type reviewAppQuerySession struct {
	session       reviewapp.Session
	livingSession livingapp.Session
	operation     string
	slideMode     bool
}

type slideOverview struct {
	Saga     reviewapp.SagaIdentity     `json:"saga"`
	Source   reviewapp.SourceSnapshot   `json:"source"`
	Root     reviewapp.Node             `json:"root"`
	Decks    []reviewapp.ChapterSummary `json:"decks"`
	Coverage reviewapp.CoverageSummary  `json:"coverage"`
}

type slideQueryContent struct {
	Target       string                       `json:"target"`
	ID           string                       `json:"id"`
	Title        string                       `json:"title"`
	Intent       string                       `json:"intent"`
	Layout       string                       `json:"layout"`
	Takeaway     string                       `json:"takeaway"`
	MediaType    string                       `json:"media_type"`
	Content      reviewapp.FragmentChunk      `json:"content"`
	Assets       []reviewapp.AssetSummary     `json:"assets"`
	Items        []reviewapp.SemanticLandmark `json:"items"`
	ReadingOrder []string                     `json:"reading_order"`
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
		return &reviewAppQuerySession{session: reviewSession, livingSession: session, operation: options.Operation, slideMode: options.SlideMode}, nil
	}
	session, err := reviewapp.Open(ctx, reviewapp.OpenOptions{SagaRoot: options.SagaRoot, SourceDir: options.SourceDir, SummaryOnly: options.SummaryOnly})
	if err != nil {
		return nil, err
	}
	return &reviewAppQuerySession{session: session, operation: options.Operation, slideMode: options.SlideMode}, nil
}

func (s *reviewAppQuerySession) Snapshot() string {
	if s.livingSession != nil {
		return s.livingSession.Snapshot()
	}
	return s.session.Snapshot()
}

func (s *reviewAppQuerySession) Overview(ctx context.Context, _ overviewQuery) (any, error) {
	value, err := s.session.Overview(ctx, reviewapp.OverviewQuery{})
	if err != nil || !s.slideMode {
		return value, err
	}
	return slideOverview{Saga: value.Saga, Source: value.Source, Root: value.Root, Decks: value.Decks, Coverage: value.Coverage}, nil
}

func (s *reviewAppQuerySession) Children(ctx context.Context, query childrenQuery) (queryPage, error) {
	value, err := s.session.Children(ctx, reviewapp.ChildrenQuery{Parent: query.Parent, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, Page: queryPageFromApplication(value.Page)}, err
}

func (s *reviewAppQuerySession) ReadFragment(ctx context.Context, query fragmentQuery) (any, error) {
	value, err := s.session.ReadFragment(ctx, reviewapp.FragmentQuery{Target: query.Target, Offset: query.Offset, Limit: query.Limit})
	if err != nil || s.operation != "slide" {
		return value, err
	}
	return slideQueryContent{Target: value.Target, ID: value.ID, Title: value.Title, Intent: value.Intent, Layout: value.Layout, Takeaway: value.Takeaway, MediaType: value.MediaType, Content: value.Content, Assets: value.Assets, Items: value.Landmarks, ReadingOrder: value.ReadingOrder}, nil
}

func (s *reviewAppQuerySession) FragmentDiffs(ctx context.Context, query fragmentDiffQuery) (queryPage, error) {
	if s.operation == "slide-diffs" && !strings.Contains(query.Target, ":item:") {
		return queryPage{}, &queryError{Code: "invalid_argument", Message: "slide-diffs target must identify an Item"}
	}
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
