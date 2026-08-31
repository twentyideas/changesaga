package livingapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/internal/livingid"
	"github.com/twentyideas/changesaga/internal/requirements"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/workplan"
)

type session struct {
	snapshot     string
	requirements requirements.Document
	plan         workplan.Plan
	saga         *saga.Saga
	adopted      bool
}

func Open(_ context.Context, options OpenOptions) (Session, error) {
	if strings.TrimSpace(options.SagaRoot) == "" {
		return nil, appError(CodeInvalidArgument, "saga_root is required", false, nil, nil)
	}
	root, err := filepath.Abs(options.SagaRoot)
	if err != nil {
		return nil, appError(CodeInvalidArgument, "saga_root could not be resolved", false, nil, err)
	}
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, appError(CodeNotFound, "saga was not found", false, map[string]any{"kind": "saga"}, err)
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, appError(CodeInvalidSaga, "the saga root could not be resolved safely", false, nil, err)
	}

	doc, sagaValidation, err := saga.Load(root)
	if err != nil {
		return nil, appError(CodeInvalidSaga, "the saga could not be loaded", false, nil, err)
	}
	if !sagaValidation.Valid {
		return nil, appError(CodeInvalidSaga, "the saga is invalid", false, map[string]any{"issues": sanitizeSagaIssues(sagaValidation.Issues)}, nil)
	}
	snapshot := options.Snapshot
	if snapshot == "" {
		snapshot, err = snapshotTree(root)
		if err != nil {
			return nil, appError(CodeInternal, "the session snapshot could not be created", false, nil, err)
		}
	}
	adopted := doc.Manifest.Version == saga.CurrentSagaVersion && livingRootsPresent(root)
	if doc.Manifest.Version != saga.CurrentSagaVersion {
		return &session{
			snapshot: snapshot, saga: doc, adopted: false,
			requirements: requirements.Document{Root: root, SagaID: doc.Manifest.ID, Stories: []requirements.Story{}, Citations: []requirements.Citation{}, Relations: []requirements.Relation{}},
			plan:         workplan.Plan{Root: root, SagaID: doc.Manifest.ID, Waves: map[string]*workplan.Wave{}, WorkItems: map[string]*workplan.WorkItem{}, Dependencies: map[string]*workplan.Dependency{}, Contracts: map[string]*workplan.Contract{}, Requests: map[string]workplan.RequestRecord{}, Conflicts: []workplan.Conflict{}},
		}, nil
	}

	plan, validation, err := workplan.Load(root)
	if err != nil {
		return nil, appError(CodeInvalidSaga, "the work plan could not be loaded", false, nil, err)
	}
	if !validation.Valid {
		return nil, appError(CodeInvalidSaga, "the work plan is invalid", false, map[string]any{"issues": sanitizePlanIssues(validation.Issues)}, nil)
	}
	stale := requirements.StaleInputs{CurrentRevisions: map[string]string{}, CurrentContentDigests: map[string]string{}, Missing: map[string]bool{}}
	for _, id := range sortedKeys(plan.WorkItems) {
		item := plan.WorkItems[id]
		if item.CurrentRevision != nil {
			stale.CurrentRevisions[item.CurrentRevision.WorkItem] = item.Heads[0]
		}
	}
	document, err := requirements.LoadWithOptions(root, plan.SagaID, requirements.LoadOptions{StaleInputs: stale})
	if err != nil {
		return nil, appError(CodeInvalidSaga, "the requirements could not be loaded", false, nil, err)
	}
	evaluateCrossDomainStaleness(&document, plan, doc)
	return &session{snapshot: snapshot, requirements: document, plan: plan, saga: doc, adopted: adopted}, nil
}

func livingRootsPresent(root string) bool {
	for _, name := range []string{"___requirements", "___design", "___workplan"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false
		}
	}
	return true
}

