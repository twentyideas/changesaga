package requirements

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/livingid"
)

var contentDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type validationErrors struct{ values []error }

func (v *validationErrors) add(format string, args ...any) {
	v.values = append(v.values, fmt.Errorf(format, args...))
}

func (v *validationErrors) err() error {
	if len(v.values) == 0 {
		return nil
	}
	return errors.Join(v.values...)
}

func validateIdentity(value StoryIdentity, expectedID string) error {
	var problems validationErrors
	if value.Schema != StorySchemaURL {
		problems.add("$schema must be %q", StorySchemaURL)
	}
	if value.Version != Version {
		problems.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) || value.ID != expectedID {
		problems.add("story id must be stable and match its package name")
	}
	validateTime(&problems, value.CreatedAt)
	validateRequestID(&problems, value.RequestID)
	return problems.err()
}

func validateRevision(value Revision, sagaID, storyID string) error {
	var problems validationErrors
	if value.Schema != RevisionSchemaURL {
		problems.add("$schema must be %q", RevisionSchemaURL)
	}
	if value.Version != Version {
		problems.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) {
		problems.add("revision id is not a stable identifier")
	}
	wantStory, _ := storyURN(sagaID, storyID)
	if value.Story != wantStory {
		problems.add("story must be %q", wantStory)
	}
	if strings.TrimSpace(value.Title) == "" {
		problems.add("title is required")
	}
	if strings.TrimSpace(value.Statement) == "" {
		problems.add("statement is required")
	}
	if strings.TrimSpace(value.Priority) == "" || len(value.Priority) > 128 {
		problems.add("priority must contain 1-128 characters")
	}
	validateTime(&problems, value.CreatedAt)
	validateRequestID(&problems, value.RequestID)
	seenParents := map[string]bool{}
	for _, parent := range value.Parents {
		parentStory, err := parseStoryRevision(parent, sagaID)
		if err != nil || parentStory != storyID {
			problems.add("parent %q is not a revision of this story", parent)
		} else if seenParents[parent] {
			problems.add("parent %q is duplicated", parent)
		}
		seenParents[parent] = true
	}
	seenCitations := map[string]bool{}
	for _, citation := range value.Citations {
		ref, err := livingid.Parse(citation)
		if err != nil || ref.Kind != livingid.KindCitation || ref.SagaID != sagaID {
			problems.add("citation %q is not a canonical citation URN in this saga", citation)
		} else if seenCitations[citation] {
			problems.add("citation %q is duplicated", citation)
		}
		seenCitations[citation] = true
	}
	seenCriteria := map[string]bool{}
	for _, criterion := range value.AcceptanceCriteria {
		if !livingid.ValidID(criterion.ID) {
			problems.add("criterion id %q is not a stable identifier", criterion.ID)
		}
		if strings.TrimSpace(criterion.Statement) == "" {
			problems.add("criterion %q requires a statement", criterion.ID)
		}
		if seenCriteria[criterion.ID] {
			problems.add("criterion id %q is duplicated", criterion.ID)
		}
		seenCriteria[criterion.ID] = true
	}
	return problems.err()
}

func validateEvent(value LifecycleEvent, sagaID, storyID string) error {
	var problems validationErrors
	if value.Schema != LifecycleEventSchemaURL {
		problems.add("$schema must be %q", LifecycleEventSchemaURL)
	}
	if value.Version != Version {
		problems.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) {
		problems.add("event id is not a stable identifier")
	}
	wantStory, _ := storyURN(sagaID, storyID)
	if value.Story != wantStory {
		problems.add("story must be %q", wantStory)
	}
	if !validLifecycleState(value.State) {
		problems.add("state must be proposed, accepted, deferred, rejected, or retired")
	}
	validateTime(&problems, value.CreatedAt)
	validateRequestID(&problems, value.RequestID)
	seen := map[string]bool{}
	for _, parent := range value.Parents {
		if _, err := parseStoryEvent(parent, sagaID, storyID); err != nil {
			problems.add("parent %q %v", parent, err)
		} else if seen[parent] {
			problems.add("parent %q is duplicated", parent)
		}
		seen[parent] = true
	}
	return problems.err()
}

