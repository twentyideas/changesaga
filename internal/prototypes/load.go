package prototypes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/internal/livingid"
)

type sagaIdentity struct {
	Schema  string          `json:"$schema,omitempty"`
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	PR      json.RawMessage `json:"pr,omitempty"`
	Source  json.RawMessage `json:"source"`
}

func Load(root, sagaID string) (Document, error) { return LoadWithOptions(root, sagaID, LoadOptions{}) }

// LoadWithOptions reads only saga.json and ___requirements/prototypes. It
// never follows symlinks or opens stories, narrative, design, work-plan,
// review, or diff data.
func LoadWithOptions(root, sagaID string, options LoadOptions) (Document, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Document{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Document{}, fmt.Errorf("open saga: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Document{}, fmt.Errorf("saga root must be a real directory")
	}
	manifestPath := filepath.Join(abs, "saga.json")
	var manifest sagaIdentity
	if err := readStrictJSON(manifestPath, &manifest); err != nil {
		return Document{}, fmt.Errorf("read saga.json: %w", err)
	}
	if manifest.Version != Version {
		return Document{}, fmt.Errorf("prototypes require a format v3 saga")
	}
	if !livingid.ValidID(manifest.ID) {
		return Document{}, fmt.Errorf("saga.json contains an invalid saga id")
	}
	if sagaID == "" {
		sagaID = manifest.ID
	}
	if sagaID != manifest.ID {
		return Document{}, fmt.Errorf("requested saga id %q does not match saga.json id %q", sagaID, manifest.ID)
	}
	doc := Document{Root: abs, SagaID: sagaID, Prototypes: []Prototype{}, Annotations: []Annotation{}}
	requirementsRoot := filepath.Join(abs, "___requirements")
	present, err := realDirectory(requirementsRoot)
	if err != nil {
		return Document{}, err
	}
	if !present {
		return doc, nil
	}
	prototypeRoot := filepath.Join(requirementsRoot, "prototypes")
	present, err = realDirectory(prototypeRoot)
	if err != nil {
		return Document{}, err
	}
	if !present {
		return doc, nil
	}
	doc.Adopted = true
	entries, err := boundedReadDir(prototypeRoot, MaxPrototypes+1)
	if err != nil {
		return Document{}, err
	}
	for _, entry := range entries {
		path := filepath.Join(prototypeRoot, entry.Name())
		if entry.Type()&fs.ModeSymlink != 0 {
			return Document{}, fmt.Errorf("prototype root contains symlink %q", entry.Name())
		}
		if entry.Name() == "annotations" {
			if !entry.IsDir() {
				return Document{}, fmt.Errorf("prototype annotations must be a real directory")
			}
			if err := loadAnnotations(&doc, path); err != nil {
				return Document{}, err
			}
			continue
		}
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".prototype") {
			return Document{}, fmt.Errorf("prototype entry %q must be a real <id>.prototype directory", entry.Name())
		}
		id := strings.TrimSuffix(entry.Name(), ".prototype")
		if !livingid.ValidID(id) {
			return Document{}, fmt.Errorf("prototype package %q has an invalid id", entry.Name())
		}
		prototype, err := loadPrototypePackage(&doc, path, id, options)
		if err != nil {
			return Document{}, err
		}
		doc.Prototypes = append(doc.Prototypes, prototype)
	}
	if len(doc.Prototypes) > MaxPrototypes {
		return Document{}, fmt.Errorf("prototype limit of %d exceeded", MaxPrototypes)
	}
	sort.Slice(doc.Prototypes, func(i, j int) bool { return doc.Prototypes[i].Identity.ID < doc.Prototypes[j].Identity.ID })
	sort.Slice(doc.Annotations, func(i, j int) bool {
		if doc.Annotations[i].Prototype == doc.Annotations[j].Prototype {
			return doc.Annotations[i].ID < doc.Annotations[j].ID
		}
		return doc.Annotations[i].Prototype < doc.Annotations[j].Prototype
	})
	return doc, nil
}

