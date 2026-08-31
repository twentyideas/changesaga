package prototypes

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/store"
)

func AddHTML(root, sagaID string, input AddHTMLInput) (MutationResult, error) {
	created := mutationTime(input.CreatedAt)
	identity := Identity{Schema: IdentitySchemaURL, Version: Version, ID: input.ID, CreatedAt: created, RequestID: input.RequestID}
	if err := validateIdentity(identity, input.ID); err != nil {
		return MutationResult{}, err
	}
	prototypeURN, err := PrototypeURN(sagaID, input.ID)
	if err != nil {
		return MutationResult{}, err
	}
	var result MutationResult
	err = mutate(root, sagaID, func(doc *Document) error {
		if err := validateIdentity(identity, input.ID); err != nil {
			return err
		}
		if existing := findPrototype(doc, input.ID); existing != nil {
			if input.RequestID != "" && existing.Identity.RequestID == input.RequestID {
				digest, digestErr := digestHTMLInput(input.SourcePath)
				if digestErr != nil {
					return digestErr
				}
				wanted := Revision{Schema: RevisionSchemaURL, Version: Version, ID: input.RevisionID, Prototype: prototypeURN, Parents: []string{}, Title: strings.TrimSpace(input.Title), State: input.State, Source: Source{Kind: SourceHTML, Entrypoint: "html/index.html", ContentDigest: digest}, Styles: []StyleSource{}, CreatedAt: created, RequestID: input.RequestID}
				if equalPrototypeCreation(*existing, identity, wanted) {
					result = MutationResult{URN: prototypeURN, Path: prototypePath(input.ID), Replayed: true}
					return nil
				}
			}
			return fmt.Errorf("prototype id %q already exists", input.ID)
		}
		if len(doc.Prototypes) >= MaxPrototypes {
			return fmt.Errorf("prototype limit of %d reached", MaxPrototypes)
		}
		final := filepath.Join(doc.Root, filepath.FromSlash(prototypePath(input.ID)))
		err := store.CommitDir(doc.Root, final, func(stage string) error {
			if err := store.WriteJSON(filepath.Join(stage, "prototype.json"), identity, true); err != nil {
				return err
			}
			revisions := filepath.Join(stage, "revisions")
			if err := os.Mkdir(revisions, 0o755); err != nil {
				return err
			}
			revisionPackage := filepath.Join(revisions, input.RevisionID+".revision")
			if err := os.Mkdir(revisionPackage, 0o755); err != nil {
				return err
			}
			htmlDir := filepath.Join(revisionPackage, "html")
			if err := copyHTMLSource(input.SourcePath, htmlDir); err != nil {
				return err
			}
			digest, err := digestTree(htmlDir)
			if err != nil {
				return err
			}
			revision := Revision{Schema: RevisionSchemaURL, Version: Version, ID: input.RevisionID, Prototype: prototypeURN, Parents: []string{}, Title: strings.TrimSpace(input.Title), State: input.State, Source: Source{Kind: SourceHTML, Entrypoint: "html/index.html", ContentDigest: digest}, Styles: []StyleSource{}, CreatedAt: created, RequestID: input.RequestID}
			if err := validateRevision(revision, sagaID, input.ID); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(revisionPackage, "revision.json"), revision, true)
		})
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("prototype id %q already exists", input.ID)
		}
		if err != nil {
			return err
		}
		result = MutationResult{URN: prototypeURN, Path: prototypePath(input.ID)}
		return nil
	})
	return result, err
}

// AddHTMLPrototype is an explicit-name alias for adapters that prefer the
// resource noun in mutation calls.
func AddHTMLPrototype(root, sagaID string, input AddHTMLInput) (MutationResult, error) {
	return AddHTML(root, sagaID, input)
}

