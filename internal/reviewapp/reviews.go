package reviewapp

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitattribution"
	"github.com/twentyideas/changesaga/internal/saga"
)

func (s *session) indexReviewItems(ctx context.Context) {
	resolver := gitattribution.New(ctx, s.document.Root)
	for _, stored := range s.document.Threads {
		thread := s.normalizeThread(ctx, resolver, stored)
		s.threads[thread.ID] = thread
		copy := thread
		s.reviewItems = append(s.reviewItems, ReviewItem{Kind: "thread", Thread: &copy})
		if thread.Anchor.Type == "diff" && thread.Anchor.Diff != nil {
			selector, err := diffuri.Parse(thread.Anchor.Diff.URI)
			if err == nil {
				for _, atom := range s.changes.Atoms {
					atomReference, atomErr := diffuri.Parse(atom.URI)
					if atomErr == nil && diffuri.Matches(selector, atomReference) {
						s.threadsByDiff[atom.URI] = append(s.threadsByDiff[atom.URI], thread)
					}
				}
			}
		}
	}
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		for _, review := range section.Reviews {
			event := normalizeTargetReview(ctx, resolver, section.Target, review)
			s.reviewItems = append(s.reviewItems, ReviewItem{Kind: "target_review", Event: &event})
		}
		for _, fragment := range section.Fragments {
			for _, review := range fragment.Reviews {
				event := normalizeTargetReview(ctx, resolver, fragment.Target, review)
				s.reviewItems = append(s.reviewItems, ReviewItem{Kind: "target_review", Event: &event})
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(s.document.Section)
	for _, review := range s.document.DiffReviews {
		event := ReviewEvent{
			ID: review.ID, Kind: "file_review", Diff: review.URI, State: review.State, CreatedAt: review.CreatedAt,
			Attribution: attribution(ctx, resolver, review.Path), LegacyClaimedAuthor: review.Author,
		}
		s.reviewItems = append(s.reviewItems, ReviewItem{Kind: "file_review", Event: &event})
	}
	sortReviewItems(s.reviewItems)
}

func (s *session) normalizeThread(ctx context.Context, resolver *gitattribution.Resolver, stored *saga.Thread) ReviewThread {
	thread := ReviewThread{
		ID: stored.ID, Kind: stored.Kind, Target: stored.Target, Anchor: stored.Anchor, Suggestion: stored.Suggestion,
		State: stored.State, CreatedAt: stored.CreatedAt, Messages: []ReviewMessage{}, Events: []ReviewEvent{},
		Attribution: attribution(ctx, resolver, filepath.Join(stored.Directory, "thread.json")), LegacyClaimedAuthor: stored.CreatedBy,
	}
	if thread.Kind == "" {
		thread.Kind = "comment"
	}
	for _, storedMessage := range stored.Messages {
		message := ReviewMessage{
			ID: storedMessage.ID, CreatedAt: storedMessage.CreatedAt,
			Attribution: attribution(ctx, resolver, storedMessage.Path), LegacyClaimedAuthor: storedMessage.Author, Fragments: []ReviewFragment{},
		}
		for _, fragment := range storedMessage.Fragments {
			message.Fragments = append(message.Fragments, normalizeReviewFragment(fragment))
		}
		thread.Messages = append(thread.Messages, message)
	}
	for _, storedEvent := range stored.Events {
		kind := "thread_state"
		if storedEvent.Anchor != nil && storedEvent.State == "" {
			kind = "thread_anchor"
		} else if storedEvent.Anchor != nil {
			kind = "thread_update"
		}
		thread.Events = append(thread.Events, ReviewEvent{
			ID: storedEvent.ID, Kind: kind, Target: stored.Target, State: storedEvent.State, Anchor: storedEvent.Anchor, CreatedAt: storedEvent.CreatedAt,
			Attribution: attribution(ctx, resolver, storedEvent.Path), LegacyClaimedAuthor: storedEvent.Author,
		})
	}
	return thread
}

func normalizeTargetReview(ctx context.Context, resolver *gitattribution.Resolver, target string, review saga.Review) ReviewEvent {
	return ReviewEvent{
		ID: review.ID, Kind: "target_review", Target: target, State: review.State, Body: review.Body, CreatedAt: review.CreatedAt,
		Attribution: attribution(ctx, resolver, review.Path), LegacyClaimedAuthor: review.Author,
	}
}

func normalizeReviewFragment(fragment *saga.Fragment) ReviewFragment {
	result := ReviewFragment{Target: fragment.Target, ID: fragment.ID, Title: fragment.Title, MediaType: fragment.MediaType}
	if result.Title == "" {
		result.Title = fragment.ID
	}
	data, err := readContainedFile(fragment.Directory, fragment.Entrypoint)
	if err != nil {
		return result
	}
	result.Bytes = int64(len(data))
	if len(data) > DefaultFragmentLimit {
		data = data[:DefaultFragmentLimit]
		result.Truncated = true
	}
	result.Encoding = "base64"
	result.Data = base64.StdEncoding.EncodeToString(data)
	if strings.HasPrefix(fragment.MediaType, "text/") {
		for len(data) > 0 && !utf8.Valid(data) && result.Truncated {
			data = data[:len(data)-1]
		}
	}
	if strings.HasPrefix(fragment.MediaType, "text/") && utf8.Valid(data) {
		result.Encoding = "utf-8"
		result.Data = string(data)
	}
	return result
}

func attribution(ctx context.Context, resolver *gitattribution.Resolver, path string) Attribution {
	value := resolver.Resolve(ctx, path)
	switch value.State {
	case gitattribution.Committed:
		stamp := value.CommittedAt
		return Attribution{Status: "committed", Commit: value.CommitID, Committer: &Committer{Name: value.Name, Email: value.Email}, CommittedAt: &stamp}
	case gitattribution.Uncommitted:
		return Attribution{Status: "uncommitted"}
	default:
		return Attribution{Status: "history_unavailable"}
	}
}

func sortReviewItems(items []ReviewItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := recordTime(items[i]), recordTime(items[j])
		if !left.Equal(right) {
			return left.Before(right)
		}
		if recordID(items[i]) != recordID(items[j]) {
			return recordID(items[i]) < recordID(items[j])
		}
		return items[i].Kind < items[j].Kind
	})
}
