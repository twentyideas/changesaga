package saga

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	FlatManifestName = "00-saga.json"
	FlatMaxBasename  = 64
	FlatMaxPath      = 240
	flatMaxRank      = 9999
)

// V4 filenames are a compact, deterministic storage index. Human meaning
// stays in the JSON and target URNs; filenames carry only category, parent,
// ordering, and collision-resistant identity hints.
var (
	flatDeckName         = regexp.MustCompile(`^10-d-([0-9]{4})-([0-9a-f]{12})\.json$`)
	flatSlideName        = regexp.MustCompile(`^20-s-([0-9a-f]{12})-([0-9]{4})-([0-9a-f]{12})\.json$`)
	flatItemName         = regexp.MustCompile(`^30-i-([0-9a-f]{12})-([0-9]{4})-([0-9a-f]{12})\.json$`)
	flatEvidenceName     = regexp.MustCompile(`^40-e-([0-9a-f]{12})-([0-9a-f]{12})\.json$`)
	flatClaimName        = regexp.MustCompile(`^50-c-([0-9a-f]{12})\.json$`)
	flatVerificationName = regexp.MustCompile(`^60-v-([0-9a-f]{12})-([0-9a-f]{12})\.json$`)
	flatThreadName       = regexp.MustCompile(`^80-t-([0-9a-f]{12})-([0-9a-f]{12})\.json$`)
	flatMessageName      = regexp.MustCompile(`^81-m-([0-9a-f]{12})-([0-9a-f]{12})\.json$`)
	flatAttachmentName   = regexp.MustCompile(`^82-a-([0-9a-f]{12})-([0-9]{2})-([0-9a-f]{12})\.json$`)
	flatAttachmentAsset  = regexp.MustCompile(`^82-a-[0-9a-f]{12}-[0-9]{2}-[0-9a-f]{12}\.[a-z0-9]+$`)
	flatThreadEventName  = regexp.MustCompile(`^83-x-([0-9a-f]{12})-([0-9a-f]{12})\.json$`)
	flatReviewName       = regexp.MustCompile(`^84-r-([0-9a-f]{12})-([0-9a-f]{12})\.json$`)
	flatDiffReviewName   = regexp.MustCompile(`^85-f-([0-9a-f]{12})\.json$`)
)

func FlatKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:6])
}

func FlatTargetKey(target string) string { return FlatKey("target\x00" + target) }

func FlatDeckFilename(target string, rank int) (string, error) {
	if err := validFlatRank(rank); err != nil {
		return "", err
	}
	return fmt.Sprintf("10-d-%04d-%s.json", rank, FlatTargetKey(target)), nil
}

func FlatSlideFilename(deckTarget, slideTarget string, rank int) (string, error) {
	if err := validFlatRank(rank); err != nil {
		return "", err
	}
	return fmt.Sprintf("20-s-%s-%04d-%s.json", FlatTargetKey(deckTarget), rank, FlatTargetKey(slideTarget)), nil
}

func FlatSlideAssetFilename(slideManifest, extension string) (string, error) {
	if !strings.HasSuffix(slideManifest, ".json") {
		return "", fmt.Errorf("slide manifest must end in .json")
	}
	extension = strings.ToLower(extension)
	if extension == "" || strings.ContainsAny(extension, `/\\`) || extension[0] != '.' {
		return "", fmt.Errorf("slide content requires a simple file extension")
	}
	name := strings.TrimSuffix(slideManifest, ".json") + extension
	if len(name) > FlatMaxBasename {
		return "", fmt.Errorf("slide content filename exceeds %d characters", FlatMaxBasename)
	}
	return name, nil
}

func FlatItemFilename(slideTarget, itemTarget string, rank int) (string, error) {
	if err := validFlatRank(rank); err != nil {
		return "", err
	}
	return fmt.Sprintf("30-i-%s-%04d-%s.json", FlatTargetKey(slideTarget), rank, FlatTargetKey(itemTarget)), nil
}

