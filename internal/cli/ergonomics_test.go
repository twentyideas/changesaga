package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

// landmarkSaga returns a saga with one Markdown fragment carrying a heading
// landmark, plus the repository its comparison reads.
func landmarkSaga(t *testing.T) (root, repo string) {
	t.Helper()
	root, repo = coveredSaga(t)
	var output bytes.Buffer
	if err := AddChapter(context.Background(), []string{"--title", "Service", root, "service"}, &output); err != nil {
		t.Fatal(err)
	}
	fragment := filepath.Join(root, "service.chapter", "overview.fragment")
	writeFile(t, filepath.Join(fragment, "content.md"), "# Service {#service-intro}\n\nProse.\n\n## Submit action {#submit-action}\n\nMore.\n")
	writeFile(t, filepath.Join(fragment, "___landmarks", "submit-action.landmark", "landmark.json"),
		`{"version":2,"id":"submit-action","label":"Submit action","selector":{"type":"heading","heading_id":"submit-action"}}`+"\n")
	assertValid(t, root)
	return root, repo
}

// A landmark lives inside a reserved directory that ordinary path resolution
// refuses to enter, so the shorthand is the only ergonomic way to name one
// without first knowing its full URN.
func TestCoverResolvesLandmarkShorthand(t *testing.T) {
	root, repo := landmarkSaga(t)
	if output, err := runCover(t, "", "--repo", repo,
		"--target", "service.chapter/overview.fragment#submit-action",
		"--path", "internal/service/handler.go", "--side", "new", "--lines", "3", "--name", "submit", root); err != nil {
		t.Fatalf("landmark shorthand: %v\n%s", err, output)
	}
	recorded := filepath.Join(root, "service.chapter", "overview.fragment", "___landmarks", "submit-action.landmark", "___diffs", "submit.json")
	if _, err := os.Stat(recorded); err != nil {
		t.Fatalf("evidence was not attached to the landmark: %v", err)
	}
	assertValid(t, root)

	report, err := buildReport(context.Background(), root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Targets) != 1 || !strings.Contains(report.Targets[0].Target, ":landmark:submit-action") {
		t.Fatalf("coverage should belong to the landmark: %#v", report.Targets)
	}
}

func TestCoverLandmarkErrorsNameTheAvailableLandmarks(t *testing.T) {
	root, repo := landmarkSaga(t)
	_, err := runCover(t, "", "--repo", repo,
		"--target", "service.chapter/overview.fragment#no-such-landmark",
		"--path", "internal/service/handler.go", "--side", "new", "--lines", "3", root)
	if err == nil {
		t.Fatal("an unknown landmark must fail")
	}
	if !strings.Contains(err.Error(), "submit-action") {
		t.Fatalf("error %q does not list the landmarks the fragment declares", err)
	}

	// A fragment with no landmarks at all should say where to create one rather
	// than leaving the author to guess the directory layout.
	_, err = runCover(t, "", "--repo", repo,
		"--target", "overview.fragment#anything",
		"--path", "internal/service/handler.go", "--side", "new", "--lines", "3", root)
	if err == nil || !strings.Contains(err.Error(), "___landmarks/anything.landmark/landmark.json") {
		t.Fatalf("error %q does not explain how to declare the landmark", err)
	}
}

// An unresolvable target is the most common authoring mistake. The error has to
// point at the supported way to enumerate targets, not invite file spelunking.
func TestResolveTargetErrorPointsAtTheQueryAPI(t *testing.T) {
	root, _ := landmarkSaga(t)
	document, _, err := saga.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = resolveTarget(document, "service.chapter/missing", true)
	if err == nil {
		t.Fatal("expected an unresolvable target to fail")
	}
	for _, want := range []string{"change-saga query children", "Known targets:", ":landmark:submit-action"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q is missing %q", err, want)
		}
	}
}

func TestUnknownTargetURNIsAlsoExplained(t *testing.T) {
	root, _ := landmarkSaga(t)
	document, _, err := saga.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = resolveTarget(document, "urn:change-saga:batch:fragment:ghost", true)
	if err == nil || !strings.Contains(err.Error(), "change-saga query children") {
		t.Fatalf("an unknown URN must be explained too, got %v", err)
	}
}

