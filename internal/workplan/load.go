package workplan

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
)

// Load reads only saga.json and ___workplan. It never follows a symlink and
// never loads authored narrative/design bodies or diff atoms.
func Load(root string) (Plan, Validation, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Plan{}, Validation{}, err
	}
	validation := Validation{Valid: true, Issues: []Issue{}}
	plan := Plan{
		Root: abs, Waves: map[string]*Wave{}, WorkItems: map[string]*WorkItem{},
		Dependencies: map[string]*Dependency{}, Contracts: map[string]*Contract{},
		Requests: map[string]RequestRecord{}, Conflicts: []Conflict{},
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Plan{}, validation, fmt.Errorf("open saga: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Plan{}, validation, fmt.Errorf("saga root must be a real directory")
	}
	var manifest struct {
		Version int    `json:"version"`
		ID      string `json:"id"`
	}
	manifestPath := filepath.Join(abs, "saga.json")
	if err := readSelectedManifest(manifestPath, &manifest); err != nil {
		return Plan{}, validation, fmt.Errorf("read saga.json: %w", err)
	}
	plan.SagaID = manifest.ID
	if manifest.Version != Version {
		addIssue(&validation, "error", "saga.json", "work plans require a version 3 Saga")
	}
	if !validID(manifest.ID) {
		addIssue(&validation, "error", "saga.json", "saga id is invalid")
	}

	workplanRoot := filepath.Join(abs, RootDir)
	rootInfo, err := os.Lstat(workplanRoot)
	if errors.Is(err, fs.ErrNotExist) {
		validation.Valid = !hasErrors(validation)
		return plan, validation, nil
	}
	if err != nil {
		return Plan{}, validation, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		addIssue(&validation, "error", RootDir, "work-plan root must be a real directory")
		validation.Valid = false
		return plan, validation, nil
	}
	if err := scanWorkplanRoot(&plan, &validation, workplanRoot); err != nil {
		return Plan{}, validation, err
	}
	validatePlan(&plan, &validation)
	validation.Valid = !hasErrors(validation)
	return plan, validation, nil
}

func scanWorkplanRoot(plan *Plan, validation *Validation, root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	known := map[string]func() error{
		"waves":        func() error { return scanWaves(plan, validation, filepath.Join(root, "waves")) },
		"work-items":   func() error { return scanWorkItems(plan, validation, filepath.Join(root, "work-items")) },
		"dependencies": func() error { return scanDependencies(plan, validation, filepath.Join(root, "dependencies")) },
		"contracts":    func() error { return scanContracts(plan, validation, filepath.Join(root, "contracts")) },
		"events":       func() error { return scanEmptyCompatibilityEvents(plan, validation, filepath.Join(root, "events")) },
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		loader, ok := known[entry.Name()]
		if !ok {
			addIssue(validation, "error", relative(plan.Root, path), "unknown work-plan entry")
			continue
		}
		if !realDir(entry) {
			addIssue(validation, "error", relative(plan.Root, path), "work-plan collection must be a real directory")
			continue
		}
		if err := loader(); err != nil {
			return err
		}
	}
	return nil
}

func scanWaves(plan *Plan, validation *Validation, dir string) error {
	if err := enforceEntryLimit(dir, MaxWaves, "wave"); err != nil {
		return err
	}
	return scanPackages(plan, validation, dir, ".wave", func(packageDir, name string) error {
		var wave Wave
		if !readRecord(plan, validation, filepath.Join(packageDir, "wave.json"), &wave) {
			return nil
		}
		checkPackageID(plan, validation, packageDir, name, wave.ID)
		if _, exists := plan.Waves[wave.ID]; exists {
			addIssue(validation, "error", relative(plan.Root, packageDir), "duplicate wave id")
			return nil
		}
		wave.Revisions = []WaveRevision{}
		if err := scanRevisions(plan, validation, packageDir, func(path string) {
			var revision WaveRevision
			if readRecord(plan, validation, path, &revision) {
				wave.Revisions = append(wave.Revisions, revision)
				operation, resource := "wave-revision", revision.Wave+":revision:"+revision.ID
				if len(revision.Parents) == 0 {
					operation, resource = "wave-create", revision.Wave
				}
				registerRequest(plan, validation, relative(plan.Root, path), revision.RequestID, revision.RequestDigest, operation, resource, "")
			}
		}); err != nil {
			return err
		}
		plan.Waves[wave.ID] = &wave
		return ensureOnly(plan, validation, packageDir, map[string]string{"wave.json": "file", "revisions": "dir"})
	})
}

