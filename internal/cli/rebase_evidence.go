package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/requirements"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

type evidenceRebaseOutput struct {
	OK                   bool                `json:"ok"`
	Action               string              `json:"action"`
	DryRun               bool                `json:"dry_run"`
	Repository           string              `json:"repository"`
	FromBase             string              `json:"from_base"`
	ToBase               string              `json:"to_base"`
	ProductIdentity      string              `json:"product_identity"`
	Atoms                int                 `json:"atoms"`
	EvidenceFiles        []string            `json:"evidence_files"`
	Selectors            int                 `json:"selectors"`
	Claims               int                 `json:"claims"`
	VerificationsCarried int                 `json:"verifications_carried"`
	ClaimsLeftUnverified int                 `json:"claims_left_unverified"`
	ClaimReplacements    []claimRebaseOutput `json:"claim_replacements"`
}

type claimRebaseOutput struct {
	Previous     string `json:"previous"`
	Replacement  string `json:"replacement"`
	Supersedes   string `json:"supersedes_relation"`
	Verification string `json:"verification,omitempty"`
}

type plannedEvidenceRebase struct {
	Relative string
	Value    saga.DiffFile
}

type plannedClaimRebase struct {
	Previous     saga.Claim
	Replacement  saga.Claim
	Relation     requirements.Relation
	Verification *saga.Verification
}

type evidenceRebasePlan struct {
	Output   evidenceRebaseOutput
	Evidence []plannedEvidenceRebase
	Claims   []plannedClaimRebase
}

var evidenceRebaseFaultHook func(string) error

// RebaseEvidence refreshes selector base identities only after the current
// comparison proves that its base-independent product identity is unchanged.
func RebaseEvidence(ctx context.Context, args []string, out io.Writer) error {
	err := rebaseEvidence(ctx, args, out)
	if err != nil && jsonFlagRequested(args) {
		return reportJSONMutationFailure(out, err)
	}
	return err
}

func rebaseEvidence(ctx context.Context, args []string, out io.Writer) error {
	flags := commandFlags("rebase-evidence", commandUsage["rebase-evidence"], out)
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	fromBase := flags.String("from-base", "", "expected old exact base OID; detected when omitted")
	carryVerifications := flags.Bool("carry-verifications", false, "append equivalent verification results with an explicit analysis audit trail")
	dryRun := flags.Bool("dry-run", false, "prove equivalence and report every write without changing the Saga")
	jsonOutput := flags.Bool("json", false, "emit one machine-readable result")
	quiet := flags.Bool("quiet", false, "suppress successful output")
	allowRepositoryMismatch := flags.Bool("allow-repository-mismatch", false, "use a checkout whose origin differs from the declared repository")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["rebase-evidence"])
	}
	if *jsonOutput && *quiet {
		return fmt.Errorf("--json and --quiet cannot be combined")
	}

	root := flags.Arg(0)
	document, validation, err := saga.Load(root)
	if err != nil {
		return err
	}
	if !validation.Valid {
		return fmt.Errorf("cannot rebase evidence in a Saga that does not validate")
	}
	checkout := firstNonEmpty(*repoDir, document.Root)
	changes, err := gitdiff.ReadWithOptions(ctx, checkout, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head, gitdiff.ReadOptions{AllowRepositoryMismatch: *allowRepositoryMismatch})
	if err != nil {
		return fmt.Errorf("read refreshed source diff (use --repo for a separate saga repository): %w", err)
	}
	at := time.Now().UTC()
	if *dryRun {
		plan, planErr := buildEvidenceRebasePlan(document, changes, strings.TrimSpace(*fromBase), *carryVerifications, at)
		if planErr != nil {
			return planErr
		}
		plan.Output.DryRun = true
		return writeEvidenceRebaseResult(out, plan.Output, *jsonOutput, *quiet)
	}

	var result evidenceRebaseOutput
	err = store.WithSagaLock(root, store.DefaultLockTimeout, func() error {
		locked, lockedValidation, loadErr := saga.Load(root)
		if loadErr != nil {
			return loadErr
		}
		if !lockedValidation.Valid {
			return fmt.Errorf("cannot rebase evidence in a Saga that does not validate")
		}
		plan, planErr := buildEvidenceRebasePlan(locked, changes, strings.TrimSpace(*fromBase), *carryVerifications, at)
		if planErr != nil {
			return planErr
		}
		if applyErr := applyEvidenceRebase(locked, plan); applyErr != nil {
			return applyErr
		}
		result = plan.Output
		return nil
	})
	if err != nil {
		return err
	}
	return writeEvidenceRebaseResult(out, result, *jsonOutput, *quiet)
}

