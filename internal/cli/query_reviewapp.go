package cli

import (
	"context"

	"github.com/change-saga/change-saga/internal/reviewapp"
)

type reviewAppQuerySession struct {
	session reviewapp.Session
}

func openReviewAppSession(ctx context.Context, options queryOpenOptions) (querySession, error) {
	session, err := reviewapp.Open(ctx, reviewapp.OpenOptions{SagaRoot: options.SagaRoot, SourceDir: options.SourceDir})
	if err != nil {
		return nil, err
	}
	return &reviewAppQuerySession{session: session}, nil
}

func (s *reviewAppQuerySession) Snapshot() string { return s.session.Snapshot() }

func (s *reviewAppQuerySession) Overview(ctx context.Context, _ overviewQuery) (any, error) {
	return s.session.Overview(ctx, reviewapp.OverviewQuery{})
}

func (s *reviewAppQuerySession) Children(ctx context.Context, query childrenQuery) (queryPage, error) {
	value, err := s.session.Children(ctx, reviewapp.ChildrenQuery{Parent: query.Parent, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, NextCursor: value.Page.NextCursor}, err
}

func (s *reviewAppQuerySession) ReadFragment(ctx context.Context, query fragmentQuery) (any, error) {
	return s.session.ReadFragment(ctx, reviewapp.FragmentQuery{Target: query.Target, Offset: query.Offset, Limit: query.Limit})
}

func (s *reviewAppQuerySession) FragmentDiffs(ctx context.Context, query fragmentDiffQuery) (queryPage, error) {
	value, err := s.session.FragmentDiffs(ctx, reviewapp.FragmentDiffQuery{Target: query.Target, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, NextCursor: value.Page.NextCursor}, err
}

func (s *reviewAppQuerySession) DiffOwners(ctx context.Context, query diffOwnerQuery) (queryPage, error) {
	value, err := s.session.DiffOwners(ctx, reviewapp.DiffOwnerQuery{Diff: query.Diff, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, NextCursor: value.Page.NextCursor}, err
}

func (s *reviewAppQuerySession) Reviews(ctx context.Context, query reviewQuery) (queryPage, error) {
	value, err := s.session.Reviews(ctx, reviewapp.ReviewQuery{Target: query.Target, Thread: query.Thread, State: query.State, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, NextCursor: value.Page.NextCursor}, err
}

func (s *reviewAppQuerySession) Gaps(ctx context.Context, query gapQuery) (queryPage, error) {
	value, err := s.session.Gaps(ctx, reviewapp.GapQuery{Kind: query.Kind, Cursor: query.Cursor, Limit: query.Limit})
	return queryPage{Data: value, NextCursor: value.Page.NextCursor}, err
}
