package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/twentyideas/changesaga/internal/gitattribution"
	"github.com/twentyideas/changesaga/internal/saga"
)

// outlineCache is intentionally independent from snapshotCache. Its input set
// excludes coverage and source-comparison data, so adding a mapping or changing
// a source commit cannot force the root request onto the expensive path.
type outlineCache struct {
	mutex       sync.Mutex
	fingerprint string
	document    *saga.Saga
	builds      int
}

func (a *app) outlineDocument(ctx context.Context) *saga.Saga {
	a.outline.mutex.Lock()
	defer a.outline.mutex.Unlock()
	fingerprint, err := a.outlineFingerprint(ctx)
	if err != nil {
		return nil
	}
	if a.outline.document != nil && fingerprint == a.outline.fingerprint {
		return a.outline.document
	}
	document, validation, err := saga.LoadOutline(a.root)
	if err != nil || !validation.Valid {
		return nil
	}
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	// Fingerprint after loading. A concurrent edit cannot be mistaken for the
	// model just read; it produces a miss on the next request.
	after, err := a.outlineFingerprint(ctx)
	if err != nil {
		return document
	}
	a.outline.builds++
	a.outline.fingerprint, a.outline.document = after, document
	return document
}

func (a *app) outlineFingerprint(ctx context.Context) (string, error) {
	tree, err := outlineFingerprint(a.root)
	if err != nil {
		return "", err
	}
	head, _ := gitOutput(ctx, a.root, "rev-parse", "HEAD")
	return tree + "\x00" + head, nil
}

// outlineFingerprint commits only to files LoadOutline consumes. In
// particular, ___diffs directories and fragment bodies are skipped as
// directories/files rather than merely omitted from the digest after a full
// walk. A root request therefore does not scale with per-line evidence or code
// attachment size even during freshness checks.
func outlineFingerprint(root string) (string, error) {
	digest := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			base := entry.Name()
			if path != root && skipOutlineDirectory(rel, base) {
				return filepath.SkipDir
			}
			fmt.Fprintf(digest, "d\x00%s\x00", rel)
			return nil
		}
		if !outlineFile(rel, entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(digest, "f\x00%s\x00%d\x00%d\x00", rel, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func skipOutlineDirectory(rel, base string) bool {
	switch base {
	case "___diffs", "___claims", "___verifications", "___landmarks", "messages":
		return true
	}
	if strings.HasSuffix(base, ".message") {
		return true
	}
	// Fragment packages may contain arbitrarily large asset trees. The outline
	// reads only fragment.json and ___approvals from them.
	parts := strings.Split(rel, "/")
	for index := 0; index < len(parts)-1; index++ {
		if strings.HasSuffix(parts[index], ".fragment") && !strings.HasPrefix(base, "___") {
			return true
		}
	}
	return false
}

func outlineFile(rel, base string) bool {
	switch base {
	case "saga.json", "chapter.json", "section.json", "fragment.json", "thread.json":
		return true
	}
	return strings.Contains(rel, "/___approvals/") || strings.Contains(rel, "/events/")
}
