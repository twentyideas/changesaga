package readiness

import "testing"

func TestEvaluateKeepsCoverageAxesIndependentAndProgressOutOfEvidence(t *testing.T) {
	projection := Evaluate([]Criterion{
		{URN: "criterion:a", Designed: true, Planned: true},
		{URN: "criterion:b", Designed: true, Planned: true, Evidence: []string{"git-oid:abc"}},
	})
	if projection.RequirementCoverage.Covered != 2 || projection.PlanCoverage.Covered != 2 {
		t.Fatalf("coverage axes = %+v %+v", projection.RequirementCoverage, projection.PlanCoverage)
	}
	if projection.DeliveryCoverage.Covered != 1 || projection.PeerReviewReady {
		t.Fatalf("delivery projection = %+v", projection)
	}
	if projection.Criteria[0].Delivered {
		t.Fatal("planned work without immutable evidence was treated as delivered")
	}
}

func TestEvaluateRetainsTransitiveBlockerPaths(t *testing.T) {
	projection := Evaluate([]Criterion{{
		URN: "criterion:a", Designed: true, Planned: true, Evidence: []string{"git-oid:abc"},
		UpstreamPaths: []BlockerPath{{Path: []string{"item:a", "item:b", "item:c"}, Blocker: Blocker{Code: "merge_not_integrated", Resource: "item:c"}}},
	}})
	if projection.Criteria[0].Delivered || len(projection.Criteria[0].TransitivePaths) != 1 {
		t.Fatalf("transitive blocker was lost: %+v", projection.Criteria[0])
	}
}

func TestOptionalPlanningGapsDoNotVetoImmutableDeliveryEvidence(t *testing.T) {
	projection := Evaluate([]Criterion{{
		URN: "criterion:a", Evidence: []string{"git-oid:abc"},
		DirectBlockers: []Blocker{{Code: "design_missing"}, {Code: "work_item_missing"}},
	}})
	if !projection.PeerReviewReady || !projection.Criteria[0].Delivered {
		t.Fatalf("optional planning axes vetoed delivery: %+v", projection)
	}
	if projection.RequirementCoverage.Covered != 0 || projection.PlanCoverage.Covered != 0 || len(projection.Criteria[0].Blockers) != 2 {
		t.Fatalf("independent coverage guidance was lost: %+v", projection)
	}
}