// "change-saga open -h" used to print "Usage of serve:", naming a command the
// user did not type and whose flags differ in default.
func TestCommandHelpNamesTheInvokedCommand(t *testing.T) {
	var open bytes.Buffer
	if err := Serve(context.Background(), []string{"-h"}, &open, true); err == nil {
		t.Fatal("-h must report flag.ErrHelp so the process exits zero without serving")
	}
	if !strings.Contains(open.String(), "change-saga open ") {
		t.Fatalf("open -h did not describe open:\n%s", open.String())
	}
	if strings.Contains(open.String(), "Usage of serve") || strings.Contains(open.String(), "change-saga serve") {
		t.Fatalf("open -h still advertises serve:\n%s", open.String())
	}
	if strings.Contains(open.String(), "detach") {
		t.Fatalf("open -h exposes an internal lifecycle detail:\n%s", open.String())
	}
	if !strings.Contains(open.String(), "managed loopback reviewer") {
		t.Fatalf("open -h does not explain that the reviewer remains available:\n%s", open.String())
	}

	var serve bytes.Buffer
	if err := Serve(context.Background(), []string{"-h"}, &serve); err == nil {
		t.Fatal("-h must report flag.ErrHelp")
	}
	if !strings.Contains(serve.String(), "change-saga serve ") {
		t.Fatalf("serve -h did not describe serve:\n%s", serve.String())
	}
}

func TestOpenIsManagedByDefaultAndAcceptsLegacyDetachFlag(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		want       []string
		background bool
	}{
		{name: "plain", args: []string{"review.saga"}, want: []string{"review.saga"}, background: true},
		{name: "legacy true", args: []string{"--detach", "review.saga"}, want: []string{"review.saga"}, background: true},
		{name: "legacy false", args: []string{"--detach=false", "review.saga"}, want: []string{"review.saga"}, background: false},
		{name: "legacy after option value", args: []string{"--repo", "/tmp/source", "--detach", "review.saga"}, want: []string{"--repo", "/tmp/source", "review.saga"}, background: true},
		{name: "positional", args: []string{"--", "--detach"}, want: []string{"--", "--detach"}, background: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, background, err := normalizeLegacyOpenDetach(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\x00") != strings.Join(test.want, "\x00") || background != test.background {
				t.Fatalf("normalizeLegacyOpenDetach(%q) = %q, %t; want %q, %t", test.args, got, background, test.want, test.background)
			}
		})
	}
	if _, _, err := normalizeLegacyOpenDetach([]string{"--detach=maybe", "review.saga"}); err == nil {
		t.Fatal("invalid legacy detach value must fail")
	}
}

// A command with no flags produced a bare "Usage of install-skill:" banner and
// nothing else, which told the reader neither what it does nor how to use it.
func TestInstallSkillHelpExplainsTheCommand(t *testing.T) {
	var output bytes.Buffer
	if err := InstallSkill([]string{"-h"}, &output); err == nil {
		t.Fatal("-h must report flag.ErrHelp")
	}
	text := output.String()
	if !strings.Contains(text, "change-saga install-skill") {
		t.Fatalf("install-skill -h omitted its usage line:\n%s", text)
	}
	if !strings.Contains(text, "prompt") {
		t.Fatalf("install-skill -h did not say what it prints:\n%s", text)
	}
	if strings.Contains(text, "Flags:") {
		t.Fatalf("a flagless command must not print an empty flag section:\n%s", text)
	}
}

func TestTopLevelHelpListsEveryDispatchedCommand(t *testing.T) {
	var output bytes.Buffer
	PrintHelp(&output)
	for _, command := range commandOrder {
		usage, ok := commandUsage[command]
		if !ok {
			t.Fatalf("command %q has no usage line", command)
		}
		if !strings.Contains(output.String(), usage) {
			t.Fatalf("help omits %q:\n%s", command, output.String())
		}
	}
}

func TestTopLevelHelpRecommendsTheAuthoringSkill(t *testing.T) {
	var output bytes.Buffer
	PrintHelp(&output)
	text := output.String()
	for _, want := range []string{
		"Using a coding agent?",
		"change-saga install-skill",
		"agent-agnostic bootstrap",
		"does not modify the repository or create a Saga",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("top-level help omitted %q:\n%s", want, text)
		}
	}
}

