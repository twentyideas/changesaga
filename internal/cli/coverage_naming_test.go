package cli

import (
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