func scanWorkItems(plan *Plan, validation *Validation, dir string) error {
	if err := enforceEntryLimit(dir, MaxWorkItems, "work-item"); err != nil {
		return err
	}
	return scanPackages(plan, validation, dir, ".work-item", func(packageDir, name string) error {
		var item WorkItem
		if !readRecord(plan, validation, filepath.Join(packageDir, "work-item.json"), &item) {
			return nil
		}
		checkPackageID(plan, validation, packageDir, name, item.ID)
		if _, exists := plan.WorkItems[item.ID]; exists {
			addIssue(validation, "error", relative(plan.Root, packageDir), "duplicate work-item id")
			return nil
		}
		item.Revisions = []WorkItemRevision{}
		item.Progress = []ProgressEvent{}
		item.Workspaces = []WorkspaceEvent{}
		item.Merges = []MergeEvent{}
		if err := scanRevisions(plan, validation, packageDir, func(path string) {
			var revision WorkItemRevision
			if readRecord(plan, validation, path, &revision) {
				item.Revisions = append(item.Revisions, revision)
				operation, resource := "work-item-revision", revision.WorkItem+":revision:"+revision.ID
				if len(revision.Parents) == 0 {
					operation, resource = "work-item-create", revision.WorkItem
				}
				registerRequest(plan, validation, relative(plan.Root, path), revision.RequestID, revision.RequestDigest, operation, resource, "")
			}
		}); err != nil {
			return err
		}
		if err := scanItemEvents(plan, validation, packageDir, &item); err != nil {
			return err
		}
		plan.WorkItems[item.ID] = &item
		return ensureOnly(plan, validation, packageDir, map[string]string{"work-item.json": "file", "revisions": "dir", "events": "dir"})
	})
}

func scanItemEvents(plan *Plan, validation *Validation, packageDir string, item *WorkItem) error {
	eventsDir := filepath.Join(packageDir, "events")
	info, err := os.Lstat(eventsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		addIssue(validation, "error", relative(plan.Root, eventsDir), "events must be a real directory")
		return nil
	}
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(eventsDir, entry.Name())
		if !realDir(entry) {
			addIssue(validation, "error", relative(plan.Root, path), "event stream must be a real directory")
			continue
		}
		switch entry.Name() {
		case "progress":
			if err := scanEventFiles(plan, validation, path, func(eventPath string) {
				var event ProgressEvent
				if readRecord(plan, validation, eventPath, &event) {
					item.Progress = append(item.Progress, event)
					operation, resource := "progress", eventURNForWorkItem(event.WorkItem, "progress-event", event.ID)
					if len(event.Parents) == 0 && event.State == "planned" {
						operation, resource = "work-item-create", event.WorkItem
					}
					registerRequest(plan, validation, relative(plan.Root, eventPath), event.RequestID, event.RequestDigest, operation, resource, event.ID)
				}
			}); err != nil {
				return err
			}
		case "workspaces":
			if err := scanEventFiles(plan, validation, path, func(eventPath string) {
				var event WorkspaceEvent
				if readRecord(plan, validation, eventPath, &event) {
					item.Workspaces = append(item.Workspaces, event)
					registerRequest(plan, validation, relative(plan.Root, eventPath), event.RequestID, event.RequestDigest, "workspace", eventURNForWorkItem(event.WorkItem, "workspace-event", event.ID), event.ID)
				}
			}); err != nil {
				return err
			}
		case "merges":
			if err := scanEventFiles(plan, validation, path, func(eventPath string) {
				var event MergeEvent
				if readRecord(plan, validation, eventPath, &event) {
					item.Merges = append(item.Merges, event)
					registerRequest(plan, validation, relative(plan.Root, eventPath), event.RequestID, event.RequestDigest, "merge", eventURNForWorkItem(event.WorkItem, "merge-event", event.ID), event.ID)
				}
			}); err != nil {
				return err
			}
		default:
			addIssue(validation, "error", relative(plan.Root, path), "unknown work-item event stream")
		}
	}
	return nil
}

func scanDependencies(plan *Plan, validation *Validation, dir string) error {
	if err := enforceEntryLimit(dir, MaxDependencies, "dependency"); err != nil {
		return err
	}
	return scanPackages(plan, validation, dir, ".dependency", func(packageDir, name string) error {
		var dependency Dependency
		path := filepath.Join(packageDir, "dependency.json")
		if !readRecord(plan, validation, path, &dependency) {
			return nil
		}
		checkPackageID(plan, validation, packageDir, name, dependency.ID)
		if _, exists := plan.Dependencies[dependency.ID]; exists {
			addIssue(validation, "error", relative(plan.Root, path), "duplicate dependency id")
		} else {
			plan.Dependencies[dependency.ID] = &dependency
		}
		registerRequest(plan, validation, relative(plan.Root, path), dependency.RequestID, dependency.RequestDigest, "dependency-create", dependency.ID, "")
		return ensureOnly(plan, validation, packageDir, map[string]string{"dependency.json": "file"})
	})
}

