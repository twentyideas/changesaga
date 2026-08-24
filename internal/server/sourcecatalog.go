package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"sort"
	"sync"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

// sourceCatalogCache retains only changed-file metadata. It advances with the
// exact source fingerprint, independently from narrative, mapping, coverage,
// and mutable review generations.
type sourceCatalogCache struct {
	mutex       sync.Mutex
	fingerprint string
	value       gitdiff.Catalog
	ready       bool
	builds      int
}

func (a *app) sourceCatalog(ctx context.Context, manifest saga.Manifest) (gitdiff.Catalog, error) {
	fingerprint, err := a.sourceFingerprint(ctx, manifest)
	if err != nil {
		return gitdiff.Catalog{}, err
	}
	a.catalog.mutex.Lock()
	defer a.catalog.mutex.Unlock()
	if fingerprint != "" && a.catalog.ready && a.catalog.fingerprint == fingerprint {
		return a.catalog.value, nil
	}
	var catalog gitdiff.Catalog
	if a.catalogLoader != nil {
		catalog, err = a.catalogLoader(ctx, manifest)
	} else {
		catalog, err = gitdiff.ReadCatalog(ctx, a.sourceDir, manifest.Source.Repository, manifest.Source.Base, manifest.Source.Head)
	}
	if err != nil {
		return gitdiff.Catalog{}, err
	}
	a.catalog.builds++
	// WORKTREE has no stable source fingerprint. Its catalog is correct for this
	// request but cannot be reused after the request without guessing freshness.
	if fingerprint != "" {
		a.catalog.fingerprint, a.catalog.value, a.catalog.ready = fingerprint, catalog, true
	}
	return catalog, nil
}

func sourceCatalogIdentity(catalog gitdiff.Catalog) string {
	digest := sha256.Sum256([]byte(catalog.Repository + "\x00" + catalog.BaseOID + "\x00" + catalog.HeadOID))
	return hex.EncodeToString(digest[:16])
}

func selectedCatalogPath(catalog gitdiff.Catalog, r *http.Request) (string, error) {
	filePath := r.URL.Query().Get("file")
	if raw := r.URL.Query().Get("diff"); raw != "" {
		reference, err := diffuri.Parse(raw)
		if err != nil || reference.Repository != catalog.Repository || reference.Base != catalog.BaseOID || reference.Head != catalog.HeadOID {
			return "", fmt.Errorf("invalid selected diff URI")
		}
		fromDiff := reference.Path
		if reference.NewPath != "" {
			fromDiff = reference.NewPath
		}
		if filePath == "" {
			filePath = fromDiff
		} else if fromDiff != "" && filePath != fromDiff {
			return "", fmt.Errorf("selected diff is not part of the changed file")
		}
	}
	if filePath == "" && len(catalog.Files) > 0 {
		filePath = catalog.Files[0].Path
	}
	if filePath != "" {
		index := sort.Search(len(catalog.Files), func(index int) bool { return catalog.Files[index].Path >= filePath })
		if index == len(catalog.Files) || catalog.Files[index].Path != filePath {
			return "", fmt.Errorf("changed file not found")
		}
	}
	return filePath, nil
}

func catalogFile(catalog gitdiff.Catalog, filePath string) (gitdiff.FileSummary, bool) {
	index := sort.Search(len(catalog.Files), func(index int) bool { return catalog.Files[index].Path >= filePath })
	if index == len(catalog.Files) || catalog.Files[index].Path != filePath {
		return gitdiff.FileSummary{}, false
	}
	return catalog.Files[index], true
}

func latestCatalogReviews(document *saga.Saga, catalog gitdiff.Catalog) (map[string]saga.DiffReview, int) {
	latest := map[string]saga.DiffReview{}
	for _, review := range document.DiffReviews {
		reference, err := diffuri.Parse(review.URI)
		if err != nil || reference.Kind != "file" || reference.Repository != catalog.Repository || reference.Base != catalog.BaseOID || reference.Head != catalog.HeadOID {
			continue
		}
		previous, ok := latest[reference.Path]
		if !ok || previous.CreatedAt.Before(review.CreatedAt) || previous.CreatedAt.Equal(review.CreatedAt) && previous.ID < review.ID {
			latest[reference.Path] = review
		}
	}
	reviewed := 0
	for _, review := range latest {
		if review.State == "reviewed" {
			reviewed++
		}
	}
	return latest, reviewed
}

func latestReviewForCatalogFile(document *saga.Saga, catalog gitdiff.Catalog, filePath string) saga.DiffReview {
	reviews, _ := latestCatalogReviews(document, catalog)
	return reviews[filePath]
}

func catalogFileView(catalog gitdiff.Catalog, file gitdiff.FileSummary, review saga.DiffReview) *FileDiffView {
	uri, _ := diffuri.Build(diffuri.Reference{
		Repository: catalog.Repository, Base: catalog.BaseOID, Head: catalog.HeadOID,
		Kind: "file", Path: file.Path,
	})
	digest := sha256.Sum256([]byte(file.Path))
	view := &FileDiffView{
		ID: fmt.Sprintf("diff-%x", digest[:8]), Name: path.Base(file.Path), Path: file.Path,
		URI: uri, Href: CodeDiffURL(file.Path, ""), Added: file.Added, Deleted: file.Deleted,
	}
	if review.ID != "" {
		view.Reviewed, view.Reviewer, view.ReviewerDetail = review.State == "reviewed", review.Author, review.AttributionDetail
	}
	return view
}
