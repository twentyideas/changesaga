package prototypes

import "sort"

// Compose resolves the prototype domain against a caller-supplied requirements
// snapshot. Missing endpoints and stale pins become quality gaps, never
// structural load errors. The returned edge list is the one source for both
// prototype-to-story and story-to-prototype projections.
func Compose(document Document, inputs CompositionInputs) CoverageProjection {
	projection := CoverageProjection{Capability: "not_applicable", Annotations: []AnnotationProjection{}, Prototypes: []ResourceCoverage{}, Stories: []ResourceCoverage{}, Gaps: []QualityGap{}}
	if !document.Adopted {
		for _, story := range inputs.Stories {
			projection.Stories = append(projection.Stories, ResourceCoverage{Resource: story.URN, Status: "not_applicable", Annotations: []string{}})
		}
		sortCoverage(&projection)
		return projection
	}
	projection.Capability = "adopted"
	type prototypeState struct {
		revision, digest string
		source           SourceKind
		ready, conflict  bool
	}
	prototypes := map[string]prototypeState{}
	for _, prototype := range document.Prototypes {
		urn, _ := PrototypeURN(document.SagaID, prototype.Identity.ID)
		state := prototypeState{conflict: prototype.CurrentRevision == nil}
		if prototype.CurrentRevision != nil {
			state.revision, _ = RevisionURN(document.SagaID, prototype.Identity.ID, prototype.CurrentRevision.ID)
			state.digest = prototype.CurrentRevision.Source.ContentDigest
			state.source = prototype.CurrentRevision.Source.Kind
			state.ready = prototype.CurrentRevision.State == StateReady
		}
		prototypes[urn] = state
	}
	targetRevision := map[string]string{}
	targetStory := map[string]string{}
	required := map[string]bool{}
	knownStories := map[string]bool{}
	for _, story := range inputs.Stories {
		knownStories[story.URN] = true
		targetRevision[story.URN] = story.CurrentRevision
		targetStory[story.URN] = story.URN
		required[story.URN] = story.PrototypeRequired
		for _, criterion := range story.Criteria {
			targetRevision[criterion] = story.CurrentRevision
			targetStory[criterion] = story.URN
		}
	}
	prototypeLinks := map[string][]string{}
	storyLinks := map[string][]string{}
	currentPrototypeLinks := map[string]int{}
	currentStoryLinks := map[string]int{}
	for _, annotation := range document.Annotations {
		item := AnnotationProjection{Annotation: annotation, StaleReasons: []string{}}
		pstate, pok := prototypes[annotation.Prototype]
		item.PrototypeResolved = pok
		currentStoryRevision, tok := targetRevision[annotation.Target]
		item.TargetResolved = tok
		annotationURN := annotationURNUnchecked(document.SagaID, annotation)
		prototypeLinks[annotation.Prototype] = append(prototypeLinks[annotation.Prototype], annotationURN)
		owner := targetStory[annotation.Target]
		if owner == "" {
			if storyID, _, err := parseTargetURN(annotation.Target, document.SagaID); err == nil {
				candidate := "urn:change-saga:" + document.SagaID + ":story:" + storyID
				if knownStories[candidate] {
					owner = candidate
				}
			}
		}
		if owner != "" {
			storyLinks[owner] = append(storyLinks[owner], annotationURN)
		}
		if !pok {
			item.StaleReasons = append(item.StaleReasons, "prototype endpoint is unresolved")
			projection.Gaps = append(projection.Gaps, QualityGap{Kind: "unresolved_prototype", Resource: annotationURN, Message: "annotation prototype endpoint is unresolved"})
		} else {
			if pstate.conflict {
				item.StaleReasons = append(item.StaleReasons, "prototype has multiple revision heads")
			} else if annotation.PrototypeRevision != "" && annotation.PrototypeRevision != pstate.revision {
				item.StaleReasons = append(item.StaleReasons, "prototype revision changed")
			} else if annotation.PrototypeContentDigest != "" && annotation.PrototypeContentDigest != pstate.digest {
				item.StaleReasons = append(item.StaleReasons, "prototype content digest changed")
			}
			if !selectorCompatible(annotation.Selector.Kind, pstate.source) {
				item.StaleReasons = append(item.StaleReasons, "selector is incompatible with prototype source")
				projection.Gaps = append(projection.Gaps, QualityGap{Kind: "selector_incompatible", Resource: annotationURN, Message: "annotation selector is incompatible with the current prototype source"})
			}
		}
		if !tok {
			item.StaleReasons = append(item.StaleReasons, "story or criterion endpoint is unresolved")
			projection.Gaps = append(projection.Gaps, QualityGap{Kind: "unresolved_requirement", Resource: annotationURN, Message: "annotation story or criterion endpoint is unresolved"})
		} else if currentStoryRevision == "" {
			item.StaleReasons = append(item.StaleReasons, "story has multiple or unavailable revision heads")
		} else if annotation.StoryRevision != currentStoryRevision {
			item.StaleReasons = append(item.StaleReasons, "story revision changed")
		}
		item.Current = pok && tok && len(item.StaleReasons) == 0
		if item.Current {
			currentPrototypeLinks[annotation.Prototype]++
			currentStoryLinks[owner]++
		}
		projection.Annotations = append(projection.Annotations, item)
	}
	for _, prototype := range document.Prototypes {
		urn, _ := PrototypeURN(document.SagaID, prototype.Identity.ID)
		status := "linked"
		state := prototypes[urn]
		if state.conflict {
			status = "conflict"
		} else if currentPrototypeLinks[urn] == 0 {
			status = "unlinked"
		}
		if state.ready && currentPrototypeLinks[urn] == 0 {
			projection.Gaps = append(projection.Gaps, QualityGap{Kind: "ready_prototype_unlinked", Resource: urn, Message: "ready prototype has no current story or criterion annotation"})
		}
		projection.Prototypes = append(projection.Prototypes, ResourceCoverage{Resource: urn, Status: status, Annotations: sortedCopy(prototypeLinks[urn])})
	}
	for _, story := range inputs.Stories {
		status := "not_applicable"
		if story.PrototypeRequired {
			if currentStoryLinks[story.URN] > 0 {
				status = "covered"
			} else {
				status = "missing"
				projection.Gaps = append(projection.Gaps, QualityGap{Kind: "prototype_required", Resource: story.URN, Message: "story requires a current prototype annotation"})
			}
		}
		projection.Stories = append(projection.Stories, ResourceCoverage{Resource: story.URN, Status: status, Annotations: sortedCopy(storyLinks[story.URN])})
	}
	sortCoverage(&projection)
	return projection
}

