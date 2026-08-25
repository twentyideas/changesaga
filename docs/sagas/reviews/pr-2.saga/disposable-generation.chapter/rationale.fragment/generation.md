# Separate durable review state from rebuildable computation {#separate-durable-review-state-from-rebuildable-computation}

The root page now comes from a structural load path: identity, overview descriptors, chapter summaries, and navigation. It does not start or wait for Git comparison work.[^shell]

Detailed comparison state is addressed by the fingerprints of the saga tree and immutable base/head comparison. A cache miss builds a new generation; a hit cannot be stale because identical content produces the same address.[^fingerprint]

Generation files are written with the repository's durability model and can be deleted without affecting truth. Their encoding hoists repeated repository, revision, path, and content strings so cached review data does not repeat large identities per atom.[^cache]

The expensive detailed review projection is initialized lazily and reused. Meanwhile comments, review decisions, threads, and approvals mutate through focused saga and review-store APIs and remain outside the immutable comparison generation.[^mutations]

[^shell]: The initial renderer loads structural saga metadata without invoking comparison generation or materializing explanation bodies.
[^fingerprint]: Snapshot generations are content-addressed by exact source and saga inputs, so invalidation is represented by a different key.
[^cache]: Disposable snapshot data is atomically persisted outside the saga and compresses repeated comparison fields.
[^mutations]: Review writes use focused mutation paths and do not rebuild or rewrite the shared comparison snapshot.
