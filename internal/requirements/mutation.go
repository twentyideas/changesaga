package requirements

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/livingid"
	"github.com/twentyideas/changesaga/internal/store"
)

func AddStory(root, sagaID string, input AddStoryInput) (MutationResult, error) {
	createdAt := mutationTime(input.CreatedAt)
	identity := StoryIdentity{Schema: StorySchemaURL, Version: Version, ID: input.ID, CreatedAt: createdAt, RequestID: input.RequestID}
	storyID, urnErr := storyURN(sagaID, input.ID)
	if urnErr != nil {
		return MutationResult{}, urnErr
	}
	revision := Revision{
		Schema: RevisionSchemaURL, Version: Version, ID: input.RevisionID, Story: storyID, Parents: []string{},
		Title: strings.TrimSpace(input.Title), Statement: strings.TrimSpace(input.Statement), Priority: strings.TrimSpace(input.Priority),
		Citations: copyStrings(input.Citations), AcceptanceCriteria: copyCriteria(input.AcceptanceCriteria), CreatedAt: createdAt, RequestID: input.RequestID,
	}
	event := LifecycleEvent{
		Schema: LifecycleEventSchemaURL, Version: Version, ID: input.EventID, Story: storyID, Parents: []string{},
		State: StateProposed, CreatedAt: createdAt, RequestID: input.RequestID,
	}
	if err := validateIdentity(identity, input.ID); err != nil {
		return MutationResult{}, err
	}
	if err := validateRevision(revision, sagaID, input.ID); err != nil {
		return MutationResult{}, err
	}
	if err := validateEvent(event, sagaID, input.ID); err != nil {
		return MutationResult{}, err
	}

	var result MutationResult
	err := mutate(root, sagaID, func(document *Document) error {
		for _, existing := range document.Stories {
			if existing.Identity.ID != input.ID {
				continue
			}
			if input.RequestID != "" && existing.Identity.RequestID == input.RequestID && equalStoryCreation(existing, identity, revision, event) {
				result = MutationResult{URN: storyID, Path: storyPackagePath(input.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("story id %q already exists", input.ID)
		}
		if len(document.Stories) >= MaxStories {
			return fmt.Errorf("story limit of %d reached", MaxStories)
		}
		if err := requireCitations(document, revision.Citations); err != nil {
			return err
		}
		storiesDir, err := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___requirements", "stories"))
		if err != nil {
			return err
		}
		final := filepath.Join(storiesDir, input.ID+".story")
		if err := store.CommitDir(document.Root, final, func(stage string) error {
			if err := store.WriteJSON(filepath.Join(stage, "story.json"), identity, true); err != nil {
				return err
			}
			revisionsDir := filepath.Join(stage, "revisions")
			eventsDir := filepath.Join(stage, "events")
			if err := ensureStageDir(revisionsDir); err != nil {
				return err
			}
			if err := ensureStageDir(eventsDir); err != nil {
				return err
			}
			if err := store.WriteJSON(filepath.Join(revisionsDir, revision.ID+".json"), revision, true); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(eventsDir, event.ID+".json"), event, true)
		}); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("story id %q already exists", input.ID)
		} else if err != nil {
			return err
		}
		result = MutationResult{URN: storyID, Path: storyPackagePath(input.ID)}
		return nil
	})
	return result, err
}

func ReviseStory(root, sagaID string, input ReviseStoryInput) (MutationResult, error) {
	storyRef, err := livingid.Parse(input.Story)
	if err != nil || storyRef.Kind != livingid.KindStory || storyRef.SagaID != sagaID {
		return MutationResult{}, fmt.Errorf("story must be a canonical story URN in saga %q", sagaID)
	}
	revision := Revision{
		Schema: RevisionSchemaURL, Version: Version, ID: input.ID, Story: input.Story, Parents: copyStrings(input.Parents),
		Title: strings.TrimSpace(input.Title), Statement: strings.TrimSpace(input.Statement), Priority: strings.TrimSpace(input.Priority),
		Citations: copyStrings(input.Citations), AcceptanceCriteria: copyCriteria(input.AcceptanceCriteria), CreatedAt: mutationTime(input.CreatedAt), RequestID: input.RequestID,
	}
	if err := validateRevision(revision, sagaID, storyRef.ID); err != nil {
		return MutationResult{}, err
	}
	revisionID, _ := revisionURN(sagaID, storyRef.ID, revision.ID)
	var result MutationResult
	err = mutate(root, sagaID, func(document *Document) error {
		story := findStory(document, storyRef.ID)
		if story == nil {
			return fmt.Errorf("story %q does not exist", storyRef.ID)
		}
		for _, existing := range story.Revisions {
			if existing.ID != revision.ID {
				continue
			}
			if input.RequestID != "" && existing.RequestID == input.RequestID && equalRevisionIgnoringTime(existing, revision) {
				result = MutationResult{URN: revisionID, Path: revisionPath(storyRef.ID, revision.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("revision id %q already exists", revision.ID)
		}
		if len(story.Revisions) >= MaxRevisionsPerStory {
			return fmt.Errorf("revision limit of %d reached", MaxRevisionsPerStory)
		}
		if !sameSet(revision.Parents, story.RevisionHeads) {
			return fmt.Errorf("revision parents must name every current head (got %v, want %v)", revision.Parents, story.RevisionHeads)
		}
		if err := requireCitations(document, revision.Citations); err != nil {
			return err
		}
		candidate := *story
		candidate.Revisions = append(append([]Revision{}, story.Revisions...), revision)
		citationIDs := map[string]bool{}
		for _, citation := range document.Citations {
			citationIDs[citation.ID] = true
		}
		if err := validateStoryGraphs(&candidate, sagaID, citationIDs); err != nil {
			return err
		}
		path := filepath.Join(document.Root, filepath.FromSlash(revisionPath(storyRef.ID, revision.ID)))
		if err := store.WriteJSON(path, revision, true); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("revision id %q already exists", revision.ID)
		} else if err != nil {
			return err
		}
		result = MutationResult{URN: revisionID, Path: revisionPath(storyRef.ID, revision.ID)}
		return nil
	})
	return result, err
}

func SetStoryState(root, sagaID string, input SetStoryStateInput) (MutationResult, error) {
	storyRef, err := livingid.Parse(input.Story)
	if err != nil || storyRef.Kind != livingid.KindStory || storyRef.SagaID != sagaID {
		return MutationResult{}, fmt.Errorf("story must be a canonical story URN in saga %q", sagaID)
	}
	event := LifecycleEvent{
		Schema: LifecycleEventSchemaURL, Version: Version, ID: input.ID, Story: input.Story, Parents: copyStrings(input.Parents),
		State: input.State, Reason: strings.TrimSpace(input.Reason), CreatedAt: mutationTime(input.CreatedAt), RequestID: input.RequestID,
	}
	if err := validateEvent(event, sagaID, storyRef.ID); err != nil {
		return MutationResult{}, err
	}
	eventURN, _ := StoryEventURN(sagaID, storyRef.ID, event.ID)
	var result MutationResult
	err = mutate(root, sagaID, func(document *Document) error {
		story := findStory(document, storyRef.ID)
		if story == nil {
			return fmt.Errorf("story %q does not exist", storyRef.ID)
		}
		for _, existing := range story.Events {
			if existing.ID != event.ID {
				continue
			}
			if input.RequestID != "" && existing.RequestID == input.RequestID && equalEventIgnoringTime(existing, event) {
				result = MutationResult{URN: eventURN, Path: eventPath(storyRef.ID, event.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("lifecycle event id %q already exists", event.ID)
		}
		if len(story.Events) >= MaxEventsPerStory {
			return fmt.Errorf("lifecycle event limit of %d reached", MaxEventsPerStory)
		}
		if !sameSet(event.Parents, story.LifecycleHeads) {
			return fmt.Errorf("lifecycle parents must name every current head (got %v, want %v)", event.Parents, story.LifecycleHeads)
		}
		candidate := *story
		candidate.Events = append(append([]LifecycleEvent{}, story.Events...), event)
		citationIDs := map[string]bool{}
		for _, citation := range document.Citations {
			citationIDs[citation.ID] = true
		}
		if err := validateStoryGraphs(&candidate, sagaID, citationIDs); err != nil {
			return err
		}
		path := filepath.Join(document.Root, filepath.FromSlash(eventPath(storyRef.ID, event.ID)))
		if err := store.WriteJSON(path, event, true); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("lifecycle event id %q already exists", event.ID)
		} else if err != nil {
			return err
		}
		result = MutationResult{URN: eventURN, Path: eventPath(storyRef.ID, event.ID)}
		return nil
	})
	return result, err
}

func AddCitation(root, sagaID string, input AddCitationInput) (MutationResult, error) {
	value := Citation{
		Schema: CitationSchemaURL, Version: Version, ID: input.ID, Kind: input.Kind,
		Title: strings.TrimSpace(input.Title), Reference: strings.TrimSpace(input.Reference),
		CreatedAt: mutationTime(input.CreatedAt), RequestID: input.RequestID,
	}
	if err := validateCitation(value, sagaID, input.ID); err != nil {
		return MutationResult{}, err
	}
	urn, _ := citationURN(sagaID, input.ID)
	var result MutationResult
	err := mutate(root, sagaID, func(document *Document) error {
		for _, existing := range document.Citations {
			if existing.ID != input.ID {
				continue
			}
			if input.RequestID != "" && existing.RequestID == input.RequestID && equalCitationIgnoringTime(existing, value) {
				result = MutationResult{URN: urn, Path: citationPath(input.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("citation id %q already exists; citations are immutable", input.ID)
		}
		if len(document.Citations) >= MaxCitations {
			return fmt.Errorf("citation limit of %d reached", MaxCitations)
		}
		dir, err := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___requirements", "citations"))
		if err != nil {
			return err
		}
		path := filepath.Join(dir, input.ID+".json")
		if err := store.WriteJSON(path, value, true); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("citation id %q already exists; citations are immutable", input.ID)
		} else if err != nil {
			return err
		}
		result = MutationResult{URN: urn, Path: citationPath(input.ID)}
		return nil
	})
	return result, err
}

func AddRelation(root, sagaID string, input AddRelationInput) (MutationResult, error) {
	if input.Type == RelationConflictsWith && input.To < input.From {
		input.From, input.To = input.To, input.From
		input.FromRevision, input.ToRevision = input.ToRevision, input.FromRevision
		input.FromContentDigest, input.ToContentDigest = input.ToContentDigest, input.FromContentDigest
	}
	value := Relation{
		Schema: RelationSchemaURL, Version: Version, ID: input.ID, Type: input.Type,
		From: input.From, To: input.To, Rationale: strings.TrimSpace(input.Rationale),
		FromRevision: input.FromRevision, ToRevision: input.ToRevision,
		FromContentDigest: input.FromContentDigest, ToContentDigest: input.ToContentDigest,
		State: RelationActive, CreatedAt: mutationTime(input.CreatedAt), RequestID: input.RequestID,
	}
	if err := validateRelation(value, sagaID, input.ID); err != nil {
		return MutationResult{}, err
	}
	urn, _ := relationURN(sagaID, input.ID)
	var result MutationResult
	err := mutate(root, sagaID, func(document *Document) error {
		for _, existing := range document.Relations {
			if existing.ID != input.ID {
				continue
			}
			if input.RequestID != "" && existing.RequestID == input.RequestID && equalRelationIgnoringTime(existing, value) {
				result = MutationResult{URN: urn, Path: relationPath(input.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("relation id %q already exists", input.ID)
		}
		if len(document.Relations) >= MaxRelations {
			return fmt.Errorf("relation limit of %d reached", MaxRelations)
		}
		candidate := *document
		candidate.Relations = append(append([]Relation{}, document.Relations...), value)
		if err := validateRelationSet(&candidate); err != nil {
			return err
		}
		dir, err := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___requirements", "relations"))
		if err != nil {
			return err
		}
		path := filepath.Join(dir, input.ID+".json")
		if err := store.WriteJSON(path, value, true); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("relation id %q already exists", input.ID)
		} else if err != nil {
			return err
		}
		result = MutationResult{URN: urn, Path: relationPath(input.ID)}
		return nil
	})
	return result, err
}

// SupersedeRelation explicitly deactivates one relation without retargeting it.
// The replacement is atomic, serialized, and preserves all endpoint pins.
func SupersedeRelation(root, sagaID, relation string, at time.Time, requestID string) (MutationResult, error) {
	ref, err := livingid.Parse(relation)
	if err != nil || ref.Kind != livingid.KindRelation || ref.SagaID != sagaID {
		return MutationResult{}, fmt.Errorf("relation must be a canonical relation URN in saga %q", sagaID)
	}
	if requestID != "" && !livingid.ValidID(requestID) {
		return MutationResult{}, fmt.Errorf("request_id must be a stable identifier")
	}
	var result MutationResult
	err = mutate(root, sagaID, func(document *Document) error {
		var existing *Relation
		for index := range document.Relations {
			if document.Relations[index].ID == ref.ID {
				existing = &document.Relations[index]
				break
			}
		}
		if existing == nil {
			return fmt.Errorf("relation %q does not exist", ref.ID)
		}
		if existing.State == RelationSuperseded {
			if requestID != "" && existing.SupersedeRequestID == requestID {
				result = MutationResult{URN: relation, Path: relationPath(ref.ID), Replayed: true}
				return nil
			}
			return fmt.Errorf("relation %q is already superseded", ref.ID)
		}
		when := mutationTime(at)
		existing.State = RelationSuperseded
		existing.SupersededAt = &when
		existing.SupersedeRequestID = requestID
		if err := validateRelation(*existing, sagaID, ref.ID); err != nil {
			return err
		}
		path := filepath.Join(document.Root, filepath.FromSlash(relationPath(ref.ID)))
		if err := store.WriteJSON(path, *existing, false); err != nil {
			return err
		}
		result = MutationResult{URN: relation, Path: relationPath(ref.ID)}
		return nil
	})
	return result, err
}

func mutate(root, sagaID string, operation func(*Document) error) error {
	if _, err := Load(root, sagaID); err != nil {
		return fmt.Errorf("cannot mutate requirements: %w", err)
	}
	return store.WithSagaLock(root, store.DefaultLockTimeout, func() error {
		document, err := Load(root, sagaID)
		if err != nil {
			return fmt.Errorf("cannot mutate requirements after locking: %w", err)
		}
		return operation(&document)
	})
}

func requireCitations(document *Document, citations []string) error {
	known := map[string]bool{}
	for _, citation := range document.Citations {
		urn, _ := citationURN(document.SagaID, citation.ID)
		known[urn] = true
	}
	for _, citation := range citations {
		if !known[citation] {
			return fmt.Errorf("citation %q does not exist", citation)
		}
	}
	return nil
}

func findStory(document *Document, id string) *Story {
	for index := range document.Stories {
		if document.Stories[index].Identity.ID == id {
			return &document.Stories[index]
		}
	}
	return nil
}

func sameSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := copyStrings(left), copyStrings(right)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func mutationTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func ensureStageDir(path string) error {
	// CommitDir owns and cleans the staging tree; these two fixed subdirectories
	// cannot escape it and do not need the saga-root path builder.
	return os.Mkdir(path, 0o755)
}

func storyPackagePath(id string) string {
	return filepath.ToSlash(filepath.Join("___requirements", "stories", id+".story"))
}
func revisionPath(storyID, id string) string {
	return filepath.ToSlash(filepath.Join(storyPackagePath(storyID), "revisions", id+".json"))
}
func eventPath(storyID, id string) string {
	return filepath.ToSlash(filepath.Join(storyPackagePath(storyID), "events", id+".json"))
}
func citationPath(id string) string {
	return filepath.ToSlash(filepath.Join("___requirements", "citations", id+".json"))
}
func relationPath(id string) string {
	return filepath.ToSlash(filepath.Join("___requirements", "relations", id+".json"))
}

func copyStrings(values []string) []string        { return append([]string{}, values...) }
func copyCriteria(values []Criterion) []Criterion { return append([]Criterion{}, values...) }

func equalStoryCreation(existing Story, identity StoryIdentity, revision Revision, event LifecycleEvent) bool {
	identity.CreatedAt = existing.Identity.CreatedAt
	if !reflect.DeepEqual(existing.Identity, identity) {
		return false
	}
	var storedRevision *Revision
	for index := range existing.Revisions {
		if existing.Revisions[index].ID == revision.ID {
			storedRevision = &existing.Revisions[index]
			break
		}
	}
	var storedEvent *LifecycleEvent
	for index := range existing.Events {
		if existing.Events[index].ID == event.ID {
			storedEvent = &existing.Events[index]
			break
		}
	}
	if storedRevision == nil || storedEvent == nil {
		return false
	}
	revision.CreatedAt = storedRevision.CreatedAt
	event.CreatedAt = storedEvent.CreatedAt
	return reflect.DeepEqual(*storedRevision, revision) && reflect.DeepEqual(*storedEvent, event)
}

func equalRevisionIgnoringTime(existing, wanted Revision) bool {
	wanted.CreatedAt = existing.CreatedAt
	return reflect.DeepEqual(existing, wanted)
}

func equalEventIgnoringTime(existing, wanted LifecycleEvent) bool {
	wanted.CreatedAt = existing.CreatedAt
	return reflect.DeepEqual(existing, wanted)
}

func equalCitationIgnoringTime(existing, wanted Citation) bool {
	wanted.CreatedAt = existing.CreatedAt
	return reflect.DeepEqual(existing, wanted)
}

func equalRelationIgnoringTime(existing, wanted Relation) bool {
	wanted.CreatedAt = existing.CreatedAt
	existing.State = RelationActive
	existing.SupersededAt = nil
	existing.SupersedeRequestID = ""
	existing.Stale = false
	existing.StaleReasons = nil
	return reflect.DeepEqual(existing, wanted)
}
