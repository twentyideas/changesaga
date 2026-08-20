# Chapter purpose {#chapter-purpose}

Turn an arbitrary Git comparison into a reviewer-oriented story with a mechanically complete evidence map. This is the chapter to assign to someone evaluating the CLI, diff semantics, or AI authoring loop.

# Boundary and dependencies {#boundary-and-dependencies}

This chapter depends on the target and evidence structures from the format chapter. It covers absolute diff URIs, Git patch parsing, product identity, coverage evaluation, CLI commands, repository-safe writes, documentation, and the bundled authoring skill. The browser renderer is covered in the final chapter.

# Behavioral flow {#behavioral-flow}

The engine resolves the base, computes the merge-base-style comparison, parses every changed line and supported file event into an atom, and gives each atom a fully realized URI. Coverage walks evidence attached at saga, chapter, section, and fragment scope, then reports uncovered atoms, stale URIs, overlaps, and target totals. The CLI exposes this as a repeatable authoring loop: inspect uncovered work, explain it, attach evidence, and validate again.

The head component of a diff URI is a digest of the binary product patch with `.saga` paths excluded. This is essential for multi-day review: committing a comment must not stale all existing evidence, while any product change must.

# Risks and reviewer checks {#risks-and-reviewer-checks}

- Check range matching on old versus new sides and immutable comparison identity.
- Check rename, mode, and binary events are neither lost nor double-counted.
- Check `.saga` changes are reported separately and excluded from completeness.
- Check target resolution cannot enter reserved directories or collide on stable IDs.
- Check the skill produces goals, chapter boundaries, risks, tests, and defensible evidence—not a directory-by-directory summary.

Normal tests, race tests, vet, schema parsing, and skill validation form the acceptance signal.