func scanContracts(plan *Plan, validation *Validation, dir string) error {
	if err := enforceEntryLimit(dir, MaxContracts, "contract"); err != nil {
		return err
	}
	return scanPackages(plan, validation, dir, ".contract", func(packageDir, name string) error {
		var contract Contract
		if !readRecord(plan, validation, filepath.Join(packageDir, "contract.json"), &contract) {
			return nil
		}
		checkPackageID(plan, validation, packageDir, name, contract.ID)
		if _, exists := plan.Contracts[contract.ID]; exists {
			addIssue(validation, "error", relative(plan.Root, packageDir), "duplicate contract id")
			return nil
		}
		contract.Revisions = []ContractRevision{}
		contract.Events = []ContractEvent{}
		if err := scanRevisions(plan, validation, packageDir, func(path string) {
			var revision ContractRevision
			if readRecord(plan, validation, path, &revision) {
				contract.Revisions = append(contract.Revisions, revision)
				operation, resource := "contract-revision", revision.Contract+":revision:"+revision.ID
				if len(revision.Parents) == 0 {
					operation, resource = "contract-create", revision.Contract
				}
				registerRequest(plan, validation, relative(plan.Root, path), revision.RequestID, revision.RequestDigest, operation, resource, "")
			}
		}); err != nil {
			return err
		}
		eventsDir := filepath.Join(packageDir, "events")
		if info, err := os.Lstat(eventsDir); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				addIssue(validation, "error", relative(plan.Root, eventsDir), "events must be a real directory")
			} else if err := scanEventFiles(plan, validation, eventsDir, func(path string) {
				var event ContractEvent
				if readRecord(plan, validation, path, &event) {
					contract.Events = append(contract.Events, event)
					operation, resource := "contract-state", eventURNForContract(event.Contract, event.ID)
					if len(event.Parents) == 0 && event.State == "proposed" {
						operation, resource = "contract-create", event.Contract
					}
					registerRequest(plan, validation, relative(plan.Root, path), event.RequestID, event.RequestDigest, operation, resource, event.ID)
				}
			}); err != nil {
				return err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		plan.Contracts[contract.ID] = &contract
		return ensureOnly(plan, validation, packageDir, map[string]string{"contract.json": "file", "revisions": "dir", "events": "dir"})
	})
}

// The v3 format reserves a top-level events directory for future plan-global
// event kinds. Current assignment/progress/merge events stay item-local so an
// item and its initial progress can be published as one atomic package.
func scanEmptyCompatibilityEvents(plan *Plan, validation *Validation, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		addIssue(validation, "error", relative(plan.Root, filepath.Join(dir, entry.Name())), "unknown plan-global event")
	}
	return nil
}

func scanPackages(plan *Plan, validation *Validation, dir, suffix string, load func(string, string) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !strings.HasSuffix(entry.Name(), suffix) || !realDir(entry) {
			addIssue(validation, "error", relative(plan.Root, path), fmt.Sprintf("entries must be real *%s packages", suffix))
			continue
		}
		name := strings.TrimSuffix(entry.Name(), suffix)
		if !validID(name) {
			addIssue(validation, "error", relative(plan.Root, path), "package name is not a stable id")
			continue
		}
		if err := load(path, name); err != nil {
			return err
		}
	}
	return nil
}

func scanRevisions(plan *Plan, validation *Validation, packageDir string, load func(string)) error {
	dir := filepath.Join(packageDir, "revisions")
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		addIssue(validation, "error", relative(plan.Root, dir), "revisions must be a real directory")
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > MaxRevisionsPerResource {
		return fmt.Errorf("revision limit of %d exceeded", MaxRevisionsPerResource)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !strings.HasSuffix(entry.Name(), ".revision") || !realDir(entry) {
			addIssue(validation, "error", relative(plan.Root, path), "revisions must be real <id>.revision packages")
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".revision")
		if !validID(id) {
			addIssue(validation, "error", relative(plan.Root, path), "revision package name is invalid")
			continue
		}
		recordPath := filepath.Join(path, "revision.json")
		load(recordPath)
		if err := ensureOnly(plan, validation, path, map[string]string{"revision.json": "file"}); err != nil {
			return err
		}
	}
	return nil
}

