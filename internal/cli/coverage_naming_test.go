package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

func TestGeneratedCoverageNameIsSelectorIdentityNotAuthoringEvent(t *testing.T) {
	record := coverRecord{Path: "internal/service/handler.go", Note: "first explanation"}
	file := saga.DiffFile{Version: saga.CurrentVersion, Diffs: []saga.DiffReference{
		{URI: "saga-diff://v1/line?base=base&end=40&head=head&path=internal%2Fservice%2Fhandler.go&repository=https%3A%2F%2Fexample.test%2Facme.git&side=new&start=10", Note: record.Note},
	}}
	first := stableGeneratedCoverageName(record, file)

	record.Note = "a conflicting explanation from another branch"
	file.Diffs[0].Note = record.Note
	second := stableGeneratedCoverageName(record, file)
	if first != second {
		t.Fatalf("notes changed the logical evidence path: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, "internal-service-handler-go-") || len(first) != len("internal-service-handler-go-")+16 {
		t.Fatalf("unexpected stable evidence name %q", first)
	}
}

func TestGeneratedCoverageNameSeparatesUnrelatedSelectors(t *testing.T) {
	record := coverRecord{Path: "internal/service/handler.go"}
	file := saga.DiffFile{Version: saga.CurrentVersion, Diffs: []saga.DiffReference{{URI: "selector-one"}}}
	first := stableGeneratedCoverageName(record, file)
	file.Diffs[0].URI = "selector-two"
	second := stableGeneratedCoverageName(record, file)
	if first == second {
		t.Fatalf("unrelated selectors shared generated path %q", first)
	}
}

func TestGeneratedCoverageNameIgnoresSelectorDeliveryOrder(t *testing.T) {
	record := coverRecord{Path: "internal/service/handler.go"}
	first := saga.DiffFile{Version: saga.CurrentVersion, Diffs: []saga.DiffReference{{URI: "selector-one"}, {URI: "selector-two"}}}
	second := saga.DiffFile{Version: saga.CurrentVersion, Diffs: []saga.DiffReference{{URI: "selector-two"}, {URI: "selector-one"}}}
	if left, right := stableGeneratedCoverageName(record, first), stableGeneratedCoverageName(record, second); left != right {
		t.Fatalf("delivery order changed logical evidence path: %q != %q", left, right)
	}
}

func TestGeneratedCoveragePathsConflictOnlyForTheSameSelectorIdentity(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		leftLine     string
		rightLine    string
		wantConflict bool
	}{
		{name: "unrelated ranges merge cleanly", leftLine: "1", rightLine: "3"},
		{name: "different explanations of one range conflict", leftLine: "1", rightLine: "1", wantConflict: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root, source := coveredSaga(t)
			git(t, root, "init", "-b", "main")
			git(t, root, "config", "user.name", "Saga Author")
			git(t, root, "config", "user.email", "saga@example.test")
			git(t, root, "add", ".")
			git(t, root, "-c", "commit.gpgSign=false", "commit", "-m", "base saga")

			coverBranch := func(branch, line, note string) {
				t.Helper()
				git(t, root, "checkout", "-b", branch)
				if _, err := runCover(t, "", "--repo", source, "--path", "internal/service/handler.go", "--side", "new", "--lines", line, "--note", note, root); err != nil {
					t.Fatalf("cover %s: %v", branch, err)
				}
				git(t, root, "add", ".")
				git(t, root, "-c", "commit.gpgSign=false", "commit", "-m", branch+" evidence")
			}

			coverBranch("left", testCase.leftLine, "left explanation")
			git(t, root, "checkout", "main")
			coverBranch("right", testCase.rightLine, "right explanation")

			command := exec.Command("git", "merge", "--no-edit", "left")
			command.Dir = root
			output, err := command.CombinedOutput()
			if !testCase.wantConflict {
				if err != nil {
					t.Fatalf("unrelated evidence conflicted: %v\n%s", err, output)
				}
				if names := diffRecords(t, filepath.Join(root, "___diffs")); len(names) != 2 {
					t.Fatalf("unrelated evidence did not survive independently: %v", names)
				}
				return
			}
			if err == nil {
				t.Fatalf("different explanations of the same selector merged silently:\n%s", output)
			}
			status := git(t, root, "status", "--porcelain")
			if !strings.Contains(status, "AA ___diffs/") {
				t.Fatalf("merge failed for the wrong reason; status:\n%s\nmerge:\n%s", status, output)
			}
		})
	}
}
