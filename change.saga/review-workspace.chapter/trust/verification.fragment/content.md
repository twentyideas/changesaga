# Verification and reviewer checks {#verification}

The focused package tests exercise navigation in both directions, exact diff-range selection, annotation editing and withdrawal, repository-mismatch rejection, atomic failure cleanup, concurrent writers, rename-following attribution, and safe local HTTP defaults.[^verification-suite] Benchmarks cover large-Saga page construction, coverage projection, linked drawers, HTTP rendering, and batched attribution; they are performance signals, not latency promises.[^benchmark-scope]

Reviewer checks:

1. In the walkthrough, switch both directions and confirm each result identifies a precise target rather than only a file.
2. In the real workspace, open a visual landmark, expand its file, and confirm linked rows remain highlighted inside the full patch.
3. Draft an annotation only in a disposable Saga, exercise undo/redo, and inspect that submitting or editing adds a new record rather than rewriting an old one.
4. Attempt a foreign-repository diff anchor and a symlinked metadata path in a disposable fixture; both must fail before an externally visible write.
5. Rewrite or remove record history in a disposable repository and confirm attribution reports the explicit rewritten/unavailable state rather than trusting payload identity.

[^verification-suite]: Package tests cover the reviewer workspace, review record mutations, atomic storage, and Git-derived attribution contracts.
[^benchmark-scope]: Benchmarks exercise large local projections and bounded attribution work without asserting a universal timing threshold.
