# Scaling Change Saga to whole-codebase reviews {#scaling-change-saga-to-whole-codebase-reviews}

A pathological saga is not a special mode. It is the same review contract under enough evidence to expose every accidental whole-saga operation: 2,666 source files, 532,290 changed atoms, and 118 narrative targets.

This branch changes the shape of the work instead of hiding it behind a faster spinner. Evidence is coalesced into canonical ranges; coverage and ownership are indexed once; immutable comparison results live in disposable generations; HTTP responses expose bounded projections; and the browser progressively fills the surface the reviewer is actually using.[^architecture]

The compact connector prototype is deliberately retained as an experiment, not promoted into the format. Its measurements showed that ordinary v2 ranged evidence captures nearly all of the size win without a migration or a runtime database.[^format-decision]

## What a reviewer should test {#what-a-reviewer-should-test}

1. Open the saga and navigate immediately, before its comparison generation is ready.
2. Hover a narrative fragment, then watch code-link counts populate before clicking.
3. Open one linked file and confirm only that file's diff body is fetched.
4. Move to Coverage and let file or target summaries stream in continuously.
5. Expand one coverage item and confirm ownership ranges and diff rows arrive only then.

The architectural map that follows is also the review index: each node opens the exact implementation evidence for that stage.

[^architecture]: The shipped design separates an aggregate shell, a content-addressed comparison generation, bounded APIs, and demand-driven browser hydration.
[^format-decision]: The connector experiment remains isolated because canonical ranged v2 evidence is almost as small in Git and avoids a format or dependency change.
