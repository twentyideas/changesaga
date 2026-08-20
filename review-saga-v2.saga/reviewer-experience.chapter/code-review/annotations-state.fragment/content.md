# Beyond line-number comments {#beyond-line-number-comments}

Fragment review supports whole-target comments, resilient text quotes with context and positions, normalized rectangles/lines/ellipses, and freehand paths. Normalized geometry survives responsive resizing. Thread messages reuse the fragment model, so a discussion can carry diagrams, screenshots, or interactive demonstrations.

Ctrl/Cmd+Z reverses in-progress canvas changes, while Ctrl/Cmd+Shift+Z and Ctrl+Y restore them. Rectangle and freehand gestures accumulate in one pending annotation and each add, move, recolor, or removal is an in-memory history step. After submission, committed shapes are selectable: dragging moves them, the color picker recolors them, and Remove hides them. Those durable edits append anchor or `withdrawn` state events instead of rewriting the original thread.

# Code review actions {#code-review-actions}

Every diff row can start a comment or a semantic suggestion containing explicit replacement code. File-level reviewed markers use absolute `/file` diff URIs, so a new product comparison starts unreviewed rather than inheriting stale confidence. Saga, chapter, section, and fragment approvals use append-only decision events.

# Shared behavior across views {#shared-behavior-across-views}

Diff threads are indexed by their absolute diff URI rather than by the screen where they were created. The attached-code drawer and the full Code Diff view therefore render the same discussion. Target-level comments remain with their narrative target.

# Safety details {#safety-details}

Mutation handlers cap request sizes, validate targets and anchors, constrain attachment media types, and delegate to exclusive append-only storage. Fragment assets are served through a target lookup with path cleaning, reserved-name rejection, realpath containment, and a restrictive content security policy.

Diff controls deliberately store custom-scheme evidence addresses in `data-diff-ref` rather than a URL-classified HTML attribute. The first rendered saga exposed that Go's template sanitizer rewrote `data-uri="saga-diff://…"` to `#ZgotmplZ`; the regression test now requires the complete diff reference to survive rendering.