func TestTopLevelHelpRoutesExistingAndNewWork(t *testing.T) {
	var output bytes.Buffer
	PrintHelp(&output)
	text := output.String()
	for _, want := range []string{
		"Existing implementation or PR",
		"small focused",
		"normal PR may be enough",
		"New feature or exploration",
		"overlapping surfaces, not gates",
		"waves of parallel workspaces",
		"Parallel by design",
		"acceptance-criterion coverage",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("top-level help omitted workflow guidance %q:\n%s", want, text)
		}
	}
}

func TestLivingCommandHelpExplainsParallelWorkflow(t *testing.T) {
	checks := []struct {
		name string
		run  func() string
		want []string
	}{
		{name: "init", run: func() string {
			var output bytes.Buffer
			_ = Init(context.Background(), []string{"-h"}, &output)
			return output.String()
		}, want: []string{"reviewer guide", "Small focused changes may not need a Saga"}},
		{name: "story add", run: func() string {
			var output bytes.Buffer
			_ = Story(context.Background(), []string{"add", "-h"}, &output)
			return output.String()
		}, want: []string{"acceptance-criteria", "evolve alongside"}},
		{name: "plan add-wave", run: func() string {
			var output bytes.Buffer
			_ = Plan(context.Background(), []string{"add-wave", "-h"}, &output)
			return output.String()
		}, want: []string{"parallel workspace lanes", "converge", "not dependency"}},
		{name: "plan add-item", run: func() string {
			var output bytes.Buffer
			_ = Plan(context.Background(), []string{"add-item", "-h"}, &output)
			return output.String()
		}, want: []string{"independently assignable", "parallel work"}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			text := check.run()
			for _, want := range check.want {
				if !strings.Contains(text, want) {
					t.Fatalf("help omitted %q:\n%s", want, text)
				}
			}
		})
	}
}

// The installed skill is the contract an agent follows. It has to route reads
// through the versioned query API and name every operation, or an agent will
// fall back to reading saga files whose layout is not a compatibility promise.
func TestInstallSkillRoutesAgentsThroughTheQueryAPI(t *testing.T) {
	var output bytes.Buffer
	if err := InstallSkill(nil, &output); err != nil {
		t.Fatal(err)
	}
	prompt := output.String()
	for _, operation := range queryOperations {
		if !strings.Contains(prompt, "`"+operation+"`") {
			t.Fatalf("the installed skill never names the %q operation", operation)
		}
		if !strings.Contains(prompt, queryUsage[operation]) {
			t.Fatalf("the installed skill omits the usage for %q", operation)
		}
		if strings.TrimSpace(queryPurpose[operation]) == "" {
			t.Fatalf("operation %q has no purpose description", operation)
		}
	}
	for _, want := range []string{
		"change-saga query",
		"error.code",
		"page.next_cursor",
		"--batch",
		"--dry-run",
		"<fragment-path>#<landmark-id>",
		"Never widen",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the installed skill omits %q", want)
		}
	}
}

