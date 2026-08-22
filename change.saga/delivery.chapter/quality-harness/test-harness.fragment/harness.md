# The test harness as a product boundary {#test-harness}

The quality system has three complementary layers:

1. Shell and PowerShell contract tests build real artifacts or mock local release downloads, then exercise checksums, permissions, path traversal rejection, failed replacement, credentials, and workflow pinning.[^artifact-contracts]
2. Go fixtures synthesize deterministic large Sagas with thousands of exact mappings and append-only review records so validation, status, rendering, attribution, and coverage hot paths share a repeatable workload.[^large-fixture]
3. Playwright drives a real local server across navigation, review mutations, annotations, accessibility, CLI safety, and hostile HTTP inputs; failure hooks preserve browser and repository state for diagnosis.[^browser-harness]

The performance document treats timings as review budgets rather than portable assertions and preserves the command and fixture shape needed to reproduce comparisons.[^performance-budget]

[^artifact-contracts]: Release and installer tests assert consumer-visible archive, checksum, permission, replacement, credential, and workflow contracts.
[^large-fixture]: The generated large-Saga fixture is deterministic, valid, exactly covered, and deliberately large enough to exercise hierarchy and review history.
[^browser-harness]: The browser suite uses isolated Git repositories and a managed local server to test review behavior, security rejection, navigation, and accessibility end to end.
[^performance-budget]: Large-Saga measurements publish workload, benchmark command, budgets, and remaining bottlenecks instead of promising universal wall-clock numbers.
