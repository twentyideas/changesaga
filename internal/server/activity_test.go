package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/reviewstore"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestReviewActivityShowsDecisionHistoryAndResolvedConversations(t *testing.T) {
	root := validServerSaga(t)
	if err := reviewstore.AddReview(root, root, "open", "First **review round**"); err != nil {
		t.Fatal(err)
	}
	if err := reviewstore.AddReview(root, root, "approved", "Approved after the follow-up"); err != nil {
		t.Fatal(err)
	}
	target := saga.FragmentTarget("test", "overview")
	threadID, err := reviewstore.AddThread(root, target, "Blocking finding", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reviewstore.AddReply(root, threadID, "Accepted and fixed", nil); err != nil {
		t.Fatal(err)
	}
	if err := reviewstore.SetState(root, threadID, "resolved"); err != nil {
		t.Fatal(err)
	}

	application := &app{root: root, sourceDir: root, template: serverTemplate(t)}
	activity := httptest.NewRecorder()
	application.reviewActivity(activity, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	if activity.Code != http.StatusOK {
		t.Fatalf("activity status = %d: %s", activity.Code, activity.Body.String())
	}
	page := activity.Body.String()
	for _, expected := range []string{
		`data-review-surface-response="activity"`,
		`data-activity-filter="all" aria-pressed="true">All <span>3</span>`,
		`data-activity-filter="resolved" aria-pressed="false">Resolved <span>1</span>`,
		`data-activity-filter="decision" aria-pressed="false">Decisions <span>2</span>`,
		`data-activity-state="resolved"`,
		`href="#` + domID("thread:"+threadID) + `"`,
		`<strong>review round</strong>`,
		`Approved after the follow-up`,
		`Blocking finding`,
		`Accepted and fixed`,
		`class="activity-message reply"`,
		`class="activity-message-head"`,
		`<span class="activity-event-state">resolved</span>`,
		`<strong>Overview</strong>`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("activity is missing %q:\n%s", expected, page)
		}
	}
	if got := strings.Count(page, `class="activity-card `); got != 3 {
		t.Fatalf("activity cards = %d, want two decisions and one thread", got)
	}

	rootPage := httptest.NewRecorder()
	application.page(rootPage, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootPage.Code != http.StatusOK {
		t.Fatalf("root status = %d: %s", rootPage.Code, rootPage.Body.String())
	}
	shell := rootPage.Body.String()
	if !strings.Contains(shell, `data-open-activity`) || !strings.Contains(shell, `<span class="view-tab-count">3</span>`) {
		t.Fatalf("root shell did not advertise three activity records:\n%s", shell)
	}
	if strings.Contains(shell, `data-view-tab="activity"`) || strings.Contains(shell, `id="view-activity"`) {
		t.Fatal("review activity still replaces the saga instead of opening in the drawer")
	}
	if strings.Contains(shell, "Blocking finding") || strings.Contains(shell, "Accepted and fixed") {
		t.Fatal("root shell eagerly included review conversation bodies")
	}

	scoped := httptest.NewRecorder()
	scopedURL := "/api/activity?target=" + url.QueryEscape(target)
	application.reviewActivity(scoped, httptest.NewRequest(http.MethodGet, scopedURL, nil))
	if scoped.Code != http.StatusOK {
		t.Fatalf("scoped activity status = %d: %s", scoped.Code, scoped.Body.String())
	}
	scopedPage := scoped.Body.String()
	if !strings.Contains(scopedPage, `<h1>Activity for Overview</h1>`) ||
		!strings.Contains(scopedPage, `data-activity-filter="all" aria-pressed="true">All <span>1</span>`) ||
		strings.Contains(scopedPage, "Approved after the follow-up") {
		t.Fatalf("scoped activity did not isolate the requested target:\n%s", scopedPage)
	}

	unknown := httptest.NewRecorder()
	application.reviewActivity(unknown, httptest.NewRequest(http.MethodGet, "/api/activity?target=missing", nil))
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown activity target status = %d, want 400", unknown.Code)
	}
}

func TestReviewActivityBrowserContractIncludesFilteringAndHistoryNavigation(t *testing.T) {
	for _, contract := range []string{
		`data-open-activity data-activity-href="/api/activity"`,
		`data-drawer-mode=activity`,
		`.diff-drawer[data-drawer-mode=activity]{width:min(620px,92vw)}`,
		`.activity-message.reply{margin-left:18px`,
		`data-activity-filter="open"`,
		`data-activity-filter-empty`,
	} {
		if !strings.Contains(pageTemplate, contract) {
			t.Errorf("activity template is missing %q", contract)
		}
	}
	for _, contract := range []string{
		`function filterActivity(`,
		`item.dataset.activityState === selected`,
		`function openActivityDrawer(`,
		`surface.dataset.reviewSurface = 'activity'`,
		`destination.searchParams.has('activity')`,
	} {
		if !strings.Contains(appJavaScript, contract) {
			t.Errorf("activity browser behavior is missing %q", contract)
		}
	}
}