func TestValidateFixAddsMissingHeadingAnchors(t *testing.T) {
	root, _ := coveredSaga(t)
	fragment := filepath.Join(root, "overview.fragment")
	writeFile(t, filepath.Join(fragment, "content.md"), "# Overview\n\nProse.\n\n## Risks\n")

	var before bytes.Buffer
	if err := Validate(context.Background(), []string{root}, &before); err != nil {
		t.Fatalf("a saga with unanchored headings is still valid: %v", err)
	}
	if !strings.Contains(before.String(), "should declare a stable anchor") {
		t.Fatalf("expected anchor warnings before the fix:\n%s", before.String())
	}

	var fixed bytes.Buffer
	if err := Validate(context.Background(), []string{"--fix", "--json", root}, &fixed); err != nil {
		t.Fatalf("validate --fix: %v\n%s", err, fixed.String())
	}
	var result struct {
		Valid  bool         `json:"valid"`
		Issues []saga.Issue `json:"issues"`
		Fixes  []AnchorFix  `json:"fixes"`
	}
	if err := json.Unmarshal(fixed.Bytes(), &result); err != nil {
		t.Fatalf("validate --fix --json is not one JSON value: %v\n%s", err, fixed.String())
	}
	if len(result.Fixes) != 2 {
		t.Fatalf("expected two anchors to be added: %#v", result.Fixes)
	}
	if result.Fixes[0].Path != "overview.fragment/content.md" || result.Fixes[0].Anchor != "overview" {
		t.Fatalf("unexpected fix record: %#v", result.Fixes[0])
	}
	for _, issue := range result.Issues {
		if strings.Contains(issue.Message, "stable anchor") {
			t.Fatalf("the anchor warning survived --fix: %#v", issue)
		}
	}
	content, err := os.ReadFile(filepath.Join(fragment, "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "# Overview {#overview}") || !strings.Contains(string(content), "## Risks {#risks}") {
		t.Fatalf("headings were not anchored:\n%s", content)
	}
	assertValid(t, root)
}

// --fix must be a no-op on an already-anchored saga, so it is safe to run in a
// loop or a pre-handoff check without generating churn.
func TestValidateFixIsANoOpWhenNothingIsMissing(t *testing.T) {
	root, _ := coveredSaga(t)
	writeFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Overview {#overview}\n")
	var output bytes.Buffer
	if err := Validate(context.Background(), []string{"--fix", "--json", root}, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Fixes []AnchorFix `json:"fixes"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Fixes) != 0 {
		t.Fatalf("nothing should have been fixed: %#v", result.Fixes)
	}
}

// --fix rewrites authored narrative only. Thread messages are append-only
// review history and must never be edited to tidy up their Markdown.
func TestValidateFixLeavesReviewOverlayFragmentsAlone(t *testing.T) {
	root, _ := coveredSaga(t)
	var output bytes.Buffer
	if err := Thread(context.Background(), []string{"--target", ".", "--body", "# A reviewer heading\n\nnote", root}, &output); err != nil {
		t.Fatal(err)
	}
	var messages []string
	if err := filepath.Walk(filepath.Join(root, "___review"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			messages = append(messages, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 {
		t.Fatal("the thread body was not stored as a Markdown fragment")
	}
	before := map[string][]byte{}
	for _, message := range messages {
		data, err := os.ReadFile(message)
		if err != nil {
			t.Fatal(err)
		}
		before[message] = data
	}

	output.Reset()
	if err := Validate(context.Background(), []string{"--fix", root}, &output); err != nil {
		t.Fatalf("validate --fix: %v\n%s", err, output.String())
	}
	for message, original := range before {
		current, err := os.ReadFile(message)
		if err != nil {
			t.Fatal(err)
		}
		if string(current) != string(original) {
			t.Fatalf("--fix rewrote review history %s:\n%s\n%s", message, original, current)
		}
	}
}

// The end-to-end shape matters as much as the in-memory one: an agent polls
// status --json until "uncovered" is empty, and a null there is a crash.
func TestStatusJSONReportsEmptyCollectionsOnSuccess(t *testing.T) {
	root, repo := coveredSaga(t)
	batch := `{"path":"internal/service/handler.go","side":"new","lines":"1-5","note":"the whole new file"}
{"path":"internal/service/handler.go","event":"add","note":"file added"}`
	if out, err := runCover(t, batch, "--repo", repo, "--batch", "-", root); err != nil {
		t.Fatalf("cover: %v\n%s", err, out)
	}

	var output bytes.Buffer
	if err := Status(context.Background(), []string{"--json", "--repo", repo, root}, &output); err != nil {
		t.Fatalf("status: %v\n%s", err, output.String())
	}
	var report map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("status --json is not one JSON value: %v\n%s", err, output.String())
	}
	if string(report["complete"]) != "true" {
		t.Fatalf("expected a complete saga:\n%s", output.String())
	}
	for _, field := range []string{"uncovered", "overlaps", "orphans", "saga_changes", "schema_issues"} {
		value, present := report[field]
		if !present || string(value) != "[]" {
			t.Fatalf("status --json %q = %s (present=%v), want []", field, value, present)
		}
	}
}
