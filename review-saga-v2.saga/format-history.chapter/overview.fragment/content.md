# Chapter purpose

Define the portable on-disk contract that every authoring tool, renderer, and reviewer depends on. This chapter can be reviewed independently as a data-format and persistence change.

# Boundary

Included here are the domain model, strict recursive loader, structural validation, JSON schemas, append-only review store, and normative specification. Diff generation, coverage evaluation, CLI orchestration, and rendering are covered later.

# Walkthrough

A `.saga` root supplies the source comparison and big overview. Direct `.chapter` children are independently reviewable boundaries. Ordinary directories recurse as sections. Every `.fragment` directory declares a stable ID, media type, and package-relative entrypoint. Diff evidence lives beside the target it explains, while review threads are a separate overlay keyed by stable target URNs.

Comments do not accumulate in shared arrays. Each top-level comment owns a thread directory; each initial comment or reply owns a message directory containing fragment packages; every thread state, approval, and file-reviewed transition is a new event file.

# Invariants and risks

- IDs must be globally unambiguous within the saga.
- Entrypoints and metadata directories cannot escape through paths or symlinks.
- Unknown JSON fields are errors so misspelled contract data cannot be ignored.
- Chapters cannot nest; sections provide fractal detail inside a chapter.
- Review writers create new paths exclusively and never rewrite another reviewer’s record.

# Reviewer checks

Compare the Go types, loader rules, JSON schemas, and SPEC examples. Pay special attention to reserved-directory handling, target existence, diff/text/drawing anchor validation, and the concurrency test proving that simultaneous comments and replies remain disjoint.
