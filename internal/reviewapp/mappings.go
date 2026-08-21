package reviewapp

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/twentyideas/changesaga/internal/gitdiff"
)

type mappingAccumulator struct {
	assessment MappingAssessment
	atoms      map[string]gitdiff.Atom
	files      map[string]bool
	notes      map[string]bool
}

func (s *session) Mappings(ctx context.Context, query MappingQuery) (MappingPage, error) {
	if err := ctx.Err(); err != nil {
		return MappingPage{}, err
	}
	if query.Target != "" {
		if err := s.validateTargetArgument(query.Target); err != nil {
			return MappingPage{}, err
		}
		if s.targets[query.Target] == nil {
			return MappingPage{}, notFound("target", query.Target)
		}
	}
	if query.Sort == "" {
		query.Sort = "scrutiny"
	}
	if query.Sort != "scrutiny" && query.Sort != "target" && query.Sort != "path" {
		return MappingPage{}, invalidArgument("sort must be scrutiny, target, or path")
	}
	if query.MinimumScore < 0 || query.MinimumScore > 100 {
		return MappingPage{}, invalidArgument("minimum_score must be between 0 and 100")
	}

	items := s.mappingAssessments()
	filtered := make([]MappingAssessment, 0, len(items))
	for _, item := range items {
		if query.Target != "" && item.Target != query.Target || item.ScrutinyScore < query.MinimumScore {
			continue
		}
		filtered = append(filtered, item)
	}
	sortMappings(filtered, query.Sort)
	key := query.Target + "\x00" + query.Sort + "\x00" + strconv.Itoa(query.MinimumScore)
	start, end, page, err := s.page("mappings", key, query.Cursor, query.Limit, len(filtered))
	if err != nil {
		return MappingPage{}, err
	}
	return MappingPage{Mappings: append([]MappingAssessment{}, filtered[start:end]...), Page: page}, nil
}

func (s *session) mappingAssessments() []MappingAssessment {
	targetAtoms := map[string]map[string]gitdiff.Atom{}
	targetFiles := map[string]map[string]bool{}
	groups := map[string]*mappingAccumulator{}
	for target, selectors := range s.selectors {
		for _, entry := range selectors {
			key := target + "\x00" + entry.selector.EvidenceFile
			group := groups[key]
			if group == nil {
				kind := "unknown"
				if targetEntry := s.targets[target]; targetEntry != nil {
					kind = targetEntry.node.Kind
				}
				group = &mappingAccumulator{
					assessment: MappingAssessment{Target: target, TargetKind: kind, EvidenceFile: entry.selector.EvidenceFile, Notes: []string{}, Reasons: []MappingReason{}},
					atoms:      map[string]gitdiff.Atom{}, files: map[string]bool{}, notes: map[string]bool{},
				}
				groups[key] = group
			}
			group.assessment.SelectorCount++
			if len(entry.selector.Atoms) == 0 {
				group.assessment.StaleSelectorCount++
			}
			note := strings.TrimSpace(entry.selector.Note)
			group.notes[note] = true
			if targetAtoms[target] == nil {
				targetAtoms[target] = map[string]gitdiff.Atom{}
				targetFiles[target] = map[string]bool{}
			}
			for _, atom := range entry.selector.Atoms {
				group.atoms[atom.URI] = atom
				group.files[atomFilePath(atom)] = true
				targetAtoms[target][atom.URI] = atom
				targetFiles[target][atomFilePath(atom)] = true
			}
		}
	}

	items := make([]MappingAssessment, 0, len(groups))
	for _, group := range groups {
		item := group.assessment
		item.AtomCount = len(group.atoms)
		item.FileCount = len(group.files)
		item.TargetAtomCount = len(targetAtoms[item.Target])
		item.TargetFileCount = len(targetFiles[item.Target])
		if len(s.changes.Atoms) > 0 {
			item.ChangesetShare = math.Round(float64(item.TargetAtomCount)/float64(len(s.changes.Atoms))*10000) / 10000
		}
		for note := range group.notes {
			item.Notes = append(item.Notes, note)
		}
		sort.Strings(item.Notes)
		if len(item.Notes) > 0 {
			item.AtomsPerNote = math.Round(float64(item.AtomCount)/float64(len(item.Notes))*100) / 100
		}
		item.Reasons = mappingReasons(item)
		for _, reason := range item.Reasons {
			item.ScrutinyScore += reason.Weight
		}
		if item.ScrutinyScore > 100 {
			item.ScrutinyScore = 100
		}
		items = append(items, item)
	}
	return items
}

