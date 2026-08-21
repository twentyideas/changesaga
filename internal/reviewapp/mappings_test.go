package reviewapp

import "testing"

func TestMappingReasonsExposeBreadthWithoutCallingItCorrectness(t *testing.T) {
	item := MappingAssessment{
		TargetKind: "fragment", Notes: []string{"implementation"}, SelectorCount: 8,
		StaleSelectorCount: 1, AtomCount: 486, FileCount: 6, TargetAtomCount: 486,
		TargetFileCount: 6, ChangesetShare: .354,
	}
	reasons := mappingReasons(item)
	wanted := map[string]bool{
		"thin_note": true, "generic_note": true, "stale_selector": true, "very_broad_record": true,
		"multi_file_record": true, "broad_target": true, "many_files_per_target": true,
		"dominant_target": true, "prefer_landmarks": true,
	}
	weight := 0
	for _, reason := range reasons {
		delete(wanted, reason.Code)
		weight += reason.Weight
		if reason.Code == "correct" || reason.Code == "incorrect" {
			t.Fatalf("mapping breadth became a correctness verdict: %#v", reasons)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing scrutiny reasons %v from %#v", wanted, reasons)
	}
	if weight <= 100 {
		t.Fatalf("fixture should exercise score capping, raw weight=%d", weight)
	}
}

func TestFocusedMappingHasNoManufacturedWarning(t *testing.T) {
	item := MappingAssessment{
		TargetKind: "landmark", Notes: []string{"Prevents a second sampler from starting while the first remains active."},
		SelectorCount: 1, AtomCount: 8, FileCount: 1, TargetAtomCount: 8, TargetFileCount: 1, ChangesetShare: .01,
	}
	if reasons := mappingReasons(item); len(reasons) != 0 {
		t.Fatalf("focused mapping reasons = %#v", reasons)
	}
}