func loadPrototypePackage(doc *Document, dir, id string, options LoadOptions) (Prototype, error) {
	entries, err := boundedReadDir(dir, 2)
	if err != nil {
		return Prototype{}, err
	}
	foundIdentity, foundRevisions := false, false
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 {
			return Prototype{}, fmt.Errorf("prototype %q contains symlink %q", id, entry.Name())
		}
		switch entry.Name() {
		case "prototype.json":
			if !entry.Type().IsRegular() {
				return Prototype{}, fmt.Errorf("prototype %q identity must be a regular file", id)
			}
			foundIdentity = true
		case "revisions":
			if !entry.IsDir() {
				return Prototype{}, fmt.Errorf("prototype %q revisions must be a real directory", id)
			}
			foundRevisions = true
		default:
			return Prototype{}, fmt.Errorf("prototype %q contains unknown entry %q", id, entry.Name())
		}
	}
	if !foundIdentity || !foundRevisions {
		return Prototype{}, fmt.Errorf("prototype %q requires prototype.json and revisions", id)
	}
	var prototype Prototype
	identityPath := filepath.Join(dir, "prototype.json")
	if err := readStrictJSON(identityPath, &prototype.Identity); err != nil {
		return Prototype{}, fmt.Errorf("%s: %w", relative(doc.Root, identityPath), err)
	}
	if err := validateIdentity(prototype.Identity, id); err != nil {
		return Prototype{}, fmt.Errorf("%s: %w", relative(doc.Root, identityPath), err)
	}
	prototype.Revisions, err = loadRevisions(doc, filepath.Join(dir, "revisions"), id, options)
	if err != nil {
		return Prototype{}, err
	}
	if len(prototype.Revisions) == 0 {
		return Prototype{}, fmt.Errorf("prototype %q requires at least one revision", id)
	}
	if err := validateRevisionGraph(&prototype, doc.SagaID); err != nil {
		return Prototype{}, fmt.Errorf("prototype %q: %w", id, err)
	}
	return prototype, nil
}

func loadRevisions(doc *Document, dir, prototypeID string, options LoadOptions) ([]Revision, error) {
	entries, err := boundedReadDir(dir, MaxRevisionsPerPrototype)
	if err != nil {
		return nil, err
	}
	values := make([]Revision, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".revision") {
			return nil, fmt.Errorf("prototype %q revision entry %q must be a real <id>.revision directory", prototypeID, entry.Name())
		}
		id := strings.TrimSuffix(entry.Name(), ".revision")
		if !livingid.ValidID(id) {
			return nil, fmt.Errorf("prototype %q has invalid revision package %q", prototypeID, entry.Name())
		}
		packageDir := filepath.Join(dir, entry.Name())
		revision, err := loadRevisionPackage(doc, packageDir, prototypeID, id, options)
		if err != nil {
			return nil, err
		}
		values = append(values, revision)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values, nil
}