func validateCitation(value Citation, sagaID, expectedID string) error {
	var problems validationErrors
	if value.Schema != CitationSchemaURL {
		problems.add("$schema must be %q", CitationSchemaURL)
	}
	if value.Version != Version {
		problems.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) || value.ID != expectedID {
		problems.add("citation id must be stable and match its filename")
	}
	if !validCitationKind(value.Kind) {
		problems.add("citation kind is invalid")
	}
	if strings.TrimSpace(value.Title) == "" || strings.TrimSpace(value.Reference) == "" {
		problems.add("citation title and reference are required")
	}
	if value.Kind == CitationURL {
		parsed, err := url.Parse(value.Reference)
		if err != nil || !parsed.IsAbs() || parsed.Host == "" {
			problems.add("URL citation reference must be an absolute URL")
		}
	}
	validateTime(&problems, value.CreatedAt)
	validateRequestID(&problems, value.RequestID)
	_ = sagaID
	return problems.err()
}

func validateRelation(value Relation, sagaID, expectedID string) error {
	var problems validationErrors
	if value.Schema != RelationSchemaURL {
		problems.add("$schema must be %q", RelationSchemaURL)
	}
	if value.Version != Version {
		problems.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) || value.ID != expectedID {
		problems.add("relation id must be stable and match its filename")
	}
	if !validRelationType(value.Type) {
		problems.add("relation type is invalid")
	}
	from, fromErr := parseEndpoint(value.From)
	to, toErr := parseEndpoint(value.To)
	if fromErr != nil {
		problems.add("from endpoint: %v", fromErr)
	}
	if toErr != nil {
		problems.add("to endpoint: %v", toErr)
	}
	if fromErr == nil && from.SagaID != sagaID {
		problems.add("from endpoint belongs to a different saga")
	}
	if toErr == nil && to.SagaID != sagaID {
		problems.add("to endpoint belongs to a different saga")
	}
	if value.From == value.To {
		problems.add("self-relations are invalid")
	}
	if strings.TrimSpace(value.Rationale) == "" {
		problems.add("rationale is required")
	}
	if value.State != RelationActive && value.State != RelationSuperseded {
		problems.add("relation state must be active or superseded")
	}
	if value.State == RelationActive && (value.SupersededAt != nil || value.SupersedeRequestID != "") {
		problems.add("active relation cannot have supersede metadata")
	}
	if value.State == RelationSuperseded && value.SupersededAt == nil {
		problems.add("superseded relation requires superseded_at")
	}
	if value.SupersededAt != nil {
		validateTime(&problems, *value.SupersededAt)
	}
	validateTime(&problems, value.CreatedAt)
	validateRequestID(&problems, value.RequestID)
	validateRequestID(&problems, value.SupersedeRequestID)
	for name, digest := range map[string]string{"from_content_digest": value.FromContentDigest, "to_content_digest": value.ToContentDigest} {
		if digest != "" && !contentDigestPattern.MatchString(digest) {
			problems.add("%s must use sha256:<64 lowercase hex>", name)
		}
	}
	if fromErr == nil && toErr == nil {
		if value.Type == RelationConflictsWith && value.To < value.From {
			problems.add("conflicts_with endpoints must use canonical lexical order")
		}
		validateRelationMatrix(&problems, value, from, to)
		validateEndpointPins(&problems, "from", from, value.FromRevision, value.FromContentDigest)
		validateEndpointPins(&problems, "to", to, value.ToRevision, value.ToContentDigest)
	}
	return problems.err()
}

func validateRelationMatrix(problems *validationErrors, relation Relation, from, to endpoint) {
	require := func(ok bool, description string) {
		if !ok {
			problems.add("%s relation requires %s", relation.Type, description)
		}
	}
	requirement := func(kind endpointKind) bool { return kind == endpointStory || kind == endpointCriterion }
	switch relation.Type {
	case RelationRefines:
		require(requirement(from.Kind) && requirement(to.Kind), "story or criterion endpoints")
	case RelationAddresses:
		require(from.Kind == endpointDesign && requirement(to.Kind), "a design source and story or criterion target")
		require(relation.FromContentDigest != "" && relation.ToRevision != "", "a source content digest and target revision pin")
	case RelationImplements:
		require(from.Kind == endpointWorkItem && (to.Kind == endpointDesign || to.Kind == endpointCriterion), "a work-item source and design or criterion target")
		require(relation.FromRevision != "", "a source work-item revision pin")
		if to.Kind == endpointDesign {
			require(relation.ToContentDigest != "", "a target content digest")
		} else {
			require(relation.ToRevision != "", "a target criterion revision pin")
		}
	case RelationVerifies:
		require((from.Kind == endpointClaim || from.Kind == endpointVerification) && to.Kind == endpointCriterion, "a claim or verification source and criterion target")
		require(relation.ToRevision != "", "a target criterion revision pin")
	case RelationSupersedes:
		sameKind := from.Kind == to.Kind
		if from.Kind == endpointDesign && to.Kind == endpointDesign {
			sameKind = from.DesignKind == to.DesignKind
		}
		require(sameKind, "endpoints of the same resource kind")
	case RelationConflictsWith:
		require(requirement(from.Kind) && requirement(to.Kind), "compatible story or criterion endpoints")
	}
}

