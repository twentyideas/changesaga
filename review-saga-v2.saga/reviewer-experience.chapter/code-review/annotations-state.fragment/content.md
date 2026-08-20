# Beyond line-number comments {#beyond-line-number-comments}

Fragment review supports whole-target comments, resilient text quotes with context and positions, normalized rectangles/lines/ellipses, freehand paths, and placed sticky notes. Normalized geometry survives responsive resizing. Thread messages reuse the fragment model, so a discussion can carry diagrams, screenshots, or interactive demonstrations.

A sticky is a `note` anchor rather than a new thread kind: an ordinary comment thread whose anchor carries the visible note text, a normalized centre point, and an optional color. That is the smallest coherent extension that keeps replies, state, permalinks, and Git attribution while making rewording, moving, and recoloring append-only anchor events. Notes render as compact HTML overlays on the fragment stage instead of inside the non-uniformly scaled SVG canvas, so their text stays legible and selectable at any viewport width.

Ctrl/Cmd+Z reverses in-progress canvas changes, while Ctrl/Cmd+Shift+Z and Ctrl+Y restore them. Rectangle and freehand gestures accumulate in one pending annotation and each add, move, recolor, or removal is an in-memory history step. A pending sticky behaves the same way: placing it starts a draft, moving or recoloring it before commit is a history step, and undoing past the first step discards the draft so redo can restore it. After submission, committed shapes and notes are selectable: dragging moves them, arrow keys nudge a selected note, Enter or F2 rewords one, the color picker recolors them, and Remove or Delete hides them. Those durable edits append anchor or `withdrawn` state events instead of rewriting the original thread.

# Code review actions {#code-review-actions}

Every diff row can start a comment or a semantic suggestion containing explicit replacement code. File-level reviewed markers use absolute `/file` diff URIs, so a new product comparison starts unreviewed rather than inheriting stale confidence. Saga, chapter, section, and fragment approvals use append-only decision events.

# Shared behavior across views {#shared-behavior-across-views}

Diff threads are indexed by their absolute diff URI rather than by the screen where they were created. The attached-code drawer and the full Code Diff view therefore render the same discussion. Target-level comments remain with their narrative target.

# Safety details {#safety-details}

Mutation handlers cap request sizes, validate targets and anchors, constrain attachment media types, and delegate to exclusive append-only storage. Fragment assets are served through a target lookup with path cleaning, reserved-name rejection, realpath containment, and a restrictive content security policy.

Diff controls deliberately store custom-scheme evidence addresses in `data-diff-ref` rather than a URL-classified HTML attribute. The first rendered saga exposed that Go's template sanitizer rewrote `data-uri="saga-diff://…"` to `#ZgotmplZ`; the regression test now requires the complete diff reference to survive rendering.