func loadRevisionPackage(doc *Document, dir, prototypeID, id string, options LoadOptions) (Revision, error) {
	entries, err := boundedReadDir(dir, 2)
	if err != nil {
		return Revision{}, err
	}
	foundRecord, foundHTML := false, false
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 {
			return Revision{}, fmt.Errorf("revision %q contains symlink %q", id, entry.Name())
		}
		switch entry.Name() {
		case "revision.json":
			if !entry.Type().IsRegular() {
				return Revision{}, fmt.Errorf("revision %q record must be regular", id)
			}
			foundRecord = true
		case "html":
			if !entry.IsDir() {
				return Revision{}, fmt.Errorf("revision %q html must be a real directory", id)
			}
			foundHTML = true
		default:
			return Revision{}, fmt.Errorf("revision %q contains unknown entry %q", id, entry.Name())
		}
	}
	if !foundRecord {
		return Revision{}, fmt.Errorf("revision %q is missing revision.json", id)
	}
	var value Revision
	recordPath := filepath.Join(dir, "revision.json")
	if err := readStrictJSON(recordPath, &value); err != nil {
		return Revision{}, fmt.Errorf("%s: %w", relative(doc.Root, recordPath), err)
	}
	if value.ID != id {
		return Revision{}, fmt.Errorf("%s: revision id must match package name", relative(doc.Root, recordPath))
	}
	if err := validateRevision(value, doc.SagaID, prototypeID); err != nil {
		return Revision{}, fmt.Errorf("%s: %w", relative(doc.Root, recordPath), err)
	}
	if value.Source.Kind == SourceHTML {
		if !foundHTML {
			return Revision{}, fmt.Errorf("revision %q html source is missing its package", id)
		}
		digest, err := digestTree(filepath.Join(dir, "html"))
		if err != nil {
			return Revision{}, fmt.Errorf("revision %q: %w", id, err)
		}
		if digest != value.Source.ContentDigest {
			return Revision{}, fmt.Errorf("revision %q html content digest mismatch", id)
		}
		entrypoint := filepath.Join(dir, filepath.FromSlash(value.Source.Entrypoint))
		entryInfo, err := os.Lstat(entrypoint)
		if err != nil || entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
			return Revision{}, fmt.Errorf("revision %q html entrypoint is missing or unsafe", id)
		}
	} else if foundHTML {
		return Revision{}, fmt.Errorf("revision %q external source cannot contain an html package", id)
	}
	if options.RepositoryRoot != "" {
		for i := range value.Styles {
			_, digest, err := readRepositoryStyle(options.RepositoryRoot, value.Styles[i].Path)
			if err != nil {
				value.Styles[i].Stale = true
				value.Styles[i].StaleReason = err.Error()
			} else if digest != value.Styles[i].Digest {
				value.Styles[i].Stale = true
				value.Styles[i].StaleReason = "stylesheet digest changed"
			}
		}
	}
	return value, nil
}

func loadAnnotations(doc *Document, dir string) error {
	entries, err := boundedReadDir(dir, MaxAnnotations)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	count := 0
	for _, packageEntry := range entries {
		if packageEntry.Type()&fs.ModeSymlink != 0 || !packageEntry.IsDir() || !strings.HasSuffix(packageEntry.Name(), ".prototype") {
			return fmt.Errorf("annotation entry %q must be a real <prototype-id>.prototype directory", packageEntry.Name())
		}
		prototypeID := strings.TrimSuffix(packageEntry.Name(), ".prototype")
		if !livingid.ValidID(prototypeID) {
			return fmt.Errorf("annotation package %q has an invalid prototype id", packageEntry.Name())
		}
		annotationEntries, err := boundedReadDir(filepath.Join(dir, packageEntry.Name()), MaxAnnotations-count)
		if err != nil {
			return err
		}
		for _, entry := range annotationEntries {
			count++
			if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
				return fmt.Errorf("annotation entry %q must be a real JSON file", entry.Name())
			}
			path := filepath.Join(dir, packageEntry.Name(), entry.Name())
			var value Annotation
			if err := readStrictJSON(path, &value); err != nil {
				return fmt.Errorf("%s: %w", relative(doc.Root, path), err)
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if err := validateAnnotation(value, doc.SagaID, id); err != nil {
				return fmt.Errorf("%s: %w", relative(doc.Root, path), err)
			}
			valuePrototypeID, _ := parsePrototypeURN(value.Prototype, doc.SagaID)
			if valuePrototypeID != prototypeID {
				return fmt.Errorf("%s: annotation prototype must match its package", relative(doc.Root, path))
			}
			urn, _ := AnnotationURN(doc.SagaID, prototypeID, value.ID)
			if seen[urn] {
				return fmt.Errorf("duplicate annotation %q", urn)
			}
			seen[urn] = true
			doc.Annotations = append(doc.Annotations, value)
		}
	}
	return nil
}

func realDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("%s must be a real directory", path)
	}
	return true, nil
}
func boundedReadDir(path string, maximum int) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	if len(entries) > maximum {
		return nil, fmt.Errorf("%s contains %d entries; maximum is %d", path, len(entries), maximum)
	}
	return entries, nil
}
func readStrictJSON(path string, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a real regular file")
	}
	if info.Size() > MaxRecordBytes {
		return fmt.Errorf("record is %d bytes; maximum is %d", info.Size(), MaxRecordBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(data), MaxRecordBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("record must contain exactly one JSON value")
		}
		return err
	}
	return nil
}
func relative(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(value)
}