func scanEventFiles(plan *Plan, validation *Validation, dir string, load func(string)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > MaxEventsPerStream {
		return fmt.Errorf("event limit of %d exceeded", MaxEventsPerStream)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !strings.HasSuffix(entry.Name(), ".json") || !realFile(entry) {
			addIssue(validation, "error", relative(plan.Root, path), "events must be regular <id>.json files")
			continue
		}
		load(path)
	}
	return nil
}

func ensureOnly(plan *Plan, validation *Validation, dir string, allowed map[string]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		kind, ok := allowed[entry.Name()]
		path := filepath.Join(dir, entry.Name())
		if !ok {
			addIssue(validation, "error", relative(plan.Root, path), "unknown package entry")
			continue
		}
		if kind == "dir" && !realDir(entry) {
			addIssue(validation, "error", relative(plan.Root, path), "entry must be a real directory")
		}
		if kind == "file" && !realFile(entry) {
			addIssue(validation, "error", relative(plan.Root, path), "entry must be a regular file")
		}
	}
	return nil
}

func readRecord(plan *Plan, validation *Validation, path string, value any) bool {
	if err := readStrictJSON(path, value); err != nil {
		addIssue(validation, "error", relative(plan.Root, path), err.Error())
		return false
	}
	if id := decodedRecordID(value); id != "" {
		if filepath.Base(path) == "revision.json" {
			name := strings.TrimSuffix(filepath.Base(filepath.Dir(path)), ".revision")
			if name != id {
				addIssue(validation, "error", relative(plan.Root, path), "revision id must match its package name")
			}
		} else if filepath.Ext(path) == ".json" && strings.Contains(filepath.ToSlash(path), "/events/") {
			name := strings.TrimSuffix(filepath.Base(path), ".json")
			if name != id {
				addIssue(validation, "error", relative(plan.Root, path), "event id must match its file name")
			}
		}
	}
	return true
}

func decodedRecordID(value any) string {
	switch record := value.(type) {
	case *WaveRevision:
		return record.ID
	case *WorkItemRevision:
		return record.ID
	case *ContractRevision:
		return record.ID
	case *ProgressEvent:
		return record.ID
	case *WorkspaceEvent:
		return record.ID
	case *MergeEvent:
		return record.ID
	case *ContractEvent:
		return record.ID
	default:
		return ""
	}
}

func readStrictJSON(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("record must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > MaxRecordBytes {
		return fmt.Errorf("record exceeds %d byte limit", MaxRecordBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("record contains trailing JSON data")
	}
	return nil
}

func readSelectedManifest(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("manifest must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func checkPackageID(plan *Plan, validation *Validation, dir, name, id string) {
	if name != id {
		addIssue(validation, "error", relative(plan.Root, dir), "record id must match package name")
	}
}

func registerRequest(plan *Plan, validation *Validation, path, id, digest, operation, resource, eventID string) {
	if id == "" {
		return
	}
	if !validRequestID(id) || !regexpDigest.MatchString(digest) {
		addIssue(validation, "error", path, "request id requires a stable id and request digest")
		return
	}
	record := RequestRecord{Operation: operation, Digest: digest, Resource: resource, EventID: eventID, Path: path}
	if prior, exists := plan.Requests[id]; exists {
		if prior.Operation == record.Operation && prior.Digest == record.Digest && prior.Resource == record.Resource && (prior.EventID == "" || record.EventID == "" || prior.EventID == record.EventID) {
			if prior.EventID == "" {
				prior.EventID = record.EventID
				plan.Requests[id] = prior
			}
		} else {
			addIssue(validation, "error", path, "request id is reused by a different mutation")
		}
		return
	}
	plan.Requests[id] = record
}

func realDir(entry fs.DirEntry) bool { return entry.Type()&fs.ModeSymlink == 0 && entry.IsDir() }
func realFile(entry fs.DirEntry) bool {
	return entry.Type()&fs.ModeSymlink == 0 && entry.Type().IsRegular()
}
func relative(root, path string) string {
	value, _ := filepath.Rel(root, path)
	return filepath.ToSlash(value)
}

func addIssue(validation *Validation, severity, path, message string) {
	validation.Issues = append(validation.Issues, Issue{Severity: severity, Path: path, Message: message})
}

func hasErrors(validation Validation) bool {
	for _, issue := range validation.Issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func enforceEntryLimit(dir string, limit int, kind string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > limit {
		return fmt.Errorf("%s limit of %d exceeded", kind, limit)
	}
	return nil
}