func mappingReasons(item MappingAssessment) []MappingReason {
	reasons := []MappingReason{}
	missingNote := false
	shortNote := false
	genericNote := false
	for _, note := range item.Notes {
		if note == "" {
			missingNote = true
		} else if len(strings.Fields(note)) < 4 {
			shortNote = true
		}
		normalized := strings.Trim(strings.ToLower(note), " .:-_")
		switch normalized {
		case "implementation", "implementation details", "supporting changes", "supporting implementation changes", "supporting implementation changes for this section", "tests", "code changes", "changes":
			genericNote = true
		}
	}
	if missingNote {
		reasons = append(reasons, MappingReason{Code: "missing_note", Weight: 35, Message: "at least one selector has no reviewer-facing justification"})
	} else if shortNote {
		reasons = append(reasons, MappingReason{Code: "thin_note", Weight: 10, Message: "the evidence note may be too terse to explain why these atoms belong together"})
	}
	if genericNote {
		reasons = append(reasons, MappingReason{Code: "generic_note", Weight: 20, Message: "the evidence note is generic and does not explain what changed or why this target owns it"})
	}
	if item.StaleSelectorCount > 0 {
		reasons = append(reasons, MappingReason{Code: "stale_selector", Weight: 50, Message: "at least one selector in this evidence record resolves to no current diff atoms"})
	}
	if item.AtomCount > 200 {
		reasons = append(reasons, MappingReason{Code: "very_broad_record", Weight: 35, Message: "one evidence record claims more than 200 atoms"})
	} else if item.AtomCount > 80 {
		reasons = append(reasons, MappingReason{Code: "broad_record", Weight: 20, Message: "one evidence record claims more than 80 atoms"})
	}
	if item.FileCount > 1 {
		reasons = append(reasons, MappingReason{Code: "multi_file_record", Weight: 20, Message: "one evidence record spans multiple source files"})
	}
	if item.TargetAtomCount > 200 {
		reasons = append(reasons, MappingReason{Code: "broad_target", Weight: 15, Message: "this narrative target owns more than 200 atoms in total"})
	}
	if item.TargetFileCount > 4 {
		reasons = append(reasons, MappingReason{Code: "many_files_per_target", Weight: 10, Message: "this narrative target owns changes in more than four files"})
	}
	if item.TargetAtomCount >= 100 && item.ChangesetShare >= .25 {
		reasons = append(reasons, MappingReason{Code: "dominant_target", Weight: 20, Message: "this target claims at least one quarter of the entire changeset"})
	}
	if item.TargetKind == "fragment" && item.AtomCount > 40 {
		reasons = append(reasons, MappingReason{Code: "prefer_landmarks", Weight: 10, Message: "a broad fragment-level mapping may be clearer when divided among semantic landmarks"})
	}
	return reasons
}

func sortMappings(items []MappingAssessment, order string) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		switch order {
		case "target":
			if left.Target != right.Target {
				return left.Target < right.Target
			}
		case "path":
			if left.EvidenceFile != right.EvidenceFile {
				return left.EvidenceFile < right.EvidenceFile
			}
		default:
			if left.ScrutinyScore != right.ScrutinyScore {
				return left.ScrutinyScore > right.ScrutinyScore
			}
			if left.AtomCount != right.AtomCount {
				return left.AtomCount > right.AtomCount
			}
		}
		if left.Target != right.Target {
			return left.Target < right.Target
		}
		return left.EvidenceFile < right.EvidenceFile
	})
}

func (s *session) mappingSignalIndex() map[string]MappingSignal {
	result := map[string]MappingSignal{}
	for _, item := range s.mappingAssessments() {
		result[item.Target+"\x00"+item.EvidenceFile] = MappingSignal{
			ScrutinyScore: item.ScrutinyScore, AtomCount: item.AtomCount, AtomsPerNote: item.AtomsPerNote, FileCount: item.FileCount,
			TargetAtomCount: item.TargetAtomCount, TargetFileCount: item.TargetFileCount, StaleSelectorCount: item.StaleSelectorCount,
			Reasons: append([]MappingReason{}, item.Reasons...),
		}
	}
	return result
}
