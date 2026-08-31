package saga

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	MaxDesignDigestTargets        = 10_000
	MaxDesignDigestFilesPerTarget = 1_024
	MaxDesignDigestBytesPerTarget = 16 << 20
)

type designFileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type designFragmentDigest struct {
	Version    int                `json:"version"`
	ID         string             `json:"id"`
	Title      string             `json:"title,omitempty"`
	MediaType  string             `json:"media_type"`
	Entrypoint string             `json:"entrypoint"`
	Order      int                `json:"order,omitempty"`
	Files      []designFileDigest `json:"files"`
}

type designLandmarkDigest struct {
	Version     int              `json:"version"`
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Description string           `json:"description,omitempty"`
	Selector    LandmarkSelector `json:"selector"`
	Hotspot     *LandmarkRegion  `json:"hotspot,omitempty"`
	Fragment    string           `json:"fragment"`
}

type designSectionDigest struct {
	Kind     string            `json:"kind"`
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Order    int               `json:"order,omitempty"`
	Contents map[string]string `json:"contents"`
}

// CurrentDesignContentDigests returns the current digest for every addressable
// target beneath ___design. Digests cover authored design content only:
// evidence, approvals, and review records do not invalidate requirement or
// work-plan relations. The fixed budgets keep a query from turning one design
// target into an unbounded filesystem read.
func CurrentDesignContentDigests(document *Saga) (map[string]string, error) {
	result := map[string]string{}
	if document == nil || document.Section == nil || document.Manifest.Version != CurrentSagaVersion {
		return result, nil
	}
	_, err := digestDesignSection(document.Section, result, false)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CurrentDesignContentDigest looks up one addressable design target using the
// same canonical digest contract as CurrentDesignContentDigests.
func CurrentDesignContentDigest(document *Saga, target string) (string, bool, error) {
	digests, err := CurrentDesignContentDigests(document)
	if err != nil {
		return "", false, err
	}
	digest, ok := digests[target]
	return digest, ok, nil
}

func digestDesignSection(section *Section, result map[string]string, includeSelf bool) (string, error) {
	contents := map[string]string{}
	for _, fragment := range section.Fragments {
		if !isDesignPath(fragment.Path) {
			continue
		}
		digest, err := digestDesignFragment(fragment, result)
		if err != nil {
			return "", err
		}
		contents[fragment.Target] = digest
	}
	for _, child := range section.Children {
		childIsDesign := isDesignPath(child.Path)
		digest, err := digestDesignSection(child, result, childIsDesign)
		if err != nil {
			return "", err
		}
		if childIsDesign {
			contents[child.Target] = digest
		}
	}
	if !includeSelf {
		return "", nil
	}
	digest, err := canonicalDesignDigest("section-v1", designSectionDigest{
		Kind: section.Kind, ID: section.ID, Title: section.Title, Order: section.Order, Contents: contents,
	})
	if err != nil {
		return "", err
	}
	if err := addDesignDigest(result, section.Target, digest); err != nil {
		return "", err
	}
	return digest, nil
}

func digestDesignFragment(fragment *Fragment, result map[string]string) (string, error) {
	files, err := designAuthoredFiles(fragment.Directory)
	if err != nil {
		return "", fmt.Errorf("digest design fragment %q: %w", fragment.Target, err)
	}
	base, err := canonicalDesignDigest("fragment-content-v1", designFragmentDigest{
		Version: ComponentVersion, ID: fragment.ID, Title: fragment.Title, MediaType: fragment.MediaType,
		Entrypoint: fragment.Entrypoint, Order: fragment.Order, Files: files,
	})
	if err != nil {
		return "", err
	}
	landmarks := map[string]string{}
	for _, landmark := range fragment.Landmarks {
		digest, err := canonicalDesignDigest("landmark-v1", designLandmarkDigest{
			Version: landmark.Version, ID: landmark.ID, Label: landmark.Label,
			Description: landmark.Description, Selector: landmark.Selector,
			Hotspot: landmark.Hotspot, Fragment: base,
		})
		if err != nil {
			return "", err
		}
		if err := addDesignDigest(result, landmark.Target, digest); err != nil {
			return "", err
		}
		landmarks[landmark.Target] = digest
	}
	digest, err := canonicalDesignDigest("fragment-v1", struct {
		Content   string            `json:"content"`
		Landmarks map[string]string `json:"landmarks"`
	}{Content: base, Landmarks: landmarks})
	if err != nil {
		return "", err
	}
	if err := addDesignDigest(result, fragment.Target, digest); err != nil {
		return "", err
	}
	return digest, nil
}

func designAuthoredFiles(root string) ([]designFileDigest, error) {
	files := []designFileDigest{}
	total := int64(0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if strings.HasPrefix(parts[0], "___") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || rel == "fragment.json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("authored file %q must be a regular file", filepath.ToSlash(rel))
		}
		if len(files) >= MaxDesignDigestFilesPerTarget {
			return fmt.Errorf("authored package exceeds %d files", MaxDesignDigestFilesPerTarget)
		}
		total += info.Size()
		if total > MaxDesignDigestBytesPerTarget {
			return fmt.Errorf("authored package exceeds %d bytes", MaxDesignDigestBytesPerTarget)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files = append(files, designFileDigest{Path: filepath.ToSlash(rel), SHA256: fmt.Sprintf("sha256:%x", sum)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func canonicalDesignDigest(domain string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("change-saga-design-"+domain+"\x00"), data...))
	return fmt.Sprintf("sha256:%x", sum), nil
}

func addDesignDigest(result map[string]string, target, digest string) error {
	if len(result) >= MaxDesignDigestTargets {
		return fmt.Errorf("technical design exceeds %d addressable targets", MaxDesignDigestTargets)
	}
	result[target] = digest
	return nil
}

func isDesignPath(path string) bool {
	return path == "___design" || strings.HasPrefix(path, "___design/")
}