func buildEvidenceRebasePlan(document *saga.Saga, changes gitdiff.ChangeSet, expectedBase string, carryVerifications bool, at time.Time) (evidenceRebasePlan, error) {
	currentAtoms := make([]diffuri.Reference, 0, len(changes.Atoms))
	for _, atom := range changes.Atoms {
		reference, err := diffuri.Parse(atom.URI)
		if err != nil {
			return evidenceRebasePlan{}, fmt.Errorf("current product atom %q is invalid: %w", atom.Key, err)
		}
		currentAtoms = append(currentAtoms, reference)
	}
	translator := evidenceTranslator{changes: changes, atoms: currentAtoms, oldBases: map[string]bool{}, expectedBase: expectedBase}
	plan := evidenceRebasePlan{Output: evidenceRebaseOutput{
		OK: true, Action: "rebase-evidence", Repository: changes.Repository, ToBase: changes.BaseOID,
		ProductIdentity: changes.HeadOID, Atoms: len(changes.Atoms), EvidenceFiles: []string{}, ClaimReplacements: []claimRebaseOutput{},
	}}
	supersededClaims, err := loadSupersededClaims(document)
	if err != nil {
		return evidenceRebasePlan{}, err
	}

	for _, file := range collectDiffFiles(document.Section) {
		updated := file
		updated.Diffs = append([]saga.DiffReference{}, file.Diffs...)
		changed := false
		for index := range updated.Diffs {
			uri, translated, err := translator.translate(updated.Diffs[index].URI, "evidence "+file.Path)
			if err != nil {
				return evidenceRebasePlan{}, err
			}
			if translated {
				updated.Diffs[index].URI = uri
				plan.Output.Selectors++
				changed = true
			}
		}
		if changed {
			plan.Evidence = append(plan.Evidence, plannedEvidenceRebase{Relative: file.Path, Value: updated})
			plan.Output.EvidenceFiles = append(plan.Output.EvidenceFiles, file.Path)
		}
	}

	for _, claim := range document.Claims {
		if supersededClaims[claim.ID] {
			continue
		}
		evidence := append([]string{}, claim.Evidence...)
		changed := false
		for index := range evidence {
			uri, translated, err := translator.translate(evidence[index], "claim "+claim.ID)
			if err != nil {
				return evidenceRebasePlan{}, err
			}
			if translated {
				evidence[index] = uri
				changed = true
			}
		}
		if !changed {
			continue
		}
		if document.Manifest.Version != saga.CurrentSagaVersion {
			if document.Manifest.Version == saga.SlideSagaVersion {
				return evidenceRebasePlan{}, fmt.Errorf("claim %q needs immutable rollover, but v4 claim-supersession records are not yet part of the flat format; refusing to rewrite claim evidence", claim.ID)
			}
			return evidenceRebasePlan{}, fmt.Errorf("claim %q needs immutable rollover; run change-saga upgrade --to 3 first so the replacement can supersede it", claim.ID)
		}
		claimID, err := generatedRecordID("claim")
		if err != nil {
			return evidenceRebasePlan{}, err
		}
		relationID, err := generatedRecordID("relation")
		if err != nil {
			return evidenceRebasePlan{}, err
		}
		replacement := saga.Claim{
			Version: saga.CurrentVersion, ID: claimID, Target: claim.Target, Kind: claim.Kind,
			Statement: claim.Statement, Evidence: evidence, CreatedAt: at,
		}
		fromURN := fmt.Sprintf("urn:change-saga:%s:claim:%s", document.Manifest.ID, claimID)
		toURN := fmt.Sprintf("urn:change-saga:%s:claim:%s", document.Manifest.ID, claim.ID)
		relation := requirements.Relation{
			Schema: requirements.RelationSchemaURL, Version: requirements.Version, ID: relationID,
			Type: requirements.RelationSupersedes, From: fromURN, To: toURN,
			Rationale: fmt.Sprintf("Evidence rebased from %s to %s after proving unchanged product identity %s.", translator.singleOldBase(), changes.BaseOID, changes.HeadOID),
			State:     requirements.RelationActive, CreatedAt: at.Add(time.Nanosecond),
		}
		claimPlan := plannedClaimRebase{Previous: claim, Replacement: replacement, Relation: relation}
		output := claimRebaseOutput{Previous: claim.ID, Replacement: claimID, Supersedes: relationID}
		if carryVerifications {
			if previous := latestVerification(document.Verifications, claim.ID); previous != nil {
				verificationID, idErr := generatedRecordID("verification")
				if idErr != nil {
					return evidenceRebasePlan{}, idErr
				}
				method := "analysis"
				verification := &saga.Verification{
					Version: saga.CurrentVersion, ID: verificationID, Claim: claimID, Status: previous.Status, Method: method,
					Summary:   fmt.Sprintf("Carried forward %s result from verification %s after rebase-evidence proved unchanged product identity %s. Previous summary: %s", previous.Status, previous.ID, changes.HeadOID, previous.Summary),
					CreatedAt: at.Add(2 * time.Nanosecond),
				}
				claimPlan.Verification = verification
				output.Verification = verificationID
				plan.Output.VerificationsCarried++
			}
		}
		if claimPlan.Verification == nil {
			plan.Output.ClaimsLeftUnverified++
		}
		plan.Claims = append(plan.Claims, claimPlan)
		plan.Output.ClaimReplacements = append(plan.Output.ClaimReplacements, output)
	}

	if len(translator.oldBases) == 0 {
		return evidenceRebasePlan{}, fmt.Errorf("no evidence uses an older base with current product identity %s", changes.HeadOID)
	}
	if len(translator.oldBases) != 1 {
		bases := make([]string, 0, len(translator.oldBases))
		for base := range translator.oldBases {
			bases = append(bases, base)
		}
		sort.Strings(bases)
		return evidenceRebasePlan{}, fmt.Errorf("evidence spans multiple old bases (%s); reconcile the cohorts before running one bulk migration", strings.Join(bases, ", "))
	}
	plan.Output.FromBase = translator.singleOldBase()
	plan.Output.Claims = len(plan.Claims)
	sort.Strings(plan.Output.EvidenceFiles)
	return plan, nil
}

