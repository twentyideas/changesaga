# Managed reviewer lifecycle {#managed-reviewer-lifecycle}

`change-saga open` starts the local reviewer as a managed background process and returns once it is active. A per-Saga state record contains its URL, PID, source checkout, log, start time, and a random shutdown token; a second open reuses an active process instead of multiplying servers.[^detached-start]

`serve status` scans state records, distinguishes active from stale processes, and reports only public state. `serve stop` proves liveness against the recorded process, calls the loopback shutdown endpoint with the private token, waits for exit, and removes state only when it still belongs to that PID.[^managed-stop]

The lifecycle tests exercise reuse, status, stop, stale cleanup, source-path round trips, and an invalid token that must not terminate the server.[^runtime-tests]

[^detached-start]: Detached review startup persists enough runtime identity for later management while keeping shutdown authorization out of public status output.
[^managed-stop]: Managed shutdown is loopback-scoped, token-authenticated, PID-aware, and conservative about deleting another process's state.
[^runtime-tests]: Runtime integration tests cover detached reuse, authenticated stopping, stale records, and source-checkout preservation.
