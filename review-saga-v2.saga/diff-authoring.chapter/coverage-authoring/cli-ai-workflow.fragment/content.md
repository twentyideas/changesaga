# CLI surface {#cli-surface}

The CLI creates saga roots, chapters, recursive sections, content fragments, evidence files, rich threads, replies, approvals, and review-state events. `validate` checks the portable structure; `status` combines that result with the current Git comparison; `open` launches the local reviewer. JSON modes give agents a deterministic interface and exit code 3 means coverage remains incomplete rather than structurally broken.

# AI authoring discipline {#ai-authoring-discipline}

The bundled skill resolves an exact PR or local comparison, reads the full change before grouping it, writes the big-picture overview first, creates PR-sized chapters, and treats uncovered atoms as a work queue. Its content contract requires motivation, behavior, non-goals, architecture, risks, verification, chapter dependencies, and reviewer checks. Coverage is an omission guard, not permission to attach unexplained broad ranges.

# Current AI navigation boundary {#current-ai-navigation-boundary}

Authoring is feasible with `status --json` plus normal filesystem access. Reviewing is less direct: the CLI can report completeness and target totals, but it does not yet provide a compact query such as “show this chapter with its fragments and linked code” or the reverse lookup “which narrative targets explain this diff URI?” This first saga is intentionally being used to determine whether a richer CLI query layer is sufficient or whether an MCP server adds meaningful value.