func validateEndpointPins(problems *validationErrors, side string, endpoint endpoint, revision, digest string) {
	switch endpoint.Kind {
	case endpointStory, endpointCriterion:
		if revision != "" {
			storyID, err := parseStoryRevision(revision, endpoint.SagaID)
			if err != nil || (endpoint.Kind == endpointStory && storyID != endpoint.ID) || (endpoint.Kind == endpointCriterion && storyID != endpoint.StoryID) {
				problems.add("%s_revision does not pin the endpoint's story", side)
			}
		}
		if digest != "" {
			problems.add("%s story or criterion endpoint cannot use a content digest", side)
		}
	case endpointDesign:
		if revision != "" {
			problems.add("%s design endpoint cannot use a definition revision", side)
		}
	case endpointWorkItem:
		if revision != "" {
			ref, err := livingid.Parse(revision)
			if err != nil || ref.Kind != livingid.KindRevision || ref.ParentKind != livingid.KindWorkItem || ref.SagaID != endpoint.SagaID || ref.ParentID != endpoint.ID {
				problems.add("%s_revision does not pin the work-item endpoint", side)
			}
		}
		if digest != "" {
			problems.add("%s work-item endpoint cannot use a content digest", side)
		}
	case endpointClaim, endpointVerification, endpointCitation, endpointRelation:
		if revision != "" || digest != "" {
			problems.add("%s immutable endpoint cannot carry a revision or content digest pin", side)
		}
	}
}

func validateStoryGraphs(story *Story, sagaID string, citationIDs map[string]bool) error {
	var problems validationErrors
	revisions := make(map[string]Revision, len(story.Revisions))
	for _, revision := range story.Revisions {
		if _, exists := revisions[revision.ID]; exists {
			problems.add("revision id %q is duplicated", revision.ID)
		}
		revisions[revision.ID] = revision
		for _, citation := range revision.Citations {
			ref, err := livingid.Parse(citation)
			if err == nil && !citationIDs[ref.ID] {
				problems.add("revision %q cites missing citation %q", revision.ID, citation)
			}
		}
	}
	revisionParents := map[string][]string{}
	for id, revision := range revisions {
		for _, parentURN := range revision.Parents {
			parentStory, err := parseStoryRevision(parentURN, sagaID)
			parts := strings.Split(parentURN, ":")
			if err != nil || parentStory != story.Identity.ID || len(parts) != 7 {
				continue
			}
			parentID := parts[6]
			if _, exists := revisions[parentID]; !exists {
				problems.add("revision %q names missing parent %q", id, parentURN)
			} else {
				revisionParents[id] = append(revisionParents[id], parentID)
			}
		}
	}
	revisionHeads, roots, cycle := graphHeads(revisionParents, mapKeys(revisions))
	if len(roots) != 1 {
		problems.add("revision graph must have exactly one initial revision; found %d", len(roots))
	}
	if cycle {
		problems.add("revision graph must be acyclic")
	}
	story.RevisionHeads = make([]string, 0, len(revisionHeads))
	for _, id := range revisionHeads {
		urn, _ := revisionURN(sagaID, story.Identity.ID, id)
		story.RevisionHeads = append(story.RevisionHeads, urn)
	}
	if len(revisionHeads) == 1 {
		value := revisions[revisionHeads[0]]
		story.CurrentRevision = &value
	}
	if !cycle {
		validateCriterionHistory(&problems, revisions, revisionParents)
	}

	events := make(map[string]LifecycleEvent, len(story.Events))
	for _, event := range story.Events {
		if _, exists := events[event.ID]; exists {
			problems.add("lifecycle event id %q is duplicated", event.ID)
		}
		events[event.ID] = event
	}
	eventParents := map[string][]string{}
	for id, event := range events {
		for _, parentURN := range event.Parents {
			parentID, err := parseStoryEvent(parentURN, sagaID, story.Identity.ID)
			if err != nil {
				continue
			}
			parent, exists := events[parentID]
			if !exists {
				problems.add("lifecycle event %q names missing parent %q", id, parentURN)
				continue
			}
			eventParents[id] = append(eventParents[id], parentID)
			if !allowedLifecycleTransition(parent.State, event.State, len(event.Parents) > 1) {
				problems.add("lifecycle event %q cannot transition from %s to %s", id, parent.State, event.State)
			}
		}
	}
	eventHeads, eventRoots, eventCycle := graphHeads(eventParents, mapKeys(events))
	if len(eventRoots) != 1 {
		problems.add("lifecycle graph must have exactly one initial event; found %d", len(eventRoots))
	} else if root := events[eventRoots[0]]; root.State != StateProposed {
		problems.add("initial lifecycle state must be proposed")
	}
	if eventCycle {
		problems.add("lifecycle graph must be acyclic")
	}
	story.LifecycleHeads = make([]string, 0, len(eventHeads))
	for _, id := range eventHeads {
		urn, _ := StoryEventURN(sagaID, story.Identity.ID, id)
		story.LifecycleHeads = append(story.LifecycleHeads, urn)
	}
	if len(eventHeads) == 1 {
		value := events[eventHeads[0]]
		story.CurrentLifecycle = &value
		if value.State == StateAccepted {
			for _, head := range revisionHeads {
				if len(revisions[head].AcceptanceCriteria) == 0 {
					problems.add("accepted story requires every current revision head to contain an acceptance criterion")
				}
			}
		}
	}
	return problems.err()
}