func loadSupersededClaims(document *saga.Saga) (map[string]bool, error) {
	result := map[string]bool{}
	if document.Manifest.Version != saga.CurrentSagaVersion {
		return result, nil
	}
	requirementDocument, err := requirements.Load(document.Root, document.Manifest.ID)
	if err != nil {
		return nil, fmt.Errorf("load claim lineage: %w", err)
	}
	prefix := fmt.Sprintf("urn:change-saga:%s:claim:", document.Manifest.ID)
	for _, relation := range requirementDocument.Relations {
		if relation.Type != requirements.RelationSupersedes || relation.State != requirements.RelationActive || !strings.HasPrefix(relation.To, prefix) {
			continue
		}
		result[strings.TrimPrefix(relation.To, prefix)] = true
	}
	return result, nil
}

type evidenceTranslator struct {
	changes      gitdiff.ChangeSet
	atoms        []diffuri.Reference
	oldBases     map[string]bool
	expectedBase string
}

func (translator *evidenceTranslator) translate(uri, owner string) (string, bool, error) {
	reference, err := diffuri.Parse(uri)
	if err != nil {
		return "", false, fmt.Errorf("%s contains an invalid diff URI: %w", owner, err)
	}
	if reference.Repository != translator.changes.Repository {
		return "", false, fmt.Errorf("%s belongs to repository %s, not the refreshed comparison %s", owner, reference.Repository, translator.changes.Repository)
	}
	if reference.Base == translator.changes.BaseOID && reference.Head == translator.changes.HeadOID {
		return uri, false, nil
	}
	if reference.Head != translator.changes.HeadOID {
		return "", false, fmt.Errorf("refusing to rebase %s: product identity changed from %s to %s", owner, reference.Head, translator.changes.HeadOID)
	}
	if translator.expectedBase != "" && reference.Base != translator.expectedBase {
		return "", false, fmt.Errorf("refusing to rebase %s: selector base %s does not match --from-base %s", owner, reference.Base, translator.expectedBase)
	}
	oldBase := reference.Base
	reference.Base = translator.changes.BaseOID
	rewritten, err := diffuri.Build(reference)
	if err != nil {
		return "", false, fmt.Errorf("rebuild selector for %s: %w", owner, err)
	}
	matched := false
	for _, atom := range translator.atoms {
		if diffuri.Matches(reference, atom) {
			matched = true
			break
		}
	}
	if !matched {
		return "", false, fmt.Errorf("refusing to rebase %s: translated selector does not match any current product atom", owner)
	}
	translator.oldBases[oldBase] = true
	return rewritten, true, nil
}

func (translator *evidenceTranslator) singleOldBase() string {
	for base := range translator.oldBases {
		return base
	}
	return ""
}

func collectDiffFiles(section *saga.Section) []saga.DiffFile {
	if section == nil {
		return nil
	}
	result := append([]saga.DiffFile{}, section.Diffs...)
	for _, fragment := range section.Fragments {
		result = append(result, fragment.Diffs...)
		for _, landmark := range fragment.Landmarks {
			result = append(result, landmark.Diffs...)
		}
	}
	for _, child := range section.Children {
		result = append(result, collectDiffFiles(child)...)
	}
	return result
}

