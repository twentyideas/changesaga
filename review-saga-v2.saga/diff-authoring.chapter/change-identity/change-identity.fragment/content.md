# Absolute evidence addresses {#absolute-evidence-addresses}

`saga-diff://v1` URIs never inherit repository or revision context from the saga directory. Line links encode repository, base identity, product-patch identity, path, side, start, and end. Event links encode the same comparison plus rename, mode, or binary identity. File links identify reviewed-state scope but cannot satisfy coverage.

# Stable product identity {#stable-product-identity}

The base is a resolved commit OID. The head is a SHA-256 digest of the full binary product patch, excluding every path beneath a `.saga` directory. The same product comparison therefore has the same evidence identity whether the saga is uncommitted, committed beside the code, or maintained in another repository. Product edits—including binary content—change the digest.

# Parsing and matching {#parsing-and-matching}

The Git parser emits one atom per added/deleted line and one atom per supported file event. Coverage range matching requires repository, base, head identity, path, and side equality before checking containment. This prevents a familiar-looking path in another repository or revision from silently receiving credit.