func validateCriterionHistory(problems *validationErrors, revisions map[string]Revision, parents map[string][]string) {
	memo := map[string]map[string]bool{}
	var ancestorCriteria func(string) map[string]bool
	ancestorCriteria = func(id string) map[string]bool {
		if known := memo[id]; known != nil {
			return known
		}
		result := map[string]bool{}
		for _, parentID := range parents[id] {
			for _, criterion := range revisions[parentID].AcceptanceCriteria {
				result[criterion.ID] = true
			}
			for criterionID := range ancestorCriteria(parentID) {
				result[criterionID] = true
			}
		}
		memo[id] = result
		return result
	}
	for id, revision := range revisions {
		if len(parents[id]) == 0 {
			continue
		}
		presentInParent := map[string]bool{}
		for _, parentID := range parents[id] {
			for _, criterion := range revisions[parentID].AcceptanceCriteria {
				presentInParent[criterion.ID] = true
			}
		}
		ancestors := ancestorCriteria(id)
		for _, criterion := range revision.AcceptanceCriteria {
			if ancestors[criterion.ID] && !presentInParent[criterion.ID] {
				problems.add("revision %q reuses removed criterion id %q", id, criterion.ID)
			}
		}
	}
}

func validateRelationSet(document *Document) error {
	var problems validationErrors
	knownStories := map[string]bool{}
	knownCriteria := map[string]bool{}
	knownRevisions := map[string]bool{}
	for _, story := range document.Stories {
		storyID, _ := storyURN(document.SagaID, story.Identity.ID)
		knownStories[storyID] = true
		for _, revision := range story.Revisions {
			revisionID, _ := revisionURN(document.SagaID, story.Identity.ID, revision.ID)
			knownRevisions[revisionID] = true
			for _, criterion := range revision.AcceptanceCriteria {
				criterionID, _ := criterionURN(document.SagaID, story.Identity.ID, criterion.ID)
				knownCriteria[criterionID] = true
			}
		}
	}
	activeConflictPairs := map[string]string{}
	graphs := map[RelationType]map[string][]string{RelationRefines: {}, RelationSupersedes: {}}
	for _, relation := range document.Relations {
		for _, endpoint := range []string{relation.From, relation.To} {
			parsed, err := parseEndpoint(endpoint)
			if err != nil {
				continue
			}
			if parsed.Kind == endpointStory && !knownStories[endpoint] {
				problems.add("relation %q references missing story %q", relation.ID, endpoint)
			}
			if parsed.Kind == endpointCriterion && !knownCriteria[endpoint] {
				problems.add("relation %q references unknown criterion %q", relation.ID, endpoint)
			}
		}
		for _, revision := range []string{relation.FromRevision, relation.ToRevision} {
			if revision != "" {
				if ref, err := parseEndpointRevision(revision); err == nil && ref == endpointStory && !knownRevisions[revision] {
					problems.add("relation %q references missing story revision %q", relation.ID, revision)
				}
			}
		}
		if relation.State != RelationActive {
			continue
		}
		if relation.Type == RelationConflictsWith {
			first, second := relation.From, relation.To
			if second < first {
				first, second = second, first
			}
			pair := first + "\x00" + second
			if prior := activeConflictPairs[pair]; prior != "" {
				problems.add("active conflicts_with relation %q duplicates %q", relation.ID, prior)
			} else {
				activeConflictPairs[pair] = relation.ID
			}
		}
		if graph, ok := graphs[relation.Type]; ok {
			graph[relation.From] = append(graph[relation.From], relation.To)
		}
	}
	for relationType, graph := range graphs {
		if directedCycle(graph) {
			problems.add("active %s relations must be acyclic", relationType)
		}
	}
	return problems.err()
}

