# Why this change exists

Large pull requests are hard to review because a flat file diff exposes implementation order, not the story a reviewer needs. Stacking the work into several PRs changes the mechanics but can hide later reversals, complicate edits in the middle, and force reviewers to reconstruct the complete behavior across branches.

Review Saga introduces a Git-native review document for treating a large change as one coherent unit without forcing every reviewer to consume it at once. The author starts with the big picture, divides the work into independently reviewable chapters, adds focused explanatory fragments, and attaches every source change to the part of the story that explains it.

# What this implementation delivers

- A versioned directory format made from chapters, recursive sections, fragments, absolute diff links, and a separate review overlay.
- Markdown, text, raster image, SVG, and sandboxed interactive HTML fragments.
- Mechanical accounting for every added/deleted line plus rename, mode, and binary events.
- A Go CLI for authoring, validation, coverage, comments, replies, decisions, and local serving.
- A two-mode reviewer: Saga view for narrative-first review and Code Diff view for the complete comparison.
- Append-only, file-granular review records so parallel comments and replies normally merge without touching the same file.
- A bundled AI skill that turns a request such as “make a review saga for PR 123” into a disciplined authoring loop.

# Deliberate boundaries

This scaffold does not replace Git hosting, decide merge policy, apply code suggestions, synchronize shared cursors, or claim that 100% mechanical coverage makes an explanation good. The local server is loopback-oriented and unauthenticated. Interactive content can run bundled JavaScript, but is denied network and parent-page access.

# Key decisions

The format is the product. Review content and state are ordinary files that can be branched, committed, audited, or kept in a separate repository. Stable URNs identify narrative targets. Fully realized diff URIs identify the source repository, base, product-patch identity, path, side, and range/event. Saga-only commits do not invalidate product evidence; actual product edits do.

Chapters are the unit of incremental responsibility. Each is approximately the PR that might have existed in a stacked workflow, but remains visible in the context of the whole change.

# Risk and verification

The main risks are unsafe interactive content, stale or overly broad evidence, filesystem escapes, ambiguous identifiers, and review files that conflict under parallel activity. The implementation uses iframe sandboxing/CSP, strict JSON decoding, symlink containment checks, stable identifiers, append-only exclusive writes, and normal plus race-enabled test suites.

# Review order

1. Portable format and review history — understand the durable contract first.
2. Diff accounting and AI authoring — verify the omission-detection and creation loop.
3. Interactive reviewer experience — inspect how humans consume and annotate the result.

The chapters are independently approvable, but this order reduces backtracking because the UI consumes the contracts defined earlier.