func latestVerification(verifications []saga.Verification, claimID string) *saga.Verification {
	var latest *saga.Verification
	for index := range verifications {
		candidate := &verifications[index]
		if candidate.Claim != claimID {
			continue
		}
		if latest == nil || candidate.CreatedAt.After(latest.CreatedAt) || candidate.CreatedAt.Equal(latest.CreatedAt) && candidate.ID > latest.ID {
			latest = candidate
		}
	}
	return latest
}

func applyEvidenceRebase(document *saga.Saga, plan evidenceRebasePlan) (err error) {
	backups := map[string][]byte{}
	created := []string{}
	rollback := func() error {
		var rollbackErrors []error
		for index := len(created) - 1; index >= 0; index-- {
			if removeErr := os.Remove(created[index]); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove %s: %w", filepath.Base(created[index]), removeErr))
			}
			if syncErr := store.SyncDir(filepath.Dir(created[index])); syncErr != nil {
				rollbackErrors = append(rollbackErrors, syncErr)
			}
		}
		for path, data := range backups {
			if restoreErr := store.WriteFile(path, data, 0o644, false); restoreErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", filepath.Base(path), restoreErr))
			}
		}
		return errors.Join(rollbackErrors...)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, rollback())
		}
	}()

	for _, evidence := range plan.Evidence {
		path := filepath.Join(document.Root, filepath.FromSlash(evidence.Relative))
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		backups[path] = data
		value := evidence.Value
		value.Path = ""
		if writeErr := store.WriteJSON(path, value, false); writeErr != nil {
			return writeErr
		}
	}
	if evidenceRebaseFaultHook != nil {
		if faultErr := evidenceRebaseFaultHook("after-evidence"); faultErr != nil {
			return faultErr
		}
	}
	for _, claim := range plan.Claims {
		claimDir, dirErr := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___claims"))
		if dirErr != nil {
			return dirErr
		}
		claimPath := filepath.Join(claimDir, claim.Replacement.ID+".json")
		if writeErr := store.WriteJSON(claimPath, claim.Replacement, true); writeErr != nil {
			if errors.Is(writeErr, fs.ErrExist) {
				return fmt.Errorf("generated claim id %q already exists", claim.Replacement.ID)
			}
			return writeErr
		}
		created = append(created, claimPath)
		if evidenceRebaseFaultHook != nil {
			if faultErr := evidenceRebaseFaultHook("after-claim"); faultErr != nil {
				return faultErr
			}
		}

		relationDir, dirErr := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___requirements", "relations"))
		if dirErr != nil {
			return dirErr
		}
		relationPath := filepath.Join(relationDir, claim.Relation.ID+".json")
		if writeErr := store.WriteJSON(relationPath, claim.Relation, true); writeErr != nil {
			return writeErr
		}
		created = append(created, relationPath)
		if evidenceRebaseFaultHook != nil {
			if faultErr := evidenceRebaseFaultHook("after-relation"); faultErr != nil {
				return faultErr
			}
		}

		if claim.Verification != nil {
			verificationDir, dirErr := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___verifications"))
			if dirErr != nil {
				return dirErr
			}
			verificationPath := filepath.Join(verificationDir, claim.Verification.ID+".json")
			if writeErr := store.WriteJSON(verificationPath, claim.Verification, true); writeErr != nil {
				return writeErr
			}
			created = append(created, verificationPath)
		}
	}

	_, validation, loadErr := saga.Load(document.Root)
	if loadErr != nil {
		return fmt.Errorf("validate rebased Saga: %w", loadErr)
	}
	if !validation.Valid {
		return fmt.Errorf("rebased Saga failed validation")
	}
	if document.Manifest.Version == saga.CurrentSagaVersion {
		if _, loadErr := requirements.Load(document.Root, document.Manifest.ID); loadErr != nil {
			return fmt.Errorf("validate claim supersession: %w", loadErr)
		}
	}
	return nil
}

func writeEvidenceRebaseResult(out io.Writer, result evidenceRebaseOutput, jsonOutput, quiet bool) error {
	if quiet {
		return nil
	}
	if jsonOutput {
		return writeJSON(out, result)
	}
	verb := "Rebased"
	if result.DryRun {
		verb = "Would rebase"
	}
	fmt.Fprintf(out, "%s %d selectors in %d evidence files from %s to %s\n", verb, result.Selectors, len(result.EvidenceFiles), result.FromBase, result.ToBase)
	fmt.Fprintf(out, "Product identity: %s (%d atoms, unchanged)\n", result.ProductIdentity, result.Atoms)
	if result.Claims > 0 {
		fmt.Fprintf(out, "Claims: %d replacements with supersedes relations; verifications carried: %d; left unverified: %d\n", result.Claims, result.VerificationsCarried, result.ClaimsLeftUnverified)
	}
	return nil
}
