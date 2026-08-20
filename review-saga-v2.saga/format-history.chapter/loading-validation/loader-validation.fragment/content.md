# Strict recursive loading {#strict-recursive-loading}

The loader walks the saga root into a typed tree while preserving stable targets independently from directory paths. Root fragments provide big-picture content; direct `.chapter` directories become chapter targets; ordinary descendants become sections; `.fragment` packages stop structural recursion and load their own entrypoint, evidence, and approvals.

JSON decoding rejects unknown fields and trailing values. The validator then checks versions, stable IDs, global uniqueness, source repository URIs, media types, entrypoint containment, reserved directories, thread targets, anchors, messages, events, and review states.

# Filesystem safety {#filesystem-safety}

Reserved metadata directories must be real directories rather than symlinks. Fragment entrypoints are resolved against the real package root and cannot escape. Nested chapters are rejected so chapter-level approval retains one clear meaning.

# Review focus {#review-focus}

The two-pass document validation is important: message attachments are fragments too, so their stable IDs and targets must be registered before thread targets are checked. Invalid structures remain loadable enough to report useful issues where safe, but the server refuses to start on a structurally invalid saga.