func AddExternal(root, sagaID string, input AddExternalInput) (MutationResult, error) {
	created := mutationTime(input.CreatedAt)
	identity := Identity{Schema: IdentitySchemaURL, Version: Version, ID: input.ID, CreatedAt: created, RequestID: input.RequestID}
	if err := validateIdentity(identity, input.ID); err != nil {
		return MutationResult{}, err
	}
	prototypeURN, err := PrototypeURN(sagaID, input.ID)
	if err != nil {
		return MutationResult{}, err
	}
	source := Source{Kind: SourceExternal, URL: input.URL}
	if input.EmbedURL != "" {
		fallback := input.FallbackURL
		if fallback == "" {
			fallback = input.URL
		}
		source = Source{Kind: SourceEmbed, EmbedURL: input.EmbedURL, FallbackURL: fallback, Allowlist: cloneAllowlist(input.Allowlist)}
	}
	revision := Revision{Schema: RevisionSchemaURL, Version: Version, ID: input.RevisionID, Prototype: prototypeURN, Parents: []string{}, Title: strings.TrimSpace(input.Title), State: input.State, Source: source, Styles: []StyleSource{}, CreatedAt: created, RequestID: input.RequestID}
	if err := validateRevision(revision, sagaID, input.ID); err != nil {
		return MutationResult{}, err
	}
	var result MutationResult
	err = mutate(root, sagaID, func(doc *Document) error {
		if err := validateIdentity(identity, input.ID); err != nil {
			return err
		}
		if err := validateRevision(revision, sagaID, input.ID); err != nil {
			return err
		}
		if existing := findPrototype(doc, input.ID); existing != nil {
			if input.RequestID != "" && existing.Identity.RequestID == input.RequestID && equalExternalCreation(*existing, identity, revision) {
				result = MutationResult{URN: prototypeURN, Path: prototypePath(input.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("prototype id %q already exists", input.ID)
		}
		if len(doc.Prototypes) >= MaxPrototypes {
			return fmt.Errorf("prototype limit of %d reached", MaxPrototypes)
		}
		final := filepath.Join(doc.Root, filepath.FromSlash(prototypePath(input.ID)))
		err := store.CommitDir(doc.Root, final, func(stage string) error {
			if err := store.WriteJSON(filepath.Join(stage, "prototype.json"), identity, true); err != nil {
				return err
			}
			revisions := filepath.Join(stage, "revisions")
			if err := os.Mkdir(revisions, 0o755); err != nil {
				return err
			}
			revisionPackage := filepath.Join(revisions, input.RevisionID+".revision")
			if err := os.Mkdir(revisionPackage, 0o755); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(revisionPackage, "revision.json"), revision, true)
		})
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("prototype id %q already exists", input.ID)
		}
		if err != nil {
			return err
		}
		result = MutationResult{URN: prototypeURN, Path: prototypePath(input.ID)}
		return nil
	})
	return result, err
}

func AddExternalPrototype(root, sagaID string, input AddExternalInput) (MutationResult, error) {
	return AddExternal(root, sagaID, input)
}

// Revise appends a complete revision. HTML revisions must provide a fresh
// HTMLSourcePath; this prevents a mutable directory outside the Saga from
// becoming an implicit part of the revision.
func Revise(root, sagaID string, input ReviseInput) (MutationResult, error) {
	prototypeID, err := parsePrototypeURN(input.Prototype, sagaID)
	if err != nil {
		return MutationResult{}, err
	}
	created := mutationTime(input.CreatedAt)
	revisionURN, err := RevisionURN(sagaID, prototypeID, input.ID)
	if err != nil {
		return MutationResult{}, err
	}
	var result MutationResult
	err = mutate(root, sagaID, func(doc *Document) error {
		prototype := findPrototype(doc, prototypeID)
		if prototype == nil {
			return fmt.Errorf("prototype %q does not exist", prototypeID)
		}
		for _, existing := range prototype.Revisions {
			if existing.ID == input.ID {
				if input.RequestID != "" && existing.RequestID == input.RequestID && revisionInputMatches(existing, input) {
					result = MutationResult{URN: revisionURN, Path: revisionPath(prototypeID, input.ID), Replayed: true}
					return nil
				}
				return fmt.Errorf("revision id %q already exists", input.ID)
			}
		}
		if len(prototype.Revisions) >= MaxRevisionsPerPrototype {
			return fmt.Errorf("revision limit of %d reached", MaxRevisionsPerPrototype)
		}
		if !sameSet(input.Parents, prototype.RevisionHeads) {
			return fmt.Errorf("revision parents must name every current head (got %v, want %v)", input.Parents, prototype.RevisionHeads)
		}
		final := filepath.Join(doc.Root, filepath.FromSlash(revisionPath(prototypeID, input.ID)))
		err := store.CommitDir(doc.Root, final, func(stage string) error {
			source := cloneSource(input.Source)
			if input.HTMLSourcePath != "" {
				if source.Kind != "" && source.Kind != SourceHTML {
					return fmt.Errorf("external revision cannot provide HTMLSourcePath")
				}
				htmlDir := filepath.Join(stage, "html")
				if err := copyHTMLSource(input.HTMLSourcePath, htmlDir); err != nil {
					return err
				}
				digest, err := digestTree(htmlDir)
				if err != nil {
					return err
				}
				source = Source{Kind: SourceHTML, Entrypoint: "html/index.html", ContentDigest: digest}
			} else if source.Kind == SourceHTML {
				return fmt.Errorf("html revision requires HTMLSourcePath")
			}
			revision := Revision{Schema: RevisionSchemaURL, Version: Version, ID: input.ID, Prototype: input.Prototype, Parents: copyStrings(input.Parents), Title: strings.TrimSpace(input.Title), State: input.State, Source: source, Styles: copyStyles(input.Styles), CreatedAt: created, RequestID: input.RequestID}
			if err := validateRevision(revision, sagaID, prototypeID); err != nil {
				return err
			}
			candidate := *prototype
			candidate.Revisions = append(append([]Revision{}, prototype.Revisions...), revision)
			if err := validateRevisionGraph(&candidate, sagaID); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(stage, "revision.json"), revision, true)
		})
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("revision id %q already exists", input.ID)
		}
		if err != nil {
			return err
		}
		result = MutationResult{URN: revisionURN, Path: revisionPath(prototypeID, input.ID)}
		return nil
	})
	return result, err
}

