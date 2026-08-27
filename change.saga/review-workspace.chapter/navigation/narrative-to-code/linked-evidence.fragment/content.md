# From explanation to exact evidence {#linked-evidence}

The page evaluates the product comparison once, indexes covered atoms by target, and builds each rendered fragment with only its owned changes.[^target-index] A landmark or citation therefore opens a focused drawer whose rows stay grouped by source file; the collapsed file summary is assembled from the mapping notes that actually match those atoms.[^drawer-summary]

Opening a file automatically streams every bounded diff page so all changed hunks are present without a `Load more` gate. The assembled diff keeps unchanged rows collapsed with controls to reveal the next ten lines, previous ten lines, or the whole gap, while originally linked rows remain marked for comment and suggestion attribution.[^drawer-hydration] Authored Markdown is rendered as safe GFM with explicit namespaced heading IDs, while raw HTML is escaped rather than trusted.[^safe-markdown]

[^target-index]: Page construction derives a per-target atom index from evaluated coverage before fragment views are rendered.
[^drawer-summary]: Attached-code projection groups exact atoms by file and derives each collapsed summary from matching authored evidence notes.
[^drawer-hydration]: The browser fetches the complete file diff on first expansion and highlights the rows already linked by the narrative target.
[^safe-markdown]: Markdown rendering enables GFM and footnotes, namespaces stable headings, and escapes authored raw HTML.