func parseEndpointRevision(value string) (endpointKind, error) {
	ref, err := livingid.Parse(value)
	if err != nil || ref.Kind != livingid.KindRevision {
		return "", fmt.Errorf("not a definition revision")
	}
	if ref.ParentKind == "" || ref.ParentKind == livingid.KindStory {
		return endpointStory, nil
	}
	if ref.ParentKind == livingid.KindWorkItem {
		return endpointWorkItem, nil
	}
	return "", nil
}

func graphHeads(parents map[string][]string, ids []string) (heads, roots []string, cycle bool) {
	isParent := map[string]bool{}
	for _, id := range ids {
		if len(parents[id]) == 0 {
			roots = append(roots, id)
		}
		for _, parent := range parents[id] {
			isParent[parent] = true
		}
	}
	for _, id := range ids {
		if !isParent[id] {
			heads = append(heads, id)
		}
	}
	colors := map[string]uint8{}
	var visit func(string) bool
	visit = func(id string) bool {
		if colors[id] == 1 {
			return true
		}
		if colors[id] == 2 {
			return false
		}
		colors[id] = 1
		for _, parent := range parents[id] {
			if visit(parent) {
				return true
			}
		}
		colors[id] = 2
		return false
	}
	for _, id := range ids {
		cycle = cycle || visit(id)
	}
	sort.Strings(heads)
	sort.Strings(roots)
	return heads, roots, cycle
}

func directedCycle(graph map[string][]string) bool {
	colors := map[string]uint8{}
	var visit func(string) bool
	visit = func(node string) bool {
		if colors[node] == 1 {
			return true
		}
		if colors[node] == 2 {
			return false
		}
		colors[node] = 1
		for _, next := range graph[node] {
			if visit(next) {
				return true
			}
		}
		colors[node] = 2
		return false
	}
	for node := range graph {
		if visit(node) {
			return true
		}
	}
	return false
}

func mapKeys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}

func validateTime(problems *validationErrors, value time.Time) {
	if value.IsZero() || value.Location() != time.UTC {
		problems.add("timestamp must be a non-zero UTC RFC 3339 value")
	}
}

func validateRequestID(problems *validationErrors, value string) {
	if value != "" && !livingid.ValidID(value) {
		problems.add("request_id must be a stable identifier")
	}
}

func validLifecycleState(value LifecycleState) bool {
	switch value {
	case StateProposed, StateAccepted, StateDeferred, StateRejected, StateRetired:
		return true
	default:
		return false
	}
}

func allowedLifecycleTransition(from, to LifecycleState, reconciliation bool) bool {
	// A multi-parent event is an explicit decision that reconciles competing
	// heads. Requiring the chosen state to be reachable independently from every
	// head would make disagreements with terminal rejected/retired heads
	// impossible to resolve without erasing history.
	if reconciliation {
		return true
	}
	allowed := map[LifecycleState]map[LifecycleState]bool{
		StateProposed: {StateAccepted: true, StateDeferred: true, StateRejected: true},
		StateAccepted: {StateDeferred: true, StateRetired: true},
		StateDeferred: {StateProposed: true, StateAccepted: true, StateRejected: true, StateRetired: true},
	}
	return allowed[from][to]
}

func validCitationKind(value CitationKind) bool {
	switch value {
	case CitationURL, CitationRepositoryCommit, CitationIssue, CitationDocument, CitationDecision:
		return true
	default:
		return false
	}
}

func validRelationType(value RelationType) bool {
	switch value {
	case RelationRefines, RelationAddresses, RelationImplements, RelationVerifies, RelationSupersedes, RelationConflictsWith:
		return true
	default:
		return false
	}
}
