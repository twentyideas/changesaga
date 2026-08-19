# One review action, one path

A comment creates a unique `.thread` directory with immutable thread metadata and an initial `.message` directory. Every reply creates another message directory. Each message contains one or more fragments, so discussion can include Markdown, images, SVG, or sandboxed HTML without expanding a shared thread file.

Resolve/reopen events, approvals/rejections, and reviewed/unreviewed file markers are independent JSON files ordered by their timestamps. IDs combine nanosecond UTC time with randomness, and writes use exclusive creation. A collision fails instead of overwriting history.

# Why this matters

Two reviewers commenting on the same fragment normally add different directories. Two people replying to the same thread add different message directories. Git may still need help if people intentionally edit the same authored fragment, but routine review activity avoids a central comments array or mutable state document.

The concurrency test exercises simultaneous comments, simultaneous replies on one thread, independent decisions, and reviewed-state transitions. It then checks that earlier thread/message bytes are unchanged and every body has its own content file.