func RevisePrototype(root, sagaID string, input ReviseInput) (MutationResult, error) {
	return Revise(root, sagaID, input)
}

func AddAnnotation(root, sagaID string, input AddAnnotationInput) (MutationResult, error) {
	prototypeID, err := parsePrototypeURN(input.Prototype, sagaID)
	if err != nil {
		return MutationResult{}, err
	}
	value := Annotation{Schema: AnnotationSchemaURL, Version: Version, ID: input.ID, Prototype: input.Prototype, Target: input.Target, Rationale: strings.TrimSpace(input.Rationale), PrototypeRevision: input.PrototypeRevision, PrototypeContentDigest: input.PrototypeContentDigest, StoryRevision: input.StoryRevision, Selector: input.Selector, CreatedAt: mutationTime(input.CreatedAt), RequestID: input.RequestID}
	if err := validateAnnotation(value, sagaID, input.ID); err != nil {
		return MutationResult{}, err
	}
	urn, _ := AnnotationURN(sagaID, prototypeID, input.ID)
	var result MutationResult
	err = mutate(root, sagaID, func(doc *Document) error {
		if err := validateAnnotation(value, sagaID, input.ID); err != nil {
			return err
		}
		for _, existing := range doc.Annotations {
			if existing.ID != input.ID || existing.Prototype != input.Prototype {
				continue
			}
			if input.RequestID != "" && existing.RequestID == input.RequestID && equalAnnotationIgnoringTime(existing, value) {
				result = MutationResult{URN: urn, Path: annotationPath(prototypeID, input.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("annotation id %q already exists", input.ID)
		}
		if len(doc.Annotations) >= MaxAnnotations {
			return fmt.Errorf("annotation limit of %d reached", MaxAnnotations)
		}
		dir, err := store.EnsureDirWithin(doc.Root, filepath.Join(doc.Root, "___requirements", "prototypes", "annotations", prototypeID+".prototype"))
		if err != nil {
			return err
		}
		path := filepath.Join(dir, input.ID+".json")
		if err := store.WriteJSON(path, value, true); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("annotation id %q already exists", input.ID)
		} else if err != nil {
			return err
		}
		result = MutationResult{URN: urn, Path: annotationPath(prototypeID, input.ID)}
		return nil
	})
	return result, err
}

func Annotate(root, sagaID string, input AddAnnotationInput) (MutationResult, error) {
	return AddAnnotation(root, sagaID, input)
}

// AddStyle appends a revision that pins one validated repository stylesheet.
// The prior immutable HTML tree is copied into the new revision package.
func AddStyle(root, sagaID string, input AddStyleInput) (MutationResult, error) {
	prototypeID, err := parsePrototypeURN(input.Prototype, sagaID)
	if err != nil {
		return MutationResult{}, err
	}
	_, digest, err := readRepositoryStyle(input.RepositoryRoot, input.Path)
	if err != nil {
		return MutationResult{}, fmt.Errorf("repository stylesheet: %w", err)
	}
	style := StyleSource{Path: input.Path, Digest: digest, CustomProperties: copyStrings(input.CustomProperties), Roles: append([]StyleRole{}, input.Roles...)}
	var p validationErrors
	validateStyle(&p, "style", style)
	if err := p.err(); err != nil {
		return MutationResult{}, err
	}
	created := mutationTime(input.CreatedAt)
	revisionURN, err := RevisionURN(sagaID, prototypeID, input.ID)
	if err != nil {
		return MutationResult{}, err
	}
	var result MutationResult
	err = mutate(root, sagaID, func(doc *Document) error {
		_, lockedDigest, err := readRepositoryStyle(input.RepositoryRoot, input.Path)
		if err != nil {
			return fmt.Errorf("repository stylesheet after locking: %w", err)
		}
		style.Digest = lockedDigest
		var lockedProblems validationErrors
		validateStyle(&lockedProblems, "style", style)
		if err := lockedProblems.err(); err != nil {
			return err
		}
		prototype := findPrototype(doc, prototypeID)
		if prototype == nil {
			return fmt.Errorf("prototype %q does not exist", prototypeID)
		}
		for _, existing := range prototype.Revisions {
			if existing.ID != input.ID {
				continue
			}
			if input.RequestID != "" && existing.RequestID == input.RequestID {
				for _, storedStyle := range existing.Styles {
					if reflect.DeepEqual(storedStyle, style) {
						result = MutationResult{URN: revisionURN, Path: revisionPath(prototypeID, input.ID), Replayed: true}
						return nil
					}
				}
			}
			return fmt.Errorf("revision id %q already exists", input.ID)
		}
		if prototype.CurrentRevision == nil {
			return fmt.Errorf("prototype %q has conflicting revision heads", prototypeID)
		}
		current := *prototype.CurrentRevision
		if current.Source.Kind != SourceHTML {
			return fmt.Errorf("repository styles are allowed only for html prototypes")
		}
		parents := input.Parents
		if len(parents) == 0 {
			parents = copyStrings(prototype.RevisionHeads)
		}
		if !sameSet(parents, prototype.RevisionHeads) {
			return fmt.Errorf("revision parents must name every current head")
		}
		styles := copyStyles(current.Styles)
		replaced := false
		for index := range styles {
			if styles[index].Path == style.Path {
				styles[index] = style
				replaced = true
				break
			}
		}
		if !replaced {
			styles = append(styles, style)
		}
		final := filepath.Join(doc.Root, filepath.FromSlash(revisionPath(prototypeID, input.ID)))
		err = store.CommitDir(doc.Root, final, func(stage string) error {
			priorHTML := filepath.Join(doc.Root, filepath.FromSlash(revisionPath(prototypeID, current.ID)), "html")
			if err := copyHTMLSource(priorHTML, filepath.Join(stage, "html")); err != nil {
				return err
			}
			revision := Revision{Schema: RevisionSchemaURL, Version: Version, ID: input.ID, Prototype: input.Prototype, Parents: copyStrings(parents), Title: current.Title, State: current.State, Source: current.Source, Styles: styles, CreatedAt: created, RequestID: input.RequestID}
			if err := validateRevision(revision, sagaID, prototypeID); err != nil {
				return err
			}
			candidate := *prototype
			candidate.Revisions = append(append([]Revision{}, prototype.Revisions...), revision)
			if err := validateRevisionGraph(&candidate, sagaID); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(stage, "revision.json"), revision, true)
		})
		if err != nil {
			return err
		}
		result = MutationResult{URN: revisionURN, Path: revisionPath(prototypeID, input.ID)}
		return nil
	})
	return result, err
}

func AddRepositoryStyle(root, sagaID string, input AddStyleInput) (MutationResult, error) {
	return AddStyle(root, sagaID, input)
}

func RefreshRepositoryStyle(root, sagaID string, input AddStyleInput) (MutationResult, error) {
	return AddStyle(root, sagaID, input)
}

func mutate(root, sagaID string, operation func(*Document) error) error {
	if _, err := Load(root, sagaID); err != nil {
		return fmt.Errorf("cannot mutate prototypes: %w", err)
	}
	return store.WithSagaLock(root, store.DefaultLockTimeout, func() error {
		doc, err := Load(root, sagaID)
		if err != nil {
			return fmt.Errorf("cannot mutate prototypes after locking: %w", err)
		}
		return operation(&doc)
	})
}

func copyHTMLSource(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("html source must not be a symlink")
	}
	if err := os.Mkdir(destination, 0o755); err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		if strings.ToLower(filepath.Ext(source)) != ".html" {
			return fmt.Errorf("single-file html source must have .html extension")
		}
		return copyRegularFile(source, filepath.Join(destination, "index.html"), info)
	}
	if !info.IsDir() {
		return fmt.Errorf("html source must be a regular .html file or real directory")
	}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("html source contains symlink %q", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !safeRelativePath(rel) {
			return fmt.Errorf("html source contains unsafe path %q", rel)
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.Mkdir(target, 0o755)
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() {
			return fmt.Errorf("html source contains non-regular file %q", rel)
		}
		return copyRegularFile(path, target, fileInfo)
	})
	if err != nil {
		return err
	}
	entrypoint, err := os.Lstat(filepath.Join(destination, "index.html"))
	if err != nil || entrypoint.Mode()&os.ModeSymlink != 0 || !entrypoint.Mode().IsRegular() {
		return fmt.Errorf("html source directory requires a regular index.html")
	}
	return nil
}