// evaluateCrossDomainStaleness completes the projection that requirements
// cannot compute without importing the work-plan and design domains.
func evaluateCrossDomainStaleness(document *requirements.Document, plan workplan.Plan, doc *saga.Saga) {
	designTargets := map[string]bool{}
	indexDesignTargets(doc.Section, designTargets)
	claims := map[string]bool{}
	for _, claim := range doc.Claims {
		claims["urn:change-saga:"+document.SagaID+":claim:"+claim.ID] = true
	}
	verifications := map[string]bool{}
	for _, verification := range doc.Verifications {
		verifications["urn:change-saga:"+document.SagaID+":verification:"+verification.ID] = true
	}
	for i := range document.Relations {
		relation := &document.Relations[i]
		if relation.State != requirements.RelationActive {
			continue
		}
		for _, endpoint := range []struct {
			name, urn, revision string
		}{{"from", relation.From, relation.FromRevision}, {"to", relation.To, relation.ToRevision}} {
			if ref, err := livingid.Parse(endpoint.urn); err == nil {
				switch ref.Kind {
				case livingid.KindWorkItem:
					item := plan.WorkItems[ref.ID]
					if item == nil {
						appendStaleReason(relation, endpoint.name+" work item is missing")
					} else if endpoint.revision != "" && len(item.Heads) != 1 {
						appendStaleReason(relation, endpoint.name+" work item has multiple revision heads")
					} else if endpoint.revision != "" && item.Heads[0] != endpoint.revision {
						appendStaleReason(relation, endpoint.name+" work-item revision changed")
					}
				case livingid.KindDesign:
					if !designTargets[endpoint.urn] {
						appendStaleReason(relation, endpoint.name+" design endpoint is missing")
					}
				}
				continue
			}
			if strings.Contains(endpoint.urn, ":claim:") && !claims[endpoint.urn] {
				appendStaleReason(relation, endpoint.name+" claim is missing")
			}
			if strings.Contains(endpoint.urn, ":verification:") && !verifications[endpoint.urn] {
				appendStaleReason(relation, endpoint.name+" verification is missing")
			}
		}
	}
}

func indexDesignTargets(section *saga.Section, targets map[string]bool) {
	if section == nil {
		return
	}
	targets[section.Target] = true
	for _, fragment := range section.Fragments {
		targets[fragment.Target] = true
		for _, landmark := range fragment.Landmarks {
			targets[landmark.Target] = true
		}
	}
	for _, child := range section.Children {
		indexDesignTargets(child, targets)
	}
}

func appendStaleReason(relation *requirements.Relation, reason string) {
	for _, existing := range relation.StaleReasons {
		if existing == reason {
			return
		}
	}
	relation.Stale = true
	relation.StaleReasons = append(relation.StaleReasons, reason)
	sort.Strings(relation.StaleReasons)
}

func (s *session) Snapshot() string { return s.snapshot }

func (s *session) Query(_ context.Context, query Query) (Result, error) {
	var rows any
	var key string
	switch query.Operation {
	case "requirements":
		values := s.requirementRows(query.Filters)
		rows, key = values, "requirements"
	case "requirement-history":
		values, err := s.historyRows(query.Filters)
		if err != nil {
			return Result{}, err
		}
		rows, key = values, "events"
	case "citations":
		values := s.citationRows(query.Filters)
		rows, key = values, "citations"
	case "relations":
		values := s.relationRows(query.Filters)
		rows, key = values, "relations"
	case "waves":
		values := s.waveRows(query.Filters)
		rows, key = values, "waves"
	case "work-items":
		values := s.workItemRows(query.Filters)
		rows, key = values, "items"
	case "work-events":
		values := s.workEventRows(query.Filters)
		rows, key = values, "events"
	case "work-conflicts":
		values := s.conflictRows(query.Filters)
		rows, key = values, "conflicts"
	case "traceability":
		values, _ := s.traceRows(query.Filters)
		rows, key = values, "criteria"
	case "readiness":
		return s.readinessResult(query)
	default:
		return Result{}, appError(CodeInvalidArgument, "unknown living query operation", false, nil, nil)
	}
	return s.pageRows(query, key, rows)
}

func (s *session) pageRows(query Query, key string, rows any) (Result, error) {
	total := sliceLength(rows)
	start, end, page, err := s.page(query.Operation, normalizedQueryKey(query.Filters), query.Cursor, query.Limit, total)
	if err != nil {
		return Result{}, err
	}
	sliced := sliceRange(rows, start, end)
	var data any
	switch key {
	case "requirements":
		data = RequirementPage{Requirements: sliced.([]Requirement)}
	case "events":
		if query.Operation == "requirement-history" {
			data = HistoryPage{Events: sliced.([]HistoryEvent)}
		} else {
			data = WorkEventPage{Events: sliced.([]WorkEvent)}
		}
	case "citations":
		data = CitationPage{Citations: sliced.([]Citation)}
	case "relations":
		data = RelationPage{Relations: sliced.([]Relation)}
	case "waves":
		data = WavePage{Waves: sliced.([]Wave)}
	case "items":
		data = WorkItemPage{Items: sliced.([]WorkItem)}
	case "conflicts":
		data = ConflictPage{Conflicts: sliced.([]Conflict)}
	case "criteria":
		data = TraceabilityPage{Criteria: sliced.([]Traceability)}
	}
	return Result{Data: data, Page: page}, nil
}

func (s *session) readinessResult(query Query) (Result, error) {
	pageData, all := s.readinessRows(query.Filters)
	start, end, page, err := s.page(query.Operation, normalizedQueryKey(query.Filters), query.Cursor, query.Limit, len(all))
	if err != nil {
		return Result{}, err
	}
	pageData.Requirements = all[start:end]
	return Result{Data: pageData, Page: page}, nil
}