// ProjectCoverage is a naming alias suited to query adapters.
func ProjectCoverage(document Document, inputs CompositionInputs) CoverageProjection {
	return Compose(document, inputs)
}

func selectorCompatible(selector SelectorKind, source SourceKind) bool {
	if source == SourceHTML {
		return selector == SelectorElement || selector == SelectorText || selector == SelectorRegion
	}
	return selector == SelectorProvider || selector == SelectorRegion
}
func annotationURNUnchecked(sagaID string, a Annotation) string {
	prototypeID, _ := parsePrototypeURN(a.Prototype, sagaID)
	value, _ := AnnotationURN(sagaID, prototypeID, a.ID)
	return value
}
func sortedCopy(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	return result
}
func sortCoverage(value *CoverageProjection) {
	sort.Slice(value.Annotations, func(i, j int) bool {
		left, right := value.Annotations[i].Annotation, value.Annotations[j].Annotation
		if left.Prototype == right.Prototype {
			return left.ID < right.ID
		}
		return left.Prototype < right.Prototype
	})
	sort.Slice(value.Prototypes, func(i, j int) bool { return value.Prototypes[i].Resource < value.Prototypes[j].Resource })
	sort.Slice(value.Stories, func(i, j int) bool { return value.Stories[i].Resource < value.Stories[j].Resource })
	sort.Slice(value.Gaps, func(i, j int) bool {
		if value.Gaps[i].Resource == value.Gaps[j].Resource {
			return value.Gaps[i].Kind < value.Gaps[j].Kind
		}
		return value.Gaps[i].Resource < value.Gaps[j].Resource
	})
}
