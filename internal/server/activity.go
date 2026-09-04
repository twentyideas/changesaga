package server

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/gitattribution"
	"github.com/twentyideas/changesaga/internal/saga"
)

type reviewActivityView struct {
	Items      []*reviewActivityItem
	Total      int
	Threads    int
	Open       int
	Resolved   int
	Decisions  int
	ScopeTitle string
	ScopeKind  string
}

type reviewActivityItem struct {
	ID                string
	DOMID             string
	Kind              string
	KindLabel         string
	Filter            string
	State             string
	StateLabel        string
	Target            string
	TargetTitle       string
	TargetContext     string
	TargetKind        string
	Href              string
	Author            string
	AttributionDetail string
	ReviewerKind      string
	ReviewerLabel     string
	ReviewerName      string
	ReviewerAgent     string
	ReviewerModel     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Body              template.HTML
	Thread            *threadView
	Slide             *SlideReferenceView
}

type reviewActivityTarget struct {
	Title   string
	Context string
	Kind    string
	Owner   string
	Slide   *SlideReferenceView
}

// reviewActivity renders the append-only review exchange without opening the
// source comparison or authored fragment bodies. The structural outline names
// every target; LoadReviewState supplies only decisions, threads, and messages.
func (a *app) reviewActivity(w http.ResponseWriter, r *http.Request) {
	document, err := a.reviewActivityDocument(r.Context())
	if err != nil {
		http.Error(w, "Review activity could not be loaded.", http.StatusInternalServerError)
		return
	}
	view := makeReviewActivityView(document, r.URL.Query().Get("target"))
	if r.URL.Query().Get("target") != "" && view.ScopeTitle == "" {
		http.Error(w, "unknown review target", http.StatusBadRequest)
		return
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	if err := a.template.ExecuteTemplate(w, "activity-view", view); err != nil {
		http.Error(w, "Review activity could not be rendered.", http.StatusInternalServerError)
	}
}

func (a *app) reviewActivityDocument(ctx context.Context) (*saga.Saga, error) {
	outline := a.outlineDocument(ctx)
	if outline == nil {
		return nil, fmt.Errorf("saga outline is unavailable")
	}
	index := saga.MutationIndexFromDocument(outline)
	loaded, validation, _, err := a.loadReviewStateWithStableFingerprint(ctx, index)
	if err != nil {
		return nil, err
	}
	if !validation.Valid {
		return nil, fmt.Errorf("review state is invalid")
	}
	document := composeReviewDocument(outline, reviewState{
		threads: loaded.Threads, diffReviews: loaded.DiffReviews, byTarget: loaded.ByTarget,
	})
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	return document, nil
}

func makeReviewActivityView(document *saga.Saga, scope string) *reviewActivityView {
	view := &reviewActivityView{}
	if document == nil || document.Section == nil {
		return view
	}
	targets := reviewActivityTargets(document.Section)
	if place, ok := targets[scope]; scope != "" && ok {
		view.ScopeTitle, view.ScopeKind = place.Title, place.Kind
	}
	inScope := func(target string) bool {
		if scope == "" || target == scope {
			return true
		}
		return targets[target].Owner == scope
	}
	appendReviews := func(target string, reviews []saga.Review) {
		if !inScope(target) {
			return
		}
		for _, review := range reviews {
			place := targets[target]
			state, label := reviewActivityDecisionState(review.State)
			item := &reviewActivityItem{
				ID: review.ID, DOMID: domID("activity:" + target + ":" + review.ID),
				Kind: "decision", KindLabel: "Review decision", Filter: "decision",
				State: state, StateLabel: label, Target: target,
				TargetTitle: place.Title, TargetContext: place.Context, TargetKind: place.Kind,
				Href: sagaHref(target), Author: review.Author, AttributionDetail: review.AttributionDetail,
				CreatedAt: review.CreatedAt, UpdatedAt: review.CreatedAt,
				Slide: place.Slide,
			}
			item.ReviewerKind, item.ReviewerLabel = "unspecified", "Unspecified reviewer"
			if review.Reviewer != nil {
				item.ReviewerKind, item.ReviewerName, item.ReviewerAgent, item.ReviewerModel = review.Reviewer.Kind, review.Reviewer.Name, review.Reviewer.Agent, review.Reviewer.Model
				if review.Reviewer.Kind == "human" {
					item.ReviewerLabel = "Human"
				} else {
					item.ReviewerLabel = "AI"
				}
			}
			if item.Author == "" {
				item.Author = "Unattributed reviewer"
			}
			item.Author += " · " + item.ReviewerLabel
			if item.ReviewerKind == "ai" {
				item.Author += " · " + item.ReviewerName + " · " + item.ReviewerAgent + " · " + item.ReviewerModel
			}
			if strings.TrimSpace(review.Body) != "" {
				item.Body = markdownWithAnchors(review.Body, item.DOMID)
			}
			view.Items = append(view.Items, item)
			view.Decisions++
		}
	}
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		appendReviews(section.Target, section.Reviews)
		for _, fragment := range section.Fragments {
			appendReviews(fragment.Target, fragment.Reviews)
			for landmarkIndex := range fragment.Landmarks {
				landmark := &fragment.Landmarks[landmarkIndex]
				appendReviews(landmark.Target, landmark.Reviews)
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)

	for _, thread := range document.Threads {
		if !inScope(thread.Target) {
			continue
		}
		place := targets[thread.Target]
		state := thread.State
		if state == "" {
			state = "open"
		}
		updated := thread.CreatedAt
		for _, message := range thread.Messages {
			if message.CreatedAt.After(updated) {
				updated = message.CreatedAt
			}
		}
		for _, event := range thread.Events {
			if event.CreatedAt.After(updated) {
				updated = event.CreatedAt
			}
		}
		item := &reviewActivityItem{
			ID: thread.ID, DOMID: domID("activity:thread:" + thread.ID),
			Kind: "thread", KindLabel: reviewActivityThreadKind(thread.Kind), Filter: state,
			State: state, StateLabel: reviewActivityThreadState(state), Target: thread.Target,
			TargetTitle: place.Title, TargetContext: place.Context, TargetKind: place.Kind,
			Href: reviewActivityThreadHref(thread), Author: thread.CreatedBy,
			AttributionDetail: thread.AttributionDetail, CreatedAt: thread.CreatedAt, UpdatedAt: updated,
			Thread: makeThreadView(thread), Slide: place.Slide,
		}
		view.Items = append(view.Items, item)
		view.Threads++
		switch state {
		case "open":
			view.Open++
		case "resolved":
			view.Resolved++
		}
	}

	sort.SliceStable(view.Items, func(i, j int) bool {
		if view.Items[i].UpdatedAt.Equal(view.Items[j].UpdatedAt) {
			return view.Items[i].ID > view.Items[j].ID
		}
		return view.Items[i].UpdatedAt.After(view.Items[j].UpdatedAt)
	})
	view.Total = len(view.Items)
	return view
}

func reviewActivityCount(document *saga.Saga) int {
	if document == nil || document.Section == nil {
		return 0
	}
	total := len(document.Threads)
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		total += len(section.Reviews)
		for _, fragment := range section.Fragments {
			total += len(fragment.Reviews)
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return total
}

func reviewActivityTargets(root *saga.Section) map[string]reviewActivityTarget {
	result := map[string]reviewActivityTarget{}
	var walk func(*saga.Section, string, string)
	walk = func(section *saga.Section, chapter, parent string) {
		kind := "Section"
		if section.Kind == "saga" {
			kind = "Saga"
		}
		if section.Kind == "deck" {
			kind = "Deck"
		}
		if section.Kind == "chapter" {
			kind, chapter = "Chapter", section.Title
		}
		context := chapter
		if context == section.Title {
			context = ""
		}
		result[section.Target] = reviewActivityTarget{Title: activityTitle(section.Title, section.ID), Context: context, Kind: kind}
		childParent := activityTitle(section.Title, parent)
		for _, fragment := range section.Fragments {
			fragmentContext := chapter
			if section.Kind != "chapter" && section.Kind != "saga" {
				fragmentContext = childParent
			}
			fragmentKind := "Explanation"
			if fragment.SlideMeta != nil {
				fragmentKind = "Slide"
			}
			var slide *SlideReferenceView
			if fragment.SlideMeta != nil {
				fragmentHref := sagaHref(fragment.Target)
				state, _, _, _ := latestReview(fragment.Reviews)
				slide = &SlideReferenceView{
					ID: fragment.ID, Title: activityTitle(fragment.Title, fragment.ID), Target: fragment.Target,
					Anchor: strings.TrimPrefix(fragmentHref, "#"), Href: fragmentHref,
					URL: fragmentAssetURL(fragment), MediaType: fragment.MediaType, ReviewState: state,
				}
			}
			result[fragment.Target] = reviewActivityTarget{Title: activityTitle(fragment.Title, fragment.ID), Context: fragmentContext, Kind: fragmentKind, Slide: slide}
			for _, landmark := range fragment.Landmarks {
				landmarkKind := "Marked place"
				if landmark.ItemMeta != nil {
					landmarkKind = "Item"
				}
				result[landmark.Target] = reviewActivityTarget{Title: activityTitle(landmark.Label, landmark.ID), Context: activityTitle(fragment.Title, fragment.ID), Kind: landmarkKind, Owner: fragment.Target, Slide: slide}
			}
		}
		for _, child := range section.Children {
			walk(child, chapter, childParent)
		}
	}
	walk(root, "", "")
	return result
}

func activityTitle(title, fallback string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "Review target"
}

func reviewActivityDecisionState(state string) (string, string) {
	switch state {
	case "approved":
		return state, "Approved"
	case "rejected":
		return state, "Changes requested"
	case "closed":
		return state, "Closed"
	default:
		return "open", "Review open"
	}
}

func reviewActivityThreadKind(kind string) string {
	if kind == "suggestion" {
		return "Suggestion thread"
	}
	return "Comment thread"
}

func reviewActivityThreadState(state string) string {
	switch state {
	case "resolved":
		return "Resolved"
	case "withdrawn":
		return "Withdrawn"
	default:
		return "Open"
	}
}

func reviewActivityThreadHref(thread *saga.Thread) string {
	anchor := domID("thread:" + thread.ID)
	if thread.Anchor.Type == "diff" && thread.Anchor.Diff != nil && thread.Anchor.Diff.URI != "" {
		return "/?view=code&diff=" + url.QueryEscape(thread.Anchor.Diff.URI) + "#" + anchor
	}
	return "#" + anchor
}
