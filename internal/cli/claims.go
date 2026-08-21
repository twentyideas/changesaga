package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

// AddClaim records one falsifiable assertion without changing coverage. The
// evidence is intentionally repeated here as an assertion boundary: query can
// then prove whether those exact atoms are current and already mapped to the
// claim's narrative target.
func AddClaim(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("add-claim", commandUsage["add-claim"], out)
	id := flags.String("id", "", "stable claim id; generated when omitted")
	target := flags.String("target", "", "saga, chapter, section, fragment, or landmark target")
	kind := flags.String("kind", "behavior", "behavior, invariant, performance, compatibility, security, data, ux, or test")
	statement := flags.String("statement", "", "falsifiable assertion made by the change author")
	var evidence stringList
	flags.Var(&evidence, "diff", "exact supporting line or event diff URI; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || strings.TrimSpace(*target) == "" || strings.TrimSpace(*statement) == "" || len(evidence) == 0 {
		return fmt.Errorf("usage: %s", commandUsage["add-claim"])
	}
	if *id == "" {
		generated, err := generatedRecordID("claim")
		if err != nil {
			return err
		}
		*id = generated
	}
	if !saga.ValidID(*id) {
		return fmt.Errorf("--id must be a stable 1-128 character identifier")
	}
	if !saga.ValidClaimKind(*kind) {
		return fmt.Errorf("--kind must be behavior, invariant, performance, compatibility, security, data, ux, or test")
	}

	var created string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		_, resolvedTarget, err := resolveTarget(document, *target, true)
		if err != nil {
			return err
		}
		repository, err := diffuri.CanonicalRepository(document.Manifest.Source.Repository)
		if err != nil {
			return fmt.Errorf("invalid declared source repository: %w", err)
		}
		seen := map[string]bool{}
		for index, uri := range evidence {
			reference, parseErr := diffuri.Parse(uri)
			if parseErr != nil || reference.Kind == "file" {
				return fmt.Errorf("invalid --diff %d: expected a canonical line or event diff URI", index+1)
			}
			if reference.Repository != repository {
				return fmt.Errorf("invalid --diff %d: repository does not match the saga source", index+1)
			}
			if seen[uri] {
				return fmt.Errorf("--diff %d duplicates an earlier URI", index+1)
			}
			seen[uri] = true
		}
		for _, existing := range document.Claims {
			if existing.ID == *id {
				return fmt.Errorf("claim id %q already exists", *id)
			}
		}
		dir, err := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___claims"))
		if err != nil {
			return err
		}
		path := filepath.Join(dir, *id+".json")
		value := saga.Claim{
			Version: saga.CurrentVersion, ID: *id, Target: resolvedTarget, Kind: *kind,
			Statement: strings.TrimSpace(*statement), Evidence: append([]string{}, evidence...), CreatedAt: time.Now().UTC(),
		}
		if err := store.WriteJSON(path, value, true); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("claim id %q already exists", *id)
		} else if err != nil {
			return err
		}
		created, _ = filepath.Rel(document.Root, path)
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added claim %s\nID: %s\n", filepath.ToSlash(created), *id)
	return nil
}

// VerifyClaim appends one independent result. It never edits the claim or an
// earlier verification, so disagreement and changing evidence remain visible
// in Git history and through the query API.
func VerifyClaim(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("verify-claim", commandUsage["verify-claim"], out)
	id := flags.String("id", "", "stable verification id; generated when omitted")
	claimID := flags.String("claim", "", "claim id being evaluated")
	status := flags.String("status", "", "unverified, verified, failed, or inconclusive")
	method := flags.String("method", "", "test, command, measurement, inspection, or analysis")
	summary := flags.String("summary", "", "what was or was not established")
	command := flags.String("command", "", "reproducible command used during verification")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || strings.TrimSpace(*claimID) == "" || strings.TrimSpace(*status) == "" || strings.TrimSpace(*summary) == "" {
		return fmt.Errorf("usage: %s", commandUsage["verify-claim"])
	}
	if !saga.ValidVerificationStatus(*status) {
		return fmt.Errorf("--status must be unverified, verified, failed, or inconclusive")
	}
	if *status != "unverified" && !saga.ValidVerificationMethod(*method) {
		return fmt.Errorf("--method is required for a result and must be test, command, measurement, inspection, or analysis")
	}
	if *status == "unverified" && *method != "" && !saga.ValidVerificationMethod(*method) {
		return fmt.Errorf("--method must be test, command, measurement, inspection, or analysis")
	}
	if *id == "" {
		generated, err := generatedRecordID("verification")
		if err != nil {
			return err
		}
		*id = generated
	}
	if !saga.ValidID(*id) {
		return fmt.Errorf("--id must be a stable 1-128 character identifier")
	}

	var created string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		claimExists := false
		for _, claim := range document.Claims {
			claimExists = claimExists || claim.ID == *claimID
		}
		if !claimExists {
			return fmt.Errorf("claim %q does not exist", *claimID)
		}
		for _, existing := range document.Verifications {
			if existing.ID == *id {
				return fmt.Errorf("verification id %q already exists", *id)
			}
		}
		dir, err := store.EnsureDirWithin(document.Root, filepath.Join(document.Root, "___verifications"))
		if err != nil {
			return err
		}
		path := filepath.Join(dir, *id+".json")
		value := saga.Verification{
			Version: saga.CurrentVersion, ID: *id, Claim: *claimID, Status: *status, Method: *method,
			Summary: strings.TrimSpace(*summary), Command: strings.TrimSpace(*command), CreatedAt: time.Now().UTC(),
		}
		if err := store.WriteJSON(path, value, true); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("verification id %q already exists", *id)
		} else if err != nil {
			return err
		}
		created, _ = filepath.Rel(document.Root, path)
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added verification %s\nID: %s\n", filepath.ToSlash(created), *id)
	return nil
}

func generatedRecordID(prefix string) (string, error) {
	var entropy [10]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(entropy[:]), nil
}
