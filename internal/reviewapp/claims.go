package reviewapp

import (
	"context"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitattribution"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

func (s *session) Claims(ctx context.Context, query ClaimQuery) (ClaimPage, error) {
	if err := ctx.Err(); err != nil {
		return ClaimPage{}, err
	}
	if query.Target != "" {
		if err := s.validateTargetArgument(query.Target); err != nil {
			return ClaimPage{}, err
		}
		if s.targets[query.Target] == nil {
			return ClaimPage{}, notFound("target", query.Target)
		}
	}
	if query.Status != "" && !saga.ValidVerificationStatus(query.Status) {
		return ClaimPage{}, invalidArgument("status must be unverified, verified, failed, or inconclusive")
	}
	resolver := gitattribution.New(ctx, s.document.Root)
	latest := s.latestVerifications(ctx, resolver)
	items := []ClaimRecord{}
	for _, stored := range s.document.Claims {
		if query.Target != "" && stored.Target != query.Target {
			continue
		}
		item := ClaimRecord{
			ID: stored.ID, Target: stored.Target, Kind: stored.Kind, Statement: stored.Statement, CreatedAt: stored.CreatedAt,
			Attribution: attribution(ctx, resolver, stored.Path), Evidence: []ClaimEvidence{}, VerificationStatus: "unverified",
		}
		if verification, ok := latest[stored.ID]; ok {
			copy := verification
			item.LatestVerification = &copy
			item.VerificationStatus = verification.Status
		}
		if query.Status != "" && item.VerificationStatus != query.Status {
			continue
		}
		for _, uri := range stored.Evidence {
			item.Evidence = append(item.Evidence, s.resolveClaimEvidence(uri, stored.Target))
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	key := query.Target + "\x00" + query.Status
	start, end, page, err := s.page("claims", key, query.Cursor, query.Limit, len(items))
	if err != nil {
		return ClaimPage{}, err
	}
	return ClaimPage{Claims: append([]ClaimRecord{}, items[start:end]...), Page: page}, nil
}

func (s *session) Verifications(ctx context.Context, query VerificationQuery) (VerificationPage, error) {
	if err := ctx.Err(); err != nil {
		return VerificationPage{}, err
	}
	if query.Claim != "" && !saga.ValidID(query.Claim) {
		return VerificationPage{}, invalidArgument("claim must be a stable claim id")
	}
	if query.Status != "" && !saga.ValidVerificationStatus(query.Status) {
		return VerificationPage{}, invalidArgument("status must be unverified, verified, failed, or inconclusive")
	}
	if query.Claim != "" {
		exists := false
		for _, stored := range s.document.Claims {
			exists = exists || stored.ID == query.Claim
		}
		if !exists {
			return VerificationPage{}, notFound("claim", query.Claim)
		}
	}
	resolver := gitattribution.New(ctx, s.document.Root)
	items := []VerificationRecord{}
	for _, stored := range s.document.Verifications {
		if query.Claim != "" && stored.Claim != query.Claim || query.Status != "" && stored.Status != query.Status {
			continue
		}
		items = append(items, normalizeVerification(ctx, resolver, stored))
	}
	key := query.Claim + "\x00" + query.Status
	start, end, page, err := s.page("verifications", key, query.Cursor, query.Limit, len(items))
	if err != nil {
		return VerificationPage{}, err
	}
	return VerificationPage{Verifications: append([]VerificationRecord{}, items[start:end]...), Page: page}, nil
}

func (s *session) resolveClaimEvidence(uri, target string) ClaimEvidence {
	result := ClaimEvidence{URI: uri, Status: "stale", Atoms: []gitdiff.Atom{}}
	selector, err := diffuri.Parse(uri)
	if err != nil {
		return result
	}
	for _, atom := range s.changes.Atoms {
		atomReference, parseErr := diffuri.Parse(atom.URI)
		if parseErr == nil && diffuri.Matches(selector, atomReference) {
			result.Atoms = append(result.Atoms, atom)
		}
	}
	if len(result.Atoms) == 0 {
		return result
	}
	result.Status = "current"
	result.MappedToTarget = true
	for _, atom := range result.Atoms {
		mapped := false
		for _, owner := range s.selectorsByAtom[atom.URI] {
			mapped = mapped || owner.Target == target
		}
		if !mapped {
			result.MappedToTarget = false
			break
		}
	}
	return result
}

func (s *session) latestVerifications(ctx context.Context, resolver *gitattribution.Resolver) map[string]VerificationRecord {
	latest := map[string]VerificationRecord{}
	for _, stored := range s.document.Verifications {
		value := normalizeVerification(ctx, resolver, stored)
		previous, exists := latest[stored.Claim]
		if !exists || previous.CreatedAt.Before(value.CreatedAt) || previous.CreatedAt.Equal(value.CreatedAt) && previous.ID < value.ID {
			latest[stored.Claim] = value
		}
	}
	return latest
}

func normalizeVerification(ctx context.Context, resolver *gitattribution.Resolver, stored saga.Verification) VerificationRecord {
	return VerificationRecord{
		ID: stored.ID, Claim: stored.Claim, Status: stored.Status, Method: stored.Method,
		Summary: strings.TrimSpace(stored.Summary), Command: stored.Command, CreatedAt: stored.CreatedAt,
		Attribution: attribution(ctx, resolver, stored.Path),
	}
}
