# Authoring a reviewer-ready saga

## Resolve the comparison

For a PR number or URL, use repository-host metadata rather than guessing:

- PR number, URL, title, and stated motivation
- base branch/OID and head branch/OID
- commit and changed-file summary
- local checkout containing the head

When GitHub CLI is available, an appropriate discovery call is:

```sh
gh pr view <number> --json number,title,url,body,baseRefName,headRefName,headRefOid,files,commits
```

Use an available provider integration instead when it has better access. For a
local change without a PR, identify the intended merge base and choose `HEAD` or
`WORKTREE` explicitly.

## Overview contract

The root overview should let a reviewer answer these before opening code:

1. What user or system problem is being solved?
2. What changes in behavior, and what deliberately does not change?
3. What is the end-to-end flow or architecture?
4. Why is this implementation shaped this way?
5. What are the major risks, rollout/compatibility concerns, and verification
   signals?
6. What chapters follow, in what order, and which can be reviewed independently?

Lead with a useful diagram when three or more components, states, or steps
interact. Prefer an interactive fragment only when interaction reveals behavior
that prose or a static diagram cannot.

## Chapter contract

A chapter is approximately a PR-sized review boundary. It should be coherent
enough that another reviewer could take responsibility for it. Its overview
should state:

- purpose and boundary;
- dependencies on earlier/later chapters;
- behavioral walkthrough and important invariants;
- notable implementation decisions and rejected alternatives when relevant;
- failure modes, security/data/operational risk, and compatibility concerns;
- tests and concrete reviewer checks;
- which code is intentionally covered at chapter scope rather than by a more
  focused fragment.

Split a chapter when it contains independently understandable behavior with a
different risk profile or reviewer specialty. Do not create a chapter merely
for each directory or language.

## Evidence discipline

- Attach a changed atom to the most focused fragment that actually explains it.
- Use chapter-level evidence only for truly cross-cutting code discussed by the
  chapter overview.
- Read generated, vendored, lockfile, migration, and snapshot changes; group
  them explicitly rather than hiding them in a broad range.
- Keep deletions visible and explain behavior that disappeared.
- Treat overlaps as intentional only when the same code is necessary in two
  distinct reviewer journeys.
- Never widen a selector solely to achieve complete coverage.

## Reviewer-readiness check

Before handing off:

- Read the saga in rendered order without relying on prior author knowledge.
- Confirm the root overview explains goals and supplies a useful chapter map.
- Confirm every chapter can be reviewed and approved independently.
- Confirm diagrams and examples agree with current code.
- Confirm every product atom is covered, no URI is stale, and every overlap is
  defensible.
- Confirm tests, migrations, generated artifacts, and removed behavior are not
  silently omitted.
- Report genuine uncertainty inside the saga instead of inventing intent.
