# Verification ledger {#verification-ledger}

The structured claim API in v0.0.6 writes Saga-root records, outside this workspace's chapter-only boundary. These chapter-local assertions preserve the same falsifiable statements and observed results without crossing that boundary.

## Verified invariants {#verified-invariants}

- **Verified · product identity:** adding a commit that changes only a `.saga` path leaves the product head identity and product atom URIs unchanged.[^product-identity-check]
- **Verified · read isolation:** the query fixture detects file creation, deletion, or content mutation performed by a read operation.[^query-isolation-check]
- **Verified · repair isolation:** `validate --fix` leaves Markdown fragments stored in the review overlay byte-identical.[^anchor-isolation-check]
- **Verified · maintenance honesty:** impact analysis marks an incomplete maintained baseline instead of reporting its projection as exhaustive.[^impact-incomplete-check]

All four results were observed by running:

```text
go test ./cmd/change-saga ./internal/cli ./internal/coverage ./internal/diffuri ./internal/gitdiff ./internal/impact ./internal/querytest ./internal/reviewapp ./internal/saga
```

The command passed across every owned Go package on this checkout.

[^product-identity-check]: TestProductIdentityIgnoresSagaOnlyCommits compares product identities and atom URIs before and after a Saga-only commit.
[^query-isolation-check]: TestFixtureNoSideEffectAssertion snapshots the fixture tree and fails when a query changes filesystem state.
[^anchor-isolation-check]: TestValidateFixLeavesReviewOverlayFragmentsAlone checks review-message bytes after authored Markdown anchor repair.
[^impact-incomplete-check]: TestAnalyzeMarksIncompleteBaselineInsteadOfClaimingCompleteImpact asserts the explicit incomplete-baseline result.
