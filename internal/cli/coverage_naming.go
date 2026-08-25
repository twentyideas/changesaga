package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

// generatedCoverageName is a stable name for one logical coverage assertion.
// Notes are deliberately excluded: two branches that attach different prose
// to the same selectors must write the same path so Git exposes the semantic
// disagreement instead of silently retaining both records as an overlap.
func stableGeneratedCoverageName(record coverRecord, file saga.DiffFile) string {
	prefix := store.Slug(firstNonEmpty(record.Path, record.Event, "diff"))
	if len(prefix) > 36 {
		prefix = strings.Trim(prefix[:36], "-")
	}
	if prefix == "" {
		prefix = "diff"
	}

	selectors := make([]string, len(file.Diffs))
	for i, reference := range file.Diffs {
		selectors[i] = reference.URI
	}
	sort.Strings(selectors)
	hash := sha256.New()
	for _, selector := range selectors {
		_, _ = hash.Write([]byte(selector))
		_, _ = hash.Write([]byte{0})
	}
	digest := hash.Sum(nil)
	return prefix + "-" + hex.EncodeToString(digest[:8])
}
