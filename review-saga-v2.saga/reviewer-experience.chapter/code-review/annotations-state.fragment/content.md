# Beyond line-number comments

Fragment review supports whole-target comments, resilient text quotes with context and positions, normalized rectangles/lines/ellipses, and freehand paths. Normalized geometry survives responsive resizing. Thread messages reuse the fragment model, so a discussion can carry diagrams, screenshots, or interactive demonstrations.

# Code review actions

Every diff row can start a comment or a semantic suggestion containing explicit replacement code. File-level reviewed markers use absolute `/file` diff URIs, so a new product comparison starts unreviewed rather than inheriting stale confidence. Saga, chapter, section, and fragment approvals use append-only decision events.

# Shared behavior across views

Diff threads are indexed by their absolute diff URI rather than by the screen where they were created. The attached-code drawer and the full Code Diff view therefore render the same discussion. Target-level comments remain with their narrative target.

# Safety details

Mutation handlers cap request sizes, validate targets and anchors, constrain attachment media types, and delegate to exclusive append-only storage. Fragment assets are served through a target lookup with path cleaning, reserved-name rejection, realpath containment, and a restrictive content security policy.

Diff controls deliberately store custom-scheme evidence addresses in `data-diff-ref` rather than a URL-classified HTML attribute. The first rendered saga exposed that Go's template sanitizer rewrote `data-uri="saga-diff://…"` to `#ZgotmplZ`; the regression test now requires the complete diff reference to survive rendering.
