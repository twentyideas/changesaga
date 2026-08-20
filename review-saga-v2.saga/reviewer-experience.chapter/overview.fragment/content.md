# Chapter purpose {#chapter-purpose}

Make the format usable for a human reviewing a large change over several sessions. This chapter owns the local server, narrative renderer, complete diff browser, annotation tooling, and mutation endpoints.

# Boundary and dependencies {#boundary-and-dependencies}

The UI consumes the loaded saga, coverage report, source atoms, and append-only review store defined in the first two chapters. It does not introduce a separate browser database or hidden review state.

# Reviewer journey {#reviewer-journey}

Saga view presents the root overview and chapters in authored order. Every section, fragment, or landmark with evidence can open its attached code in a wide scrollable drawer without losing narrative context. The drawer begins with collapsed source files and an authored what-and-why summary for each one; reviewers expand only the file whose linked ranges they are ready to inspect. Code Diff view reuses the same diff rows but exposes the complete comparison with a changed-file tree. Coverage Manifest projects the same atom ownership in both directions, so a reviewer can audit every changed range against its narrative destinations and every mapped narrative element against its exact code ranges.

Reviewers can comment on targets, select text, draw rectangles or freehand paths, attach rich fragments, comment on diff lines, propose replacement code, resolve threads, approve narrative targets, and mark files reviewed. Because both views use the same stored threads, a comment made in either context appears everywhere that diff is shown.

# Security and interaction risks {#security-and-interaction-risks}

Interactive HTML and SVG run in sandboxed iframes with scripts enabled but network, forms, objects, and parent access denied. Fragment file serving resolves symlinks and rejects reserved metadata paths. The server is intentionally local-only by default and has no authentication.

# Reviewer checks {#reviewer-checks}

Verify the same line thread appears in attached drawers and the full diff view; the Manifest has no unmapped or stale entries and both directions deep-link to the exact Saga element or code range; review state survives reload because it is file-backed; text and shape anchors remain attached to the correct fragment; unsafe fragment paths are rejected; and responsive layouts preserve access to primary controls.
