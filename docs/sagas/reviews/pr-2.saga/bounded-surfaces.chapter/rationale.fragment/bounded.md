# Make scale a contract at every boundary {#make-scale-a-contract-at-every-boundary}

Server-side rendering returns a useful shell rather than an eagerly expanded review. Chapter sections and fragment bodies arrive independently; a locate endpoint resolves deep links without preloading intervening chapters.[^incremental]

Code is exposed as a source catalog and per-file pages. File-diff requests traverse indexed atom positions for one path and return a bounded page with a continuation cursor.[^code]

Coverage similarly returns projections instead of ownership atoms. The top level is a page of file summaries in Code → Saga mode or narrative-target summaries in Saga → Code mode; ownership ranges and diff bodies are separate detail requests.[^coverage]

Response byte ceilings, item limits, first-load budgets, and generated large-saga fixtures turn these shapes into testable contracts. Browser tests exercise navigation, annotations, review decisions, and continuation behavior across the lazy boundaries.[^contracts]

[^incremental]: The server renders descriptors first and exposes section, fragment, locate, and outline endpoints for progressive narrative hydration.
[^code]: Source catalogs and file-diff APIs use per-path indexes and cursors so one request cannot expand the full comparison.
[^coverage]: Coverage endpoints page compact file or target summaries and defer ownership ranges and diff rows until an item is opened.
[^contracts]: Unit, budget, benchmark, and browser tests assert both bounded payloads and preserved review workflows.
