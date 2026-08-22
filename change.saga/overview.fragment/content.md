# From code change to shared understanding {#from-code-to-understanding}

Change Saga turns a large Git comparison into a document that a person can
understand a chapter at a time. Its completeness check answers a narrow but
important question—has every changed line or file event been explained
somewhere?—without pretending that coverage proves correctness.[^mission]

The authoring loop is deliberately built for AI. An agent performs the tedious
work of structuring the narrative, drawing the flows, citing exact evidence,
and closing coverage gaps; the human reviewer keeps the consequential job of
understanding the system and deciding whether the change is sound.[^ai-author]

## Read this from the outside in {#reading-path}

1. **Portable evidence and AI authoring** explains the file-native format,
   canonical diff identities, coverage engine, authoring loop, and bounded
   machine interface.
2. **The local review workspace** follows a reviewer between narrative, source,
   coverage, annotations, and Git-attributed review history.
3. **Shipping, trust, and quality** traces a tagged revision through tests,
   reproducible builds, release assets, verified installation, and local
   operation.

The same artifact works as a PR-scale explanation or as maintained codebase
documentation. A new source diff can be projected onto existing evidence to
identify explanations that must change, might change, or do not exist yet.[^maintenance]

## What remains deliberately separate {#boundaries}

The Saga is the author's account of the change, not a code-review verdict.
Review happens as a separate overlay: readers can move between the story and
the exact code, leave granular feedback, and commit each action as independent
history.[^review-workspace]

Agents get a bounded query interface instead of crawling metadata, while the
manual CLI remains available as an escape hatch rather than the expected
authoring experience.[^ai-interface]

Distribution and security are part of the same contract: the project documents
installation, local-only serving, source builds, support, governance, and the
limits of running untrusted interactive content.[^operations]

[^mission]: The public contract defines coverage as an omission check rather than a correctness verdict.
[^ai-author]: AI prepares the structured visual account; a human still decides whether the change is correct.
[^maintenance]: Codebase Sagas are maintained by projecting incoming source diffs onto existing evidence ownership.
[^review-workspace]: Narrative, code, coverage, and file-granular review history remain connected without becoming the same thing.
[^ai-interface]: Agents use bounded queries and atomic mutations while direct CLI authoring remains documented for exceptional use.
[^operations]: Installation, security boundaries, source builds, governance, support, and licensing are explicit parts of the public surface.
