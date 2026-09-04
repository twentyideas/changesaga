package saga

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFlatV4NamesAreCompactDeterministicAndCategorized(t *testing.T) {
	deck := DeckTarget("visual", "a-deck-title-that-never-enters-the-filename")
	slide := SlideTarget("visual", "a-slide-title-that-never-enters-the-filename")
	item := ItemTarget("visual", "a-slide-title-that-never-enters-the-filename", "a-meaningful-callout")
	deckName, err := FlatDeckFilename(deck, 10)
	if err != nil {
		t.Fatal(err)
	}
	slideName, err := FlatSlideFilename(deck, slide, 20)
	if err != nil {
		t.Fatal(err)
	}
	itemName, err := FlatItemFilename(slide, item, 30)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{
		FlatManifestName,
		deckName,
		slideName,
		itemName,
		FlatEvidenceFilename(item, strings.Repeat("source-path-that-does-not-leak-", 10)),
		FlatClaimFilename(strings.Repeat("claim-title-", 10)),
		FlatVerificationFilename("claim", strings.Repeat("verification-title-", 10)),
		FlatThreadFilename(item, strings.Repeat("thread-id-", 10)),
	}
	prefixes := []string{"00-", "10-d-", "20-s-", "30-i-", "40-e-", "50-c-", "60-v-", "80-t-"}
	for i, name := range names {
		if len(name) > FlatMaxBasename {
			t.Fatalf("%s is %d characters", name, len(name))
		}
		if !strings.HasPrefix(name, prefixes[i]) {
			t.Fatalf("%s does not use category %s", name, prefixes[i])
		}
	}
	if again, _ := FlatItemFilename(slide, item, 30); again != itemName {
		t.Fatalf("flat name is not deterministic: %s != %s", again, itemName)
	}
}

func TestFlatV4RootBudgetLeavesRoomForEveryBasename(t *testing.T) {
	short := filepath.Join(t.TempDir(), "reviews", "change.saga")
	if err := ValidateFlatRoot(short); err != nil {
		t.Fatalf("ordinary nested root rejected: %v", err)
	}
	// A short final directory can fit while the longer atomic-staging name in
	// its parent does not. Reject it before init creates anything.
	stageTightParent := string(filepath.Separator) + strings.Repeat("p", 165)
	if err := ValidateFlatRoot(filepath.Join(stageTightParent, "x.saga")); err == nil || !strings.Contains(err.Error(), "atomic initialization") {
		t.Fatalf("staging-path overflow was not rejected clearly: %v", err)
	}
	long := string(filepath.Separator) + strings.Repeat("deep"+string(filepath.Separator), 60) + "change.saga"
	if err := ValidateFlatRoot(long); err == nil || !strings.Contains(err.Error(), "shorter Saga location") {
		t.Fatalf("long root was not rejected clearly: %v", err)
	}
}

func TestFlatV4RankBudgetIsExplicit(t *testing.T) {
	if _, err := FlatDeckFilename("target", 10000); err == nil {
		t.Fatal("rank wider than the fixed sort field was accepted")
	}
}

func TestFlatReviewRecordClassificationExcludesAuthoredRecords(t *testing.T) {
	thread := FlatThreadFilename("target", "thread")
	message := FlatMessageFilename("thread", "message")
	attachment, err := FlatAttachmentFilename("message", 0, "attachment")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := FlatSlideAssetFilename(attachment, ".png")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		thread, message, attachment, asset,
		FlatThreadEventFilename("thread", "event"),
		FlatReviewFilename("target", "review"),
		FlatDiffReviewFilename("diff-review"),
	} {
		if !IsFlatReviewRecord(name) {
			t.Errorf("review record %q was not classified as mutable", name)
		}
	}
	for _, name := range []string{FlatManifestName, FlatEvidenceFilename("target", "evidence")} {
		if IsFlatReviewRecord(name) {
			t.Errorf("authored record %q was classified as mutable review state", name)
		}
	}
}