func digestHTMLInput(source string) (string, error) {
	temp, err := os.MkdirTemp("", ".change-saga-html-input-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	html := filepath.Join(temp, "html")
	if err := copyHTMLSource(source, html); err != nil {
		return "", err
	}
	return digestTree(html)
}

func copyRegularFile(source, destination string, info fs.FileInfo) error {
	if info.Size() > MaxHTMLFileBytes {
		return fmt.Errorf("html source file exceeds %d bytes", MaxHTMLFileBytes)
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(in, MaxHTMLFileBytes+1))
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func findPrototype(doc *Document, id string) *Prototype {
	for i := range doc.Prototypes {
		if doc.Prototypes[i].Identity.ID == id {
			return &doc.Prototypes[i]
		}
	}
	return nil
}
func mutationTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left, right := copyStrings(a), copyStrings(b)
	sort.Strings(left)
	sort.Strings(right)
	return reflect.DeepEqual(left, right)
}
func cloneAllowlist(value *ProviderAllowlist) *ProviderAllowlist {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Sandbox = copyStrings(value.Sandbox)
	copy.Permissions = copyStrings(value.Permissions)
	return &copy
}
func cloneSource(value Source) Source {
	value.Allowlist = cloneAllowlist(value.Allowlist)
	return value
}
func copyStrings(values []string) []string { return append([]string{}, values...) }
func copyStyles(values []StyleSource) []StyleSource {
	result := make([]StyleSource, len(values))
	for i, value := range values {
		result[i] = value
		result[i].CustomProperties = copyStrings(value.CustomProperties)
		result[i].Roles = append([]StyleRole{}, value.Roles...)
		result[i].Stale = false
		result[i].StaleReason = ""
	}
	return result
}
func equalPrototypeCreation(existing Prototype, identity Identity, revision Revision) bool {
	identity.CreatedAt = existing.Identity.CreatedAt
	if !reflect.DeepEqual(identity, existing.Identity) {
		return false
	}
	for _, stored := range existing.Revisions {
		if stored.ID != revision.ID {
			continue
		}
		revision.CreatedAt = stored.CreatedAt
		return reflect.DeepEqual(revision, stored)
	}
	return false
}
func equalExternalCreation(existing Prototype, identity Identity, revision Revision) bool {
	return equalPrototypeCreation(existing, identity, revision)
}

func revisionInputMatches(existing Revision, input ReviseInput) bool {
	source := cloneSource(input.Source)
	if input.HTMLSourcePath != "" && (source.Kind == "" || source.Kind == SourceHTML) {
		digest, err := digestHTMLInput(input.HTMLSourcePath)
		if err != nil {
			return false
		}
		source = Source{Kind: SourceHTML, Entrypoint: "html/index.html", ContentDigest: digest}
	}
	wanted := Revision{Schema: RevisionSchemaURL, Version: Version, ID: input.ID, Prototype: input.Prototype, Parents: copyStrings(input.Parents), Title: strings.TrimSpace(input.Title), State: input.State, Source: source, Styles: copyStyles(input.Styles), CreatedAt: existing.CreatedAt, RequestID: input.RequestID}
	return reflect.DeepEqual(existing, wanted)
}
func equalAnnotationIgnoringTime(existing, wanted Annotation) bool {
	wanted.CreatedAt = existing.CreatedAt
	return reflect.DeepEqual(existing, wanted)
}
func prototypePath(id string) string {
	return filepath.ToSlash(filepath.Join("___requirements", "prototypes", id+".prototype"))
}
func revisionPath(prototypeID, id string) string {
	return filepath.ToSlash(filepath.Join(prototypePath(prototypeID), "revisions", id+".revision"))
}
func annotationPath(prototypeID, id string) string {
	return filepath.ToSlash(filepath.Join("___requirements", "prototypes", "annotations", prototypeID+".prototype", id+".json"))
}
