# Append-only persistence contract {#append-only-persistence}

A new top-level thread is built in a hidden sibling directory and published with one rename only after its manifest and first message are complete.[^thread-commit] Replies use the same staged-directory publication, while state changes and anchor edits create new exclusive event files instead of changing the thread root.[^event-append]

All supported writers validate the Saga, acquire the Saga-directory lock, validate again inside the lock, and only then perform the mutation.[^locked-mutation] File writes sync a same-directory temporary file before link-or-rename publication and sync the parent directory afterward; failed exclusive publication removes the destination when needed.[^durable-write]

Path creation preflights every existing component, rejects symlinks and escapes before the first directory is created, and reserves `___` namespaces from section traversal.[^path-preflight] Platform adapters provide advisory writer locking and directory sync behavior without changing the record model.[^platform-storage]

[^thread-commit]: Thread creation publishes the thread manifest and first message as one staged directory entity.
[^event-append]: Replies, state transitions, and anchor edits are committed as independent message directories or exclusive event files.
[^locked-mutation]: Review mutations validate before and after acquiring the bounded Saga writer lock.
[^durable-write]: Atomic file and directory helpers sync data and parent directories around exclusive link or rename publication.
[^path-preflight]: Metadata path validation rejects escapes, reserved traversal, and symlinked components before creating missing directories.
[^platform-storage]: Unix and Windows adapters implement writer locking and directory durability behind the shared store contract.