type cursorToken struct {
	Version   int    `json:"v"`
	Operation string `json:"op"`
	Key       string `json:"key"`
	Snapshot  string `json:"snapshot"`
	Offset    int    `json:"offset"`
	Checksum  string `json:"checksum"`
}

func (s *session) page(operation, key, cursor string, limit, total int) (int, int, Page, error) {
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit < 1 || limit > MaxPageLimit {
		return 0, 0, Page{}, appError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaxPageLimit), false, nil, nil)
	}
	start := 0
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return 0, 0, Page{}, invalidCursor()
		}
		var token cursorToken
		decodeErr := json.Unmarshal(data, &token)
		canonical, marshalErr := json.Marshal(token)
		if decodeErr != nil || marshalErr != nil || !bytes.Equal(data, canonical) || len(token.Checksum) != sha256.Size*2 ||
			subtle.ConstantTimeCompare([]byte(token.Checksum), []byte(cursorChecksum(token))) != 1 || token.Version != 1 || token.Operation != operation || token.Key != key {
			return 0, 0, Page{}, invalidCursor()
		}
		if token.Snapshot != s.snapshot {
			return 0, 0, Page{}, appError(CodeStaleSnapshot, "the cursor belongs to a different snapshot", true, map[string]any{"expected": token.Snapshot, "actual": s.snapshot}, nil)
		}
		if token.Offset < 0 || token.Offset > total {
			return 0, 0, Page{}, invalidCursor()
		}
		start = token.Offset
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := Page{Total: total, Returned: end - start, HasMore: end < total}
	if page.HasMore {
		token := cursorToken{Version: 1, Operation: operation, Key: key, Snapshot: s.snapshot, Offset: end}
		token.Checksum = cursorChecksum(token)
		data, _ := json.Marshal(token)
		next := base64.RawURLEncoding.EncodeToString(data)
		page.NextCursor = &next
	}
	return start, end, page, nil
}

func cursorChecksum(token cursorToken) string {
	token.Checksum = ""
	data, _ := json.Marshal(token)
	digest := sha256.Sum256(append([]byte("change-saga-living-cursor-v1\x00"), data...))
	return hex.EncodeToString(digest[:])
}

func invalidCursor() error {
	return appError(CodeInvalidArgument, "cursor does not apply to this query", false, nil, nil)
}
func normalizedQueryKey(filters Filters) string {
	data, _ := json.Marshal(filters)
	return string(data)
}

func snapshotTree(root string) (string, error) {
	hash := sha256.New()
	_, _ = hash.Write([]byte("change-saga-livingapp-v1\x00"))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(rel)))
		_, _ = hash.Write([]byte{0})
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in saga")
		}
		if entry.IsDir() {
			_, _ = hash.Write([]byte("dir\x00"))
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special file in saga")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte("file\x00"))
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func appError(code ErrorCode, message string, retryable bool, details map[string]any, cause error) error {
	return &Error{Code: code, Message: message, Retryable: retryable, Details: details, cause: cause}
}

func sanitizePlanIssues(issues []workplan.Issue) []map[string]string {
	result := make([]map[string]string, 0, len(issues))
	for _, issue := range issues {
		result = append(result, map[string]string{"severity": issue.Severity, "path": filepath.ToSlash(issue.Path), "message": issue.Message})
	}
	return result
}
func sanitizeSagaIssues(issues []saga.Issue) []map[string]string {
	result := make([]map[string]string, 0, len(issues))
	for _, issue := range issues {
		result = append(result, map[string]string{"severity": issue.Severity, "path": filepath.ToSlash(issue.Path), "message": issue.Message})
	}
	return result
}

func sortedKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func sliceLength(rows any) int {
	switch v := rows.(type) {
	case []Requirement:
		return len(v)
	case []HistoryEvent:
		return len(v)
	case []Citation:
		return len(v)
	case []Relation:
		return len(v)
	case []Wave:
		return len(v)
	case []WorkItem:
		return len(v)
	case []WorkEvent:
		return len(v)
	case []Conflict:
		return len(v)
	case []Traceability:
		return len(v)
	}
	return 0
}
func sliceRange(rows any, start, end int) any {
	switch v := rows.(type) {
	case []Requirement:
		return v[start:end]
	case []HistoryEvent:
		return v[start:end]
	case []Citation:
		return v[start:end]
	case []Relation:
		return v[start:end]
	case []Wave:
		return v[start:end]
	case []WorkItem:
		return v[start:end]
	case []WorkEvent:
		return v[start:end]
	case []Conflict:
		return v[start:end]
	case []Traceability:
		return v[start:end]
	}
	return nil
}
