# Spend work where reviewer intent points {#spend-work-where-reviewer-intent-points}

A fragment hover or keyboard focus starts a short intent timer. Once it fires, the browser queues target-code summaries for that fragment and its contained elements, with a small speculative concurrency limit and cache.[^prefetch]

Leaving the fragment cancels queued work and aborts speculative fetches that are no longer useful. Clicking promotes any pending request instead of starting a duplicate.[^cancel]

As soon as a target summary arrives, one installation path updates every representation of that target: heading links, landmark-menu aliases, SVG hotspots, and Markdown citation controls. Counts therefore appear before the reviewer opens the drawer.[^counts]

Summary and body remain separate. Target summaries contain file and range metadata; the actual file diff is fetched only when its row is opened. Reverse “Explained by” links use the same bounded navigation model to return from code to narrative evidence.[^navigation]

Coverage does not expose implementation jargon about bounded pages. It continuously requests subsequent summary pages until the list is complete, while item contents remain lazy.[^streaming]

[^prefetch]: Fragment intent schedules target-code summaries with a 160 ms delay, two speculative workers, and a bounded cache.
[^cancel]: Mouse-out removes queued descendants, aborts unnecessary speculative requests, and click promotion reuses in-flight work.
[^counts]: Completed target summaries immediately update counts on all code-link aliases, SVG landmarks, and citations for that target.
[^navigation]: Code drawers fetch one file body on demand and expose reverse explanation owners that navigate to the exact narrative target.
[^streaming]: Coverage automatically appends all compact summary pages and defers ownership ranges and file diffs until expansion.