func FlatEvidenceFilename(target, identity string) string {
	return fmt.Sprintf("40-e-%s-%s.json", FlatTargetKey(target), FlatKey("evidence\x00"+identity))
}

func FlatClaimFilename(id string) string {
	return fmt.Sprintf("50-c-%s.json", FlatKey("claim\x00"+id))
}

func FlatVerificationFilename(claimID, id string) string {
	return fmt.Sprintf("60-v-%s-%s.json", FlatKey("claim\x00"+claimID), FlatKey("verification\x00"+id))
}

func FlatThreadFilename(target, id string) string {
	return fmt.Sprintf("80-t-%s-%s.json", FlatTargetKey(target), FlatKey("thread\x00"+id))
}

func FlatMessageFilename(threadID, id string) string {
	return fmt.Sprintf("81-m-%s-%s.json", FlatKey("thread\x00"+threadID), FlatKey("message\x00"+id))
}

func FlatAttachmentFilename(messageID string, order int, fragmentID string) (string, error) {
	if order < 0 || order > 99 {
		return "", fmt.Errorf("review attachment order must be between 0 and 99")
	}
	return fmt.Sprintf("82-a-%s-%02d-%s.json", FlatKey("message\x00"+messageID), order, FlatKey("fragment\x00"+fragmentID)), nil
}

func FlatThreadEventFilename(threadID, id string) string {
	return fmt.Sprintf("83-x-%s-%s.json", FlatKey("thread\x00"+threadID), FlatKey("event\x00"+id))
}

func FlatReviewFilename(target, id string) string {
	return fmt.Sprintf("84-r-%s-%s.json", FlatTargetKey(target), FlatKey("review\x00"+id))
}

func FlatDiffReviewFilename(id string) string {
	return fmt.Sprintf("85-f-%s.json", FlatKey("diff-review\x00"+id))
}

// IsFlatReviewRecord reports whether name belongs to the mutable review
// overlay in a v4 Saga. Keeping this classification beside the filename
// grammar lets caches observe review changes without mistaking authored deck,
// slide, Item, or evidence records for mutable review state.
func IsFlatReviewRecord(name string) bool {
	return flatThreadName.MatchString(name) ||
		flatMessageName.MatchString(name) ||
		flatAttachmentName.MatchString(name) ||
		flatAttachmentAsset.MatchString(name) ||
		flatThreadEventName.MatchString(name) ||
		flatReviewName.MatchString(name) ||
		flatDiffReviewName.MatchString(name)
}

func validFlatRank(rank int) error {
	if rank < 0 || rank > flatMaxRank {
		return fmt.Errorf("rank must be between 0 and %d for the portable v4 layout", flatMaxRank)
	}
	return nil
}

func parseFlatRank(value string) (int, bool) {
	rank, err := strconv.Atoi(value)
	return rank, err == nil
}

func flatPathIssue(root, name string) string {
	if len(name) > FlatMaxBasename {
		return fmt.Sprintf("portable v4 basenames cannot exceed %d characters", FlatMaxBasename)
	}
	abs := filepath.Join(root, name)
	if len(abs) > FlatMaxPath {
		return fmt.Sprintf("portable v4 path exceeds %d characters; choose a shorter Saga location", FlatMaxPath)
	}
	return ""
}

func ValidateFlatRoot(root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if issue := flatPathIssue(abs, strings.Repeat("x", FlatMaxBasename)); issue != "" {
		return fmt.Errorf("portable v4 layout: %s", issue)
	}
	// init publishes through a same-parent staging directory. Budget that path
	// too so a root with a very short final basename cannot pass preflight and
	// then fail while its staged files are being written.
	stage := filepath.Join(filepath.Dir(abs), ".change-saga-stage-xxxxxxxxxxxx")
	if issue := flatPathIssue(stage, strings.Repeat("x", FlatMaxBasename)); issue != "" {
		return fmt.Errorf("portable v4 layout: parent directory is too deep for atomic initialization; choose a shorter Saga location")
	}
	return nil
}

func flatRegular(entry fs.DirEntry) bool {
	info, err := entry.Info()
	return err == nil && info.Mode().IsRegular()
}
