import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  canonicalFileURI,
  canonicalLineURI,
  declaredRepository,
  diffURI,
  git,
  reviewFiles,
  runCLI,
  treeSnapshot
} from "../support/fixture-builder.js";
import { expect, test } from "../support/test.js";

test("@critical refuses malformed, non-canonical, and cross-repository coverage URIs without writing", async ({ sagaRepositories }) => {
  const { identity, sagaRoot, sourceRepo } = sagaRepositories;
  const canonical = canonicalLineURI(identity, "src/app.go", "new", 3, 4);
  expect(canonical).toContain("path=src%2Fapp.go");
  const [, query] = canonical.split("?");

  const rejected: Array<[string, string]> = [
    ["not a URI", "no-scheme-here"],
    ["wrong scheme", canonical.replace("saga-diff://", "http://")],
    ["wrong version host", canonical.replace("saga-diff://v1/", "saga-diff://v9/")],
    ["unknown kind", canonical.replace("/line?", "/hunk?")],
    ["reordered parameters", `saga-diff://v1/line?${query.split("&").reverse().join("&")}`],
    ["unescaped path separator", canonical.replace("path=src%2Fapp.go", "path=src/app.go")],
    ["lowercase percent escape", canonical.replace("path=src%2Fapp.go", "path=src%2fapp.go")],
    ["extra parameter", `${canonical}&note=hi`],
    ["duplicated parameter", `${canonical}&side=new`],
    ["trailing fragment", `${canonical}#L3`],
    ["inverted range", canonicalLineURI(identity, "src/app.go", "new", 9, 2)],
    ["missing side", `saga-diff://v1/line?${query.split("&").filter((pair) => !pair.startsWith("side=")).join("&")}`],
    ["path traversal", diffURI("line", { repository: identity.repository, base: identity.base, head: identity.head, path: "../../etc/passwd", side: "new", start: 1, end: 1 })],
    ["non-canonical repository casing", canonicalLineURI({ ...identity, repository: `${declaredRepository.toUpperCase()}` }, "src/app.go", "new", 3, 4)],
    ["repository with credentials", canonicalLineURI({ ...identity, repository: "https://token@example.test/acme/change-saga-demo.git" }, "src/app.go", "new", 3, 4)],
    ["repository with trailing slash", canonicalLineURI({ ...identity, repository: `${declaredRepository}/` }, "src/app.go", "new", 3, 4)],
    ["cross-repository", canonicalLineURI({ ...identity, repository: "https://example.test/acme/other-service.git" }, "src/app.go", "new", 3, 4)],
    ["cross-host repository", canonicalLineURI({ ...identity, repository: "https://evil.test/acme/change-saga-demo.git" }, "src/app.go", "new", 3, 4)],
    ["file identity used as evidence", canonicalFileURI({ ...identity, repository: "https://example.test/acme/other-service.git" }, "src/app.go")]
  ];

  const before = treeSnapshot(sagaRoot);
  for (const [label, uri] of rejected) {
    const result = runCLI(sagaRepositories, ["cover", "--repo", sourceRepo, "--target", "overview.fragment", "--name", "must-not-exist", "--uri", uri, sagaRoot]);
    expect(result.status, `cover with ${label} URI`).not.toBe(0);
    expect(`${result.stdout}${result.stderr}`, `cover with ${label} URI`).toMatch(/invalid --uri|does not match the saga source repository/);
  }
  expect(treeSnapshot(sagaRoot), "saga tree after rejected coverage URIs").toBe(before);
  expect(reviewFiles(sagaRepositories, /must-not-exist/)).toEqual([]);

  // Positive control: the same shape of URI, with this saga's own identity, is
  // accepted and written, so the rejections above are the identity check.
  const accepted = runCLI(sagaRepositories, ["cover", "--repo", sourceRepo, "--target", "overview.fragment", "--name", "accepted-evidence", "--uri", canonical, sagaRoot]);
  expect(accepted.status, accepted.stderr).toBe(0);
  expect(reviewFiles(sagaRepositories, /___diffs\/accepted-evidence\.json$/)).toHaveLength(1);
});

test("@critical exposes mapping scrutiny, claims, and verification as an AI review harness", async ({ sagaRepositories }) => {
  const { identity, sagaRepo, sagaRoot, sourceRepo } = sagaRepositories;
  const evidence = canonicalLineURI(identity, "src/app.go", "new", 3, 3);

  const claim = runCLI(sagaRepositories, [
    "add-claim", "--id", "greeting-behavior", "--target", "overview.fragment#greeting-input", "--kind", "behavior",
    "--statement", "Greeting accepts a name in its function signature.", "--diff", evidence, sagaRoot
  ]);
  expect(claim.status, claim.stderr).toBe(0);
  const verification = runCLI(sagaRepositories, [
    "verify-claim", "--id", "greeting-inspection", "--claim", "greeting-behavior", "--status", "verified",
    "--method", "inspection", "--summary", "The changed function signature and return expression were inspected.", sagaRoot
  ]);
  expect(verification.status, verification.stderr).toBe(0);
  expect(reviewFiles(sagaRepositories, /___claims\/greeting-behavior\.json$/)).toHaveLength(1);
  expect(reviewFiles(sagaRepositories, /___verifications\/greeting-inspection\.json$/)).toHaveLength(1);

  git(sagaRepo, "add", ".");
  git(sagaRepo, "commit", "-m", "record author claim and verification");

  const mappings = runCLI(sagaRepositories, ["query", "mappings", "--saga", sagaRoot, "--repo", sourceRepo, "--sort", "scrutiny"]);
  expect(mappings.status, mappings.stderr).toBe(0);
  const mappingEnvelope = JSON.parse(mappings.stdout) as { data: { mappings: Array<{ scrutiny_score: number; atoms_per_note: number; target_file_count: number; reasons: unknown[] }> } };
  expect(mappingEnvelope.data.mappings.length).toBeGreaterThan(0);
  expect(mappingEnvelope.data.mappings[0]).toEqual(expect.objectContaining({ scrutiny_score: expect.any(Number), atoms_per_note: expect.any(Number), target_file_count: expect.any(Number), reasons: expect.any(Array) }));

  const claims = runCLI(sagaRepositories, ["query", "claims", "--saga", sagaRoot, "--repo", sourceRepo, "--status", "verified"]);
  expect(claims.status, claims.stderr).toBe(0);
  const claimEnvelope = JSON.parse(claims.stdout) as { data: { claims: Array<{ id: string; verification_status: string; attribution: { status: string }; evidence: Array<{ mapped_to_target: boolean }> }> } };
  expect(claimEnvelope.data.claims).toHaveLength(1);
  expect(claimEnvelope.data.claims[0]).toEqual(expect.objectContaining({ id: "greeting-behavior", verification_status: "verified", attribution: expect.objectContaining({ status: "committed" }) }));
  expect(claimEnvelope.data.claims[0].evidence.every((item) => item.mapped_to_target)).toBe(true);

  const verifications = runCLI(sagaRepositories, ["query", "verifications", "--saga", sagaRoot, "--repo", sourceRepo, "--claim", "greeting-behavior"]);
  expect(verifications.status, verifications.stderr).toBe(0);
  const verificationEnvelope = JSON.parse(verifications.stdout) as { data: { verifications: Array<{ id: string; status: string; attribution: { status: string } }> } };
  expect(verificationEnvelope.data.verifications).toEqual([
    expect.objectContaining({ id: "greeting-inspection", status: "verified", attribution: expect.objectContaining({ status: "committed" }) })
  ]);

  const owners = runCLI(sagaRepositories, ["query", "diff-owners", "--saga", sagaRoot, "--repo", sourceRepo, "--diff", evidence]);
  expect(owners.status, owners.stderr).toBe(0);
  const ownerEnvelope = JSON.parse(owners.stdout) as { data: { atoms: Array<{ owners: Array<{ mapping?: { scrutiny_score: number } }> }> } };
  expect(ownerEnvelope.data.atoms.flatMap((atom) => atom.owners).some((owner) => typeof owner.mapping?.scrutiny_score === "number")).toBe(true);

  const status = runCLI(sagaRepositories, ["status", "--repo", sourceRepo, sagaRoot]);
  expect(status.status, status.stderr).toBe(0);
  expect(status.stdout).toContain("ALL ATOMS MAPPED");
  expect(status.stdout).toContain("does not establish explanation quality or correctness");
});

test("@critical refuses to mutate or serve a structurally invalid saga with zero side effects", async ({ sagaRepositories }) => {
  const { sagaRoot, sourceRepo } = sagaRepositories;
  const manifestPath = join(sagaRoot, "saga.json");
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as Record<string, unknown>;
  // A loadable manifest that fails schema validation: exactly the state the
  // product promises never to write review records into.
  writeFileSync(manifestPath, `${JSON.stringify({ ...manifest, version: 999 }, null, 2)}\n`);

  const validation = runCLI(sagaRepositories, ["validate", sagaRoot]);
  expect(validation.status, "validate must report the corrupted saga").not.toBe(0);
  expect(validation.stdout).toContain("Invalid saga");

  const before = treeSnapshot(sagaRoot);
  const refused: Array<[string, string[]]> = [
    ["comment", ["thread", "--target", "overview.fragment", "--body", "Should never be stored.", sagaRoot]],
    ["reply", ["reply", "--thread", "20250101T000000000Z", "--body", "Should never be stored.", sagaRoot]],
    ["approval", ["review", "--target", "overview.fragment", "--state", "approved", "--reviewer-kind", "human", "--body", "Should never be stored.", sagaRoot]],
    ["rejection", ["review", "--target", ".", "--state", "rejected", "--reviewer-kind", "human", sagaRoot]]
  ];
  for (const [label, args] of refused) {
    const result = runCLI(sagaRepositories, args);
    expect(result.status, `${label} on an invalid saga`).not.toBe(0);
    expect(`${result.stdout}${result.stderr}`, `${label} on an invalid saga`).toContain("structurally invalid");
  }

  const serve = runCLI(sagaRepositories, ["serve", "--addr", "127.0.0.1:0", "--repo", sourceRepo, sagaRoot]);
  expect(serve.status, "serve on an invalid saga").not.toBe(0);
  expect(`${serve.stdout}${serve.stderr}`).toContain("structurally invalid");
  expect(serve.stdout).not.toContain("Change Saga is available at");

  // `init` scaffolds the empty record directories, so the contract is that they
  // stay empty and that nothing anywhere under the saga moved.
  expect(reviewFiles(sagaRepositories, /___review\//), "review records after refused mutations").toEqual([]);
  expect(reviewFiles(sagaRepositories, /___approvals\//), "approval records after refused mutations").toEqual([]);
  expect(treeSnapshot(sagaRoot), "saga tree after refused mutations").toBe(before);
});

test("@critical refuses a checkout whose origin does not match the declared repository", async ({ sagaRepositories }) => {
  const { sagaRoot, sourceRepo } = sagaRepositories;
  git(sourceRepo, "remote", "set-url", "origin", "https://example.test/acme/impostor.git");
  const before = treeSnapshot(sagaRoot);

  const status = runCLI(sagaRepositories, ["status", "--repo", sourceRepo, sagaRoot]);
  expect(status.status, "status against a mismatched checkout").not.toBe(0);
  expect(`${status.stdout}${status.stderr}`).toContain("does not match declared repository");

  const cover = runCLI(sagaRepositories, ["cover", "--repo", sourceRepo, "--target", "overview.fragment", "--path", "src/app.go", "--side", "new", "--lines", "3", "--name", "must-not-exist", sagaRoot]);
  expect(cover.status, "cover against a mismatched checkout").not.toBe(0);
  expect(`${cover.stdout}${cover.stderr}`).toContain("does not match declared repository");
  expect(treeSnapshot(sagaRoot), "saga tree after a refused mismatched checkout").toBe(before);

  // The override exists and is explicit; nothing else unblocks the check.
  const overridden = runCLI(sagaRepositories, ["cover", "--repo", sourceRepo, "--allow-repository-mismatch", "--target", "overview.fragment", "--path", "src/app.go", "--side", "new", "--lines", "3", "--name", "explicit-override", sagaRoot]);
  expect(overridden.status, overridden.stderr).toBe(0);

  git(sourceRepo, "remote", "set-url", "origin", declaredRepository);
});

test("@critical projects a PR diff and PR Saga onto the codebase Saga without comparing authored content", async ({ sagaRepositories }) => {
  const { sagaRepo, sagaRoot, sourceRepo } = sagaRepositories;
  const incomingBase = git(sourceRepo, "rev-parse", "HEAD");
  writeFileSync(join(sourceRepo, "src", "app.go"), `package demo\n\nfunc Greeting(name string) string {\n\treturn "welcome, " + name\n}\n\nfunc Ready() bool {\n\treturn true\n}\n\nfunc Audited() bool {\n\treturn true\n}\n`);
  writeFileSync(join(sourceRepo, "new-capability.go"), "package demo\n\nconst NewCapability = true\n");
  git(sourceRepo, "add", ".");
  git(sourceRepo, "commit", "-m", "change greeting and add capability");

  const direct = runCLI(sagaRepositories, [
    "compare", "--json", "--repo", sourceRepo, "--base", incomingBase, "--head", "HEAD", sagaRoot
  ]);
  expect(direct.status, direct.stderr).toBe(0);
  const directResult = JSON.parse(direct.stdout) as {
    schema: string;
    mode: string;
    basis: string;
    content_compared: boolean;
    baseline: { complete: boolean };
    summary: { direct_intersections: number; contextual_additions: number; new_content_required: number; targets_must_update: number };
    targets: Array<{ action: string; target: string; content_path?: string; changes: Array<{ relationship: string }> }>;
    new_content: Array<{ atom: { path: string } }>;
  };
  expect(directResult.schema).toBe("change-saga.impact/v1");
  expect(directResult.mode).toBe("saga_to_diff");
  expect(directResult.basis).toBe("source_diffs_only");
  expect(directResult.content_compared).toBe(false);
  expect(directResult.baseline.complete).toBe(true);
  expect(directResult.summary.direct_intersections).toBeGreaterThan(0);
  expect(directResult.summary.contextual_additions).toBeGreaterThan(0);
  expect(directResult.summary.targets_must_update).toBeGreaterThan(0);
  expect(directResult.targets.some((target) => target.action === "must_update" && target.content_path?.endsWith("content.md"))).toBe(true);
  expect(directResult.new_content.some((change) => change.atom.path === "new-capability.go")).toBe(true);

  const prSaga = join(sagaRepo, "incoming-pr.saga");
  const initialized = runCLI(sagaRepositories, [
    "init", "--repo", sourceRepo, "--repository", declaredRepository, "--base", incomingBase, "--head", "HEAD",
    "--id", "incoming-pr", "--title", "Incoming PR", prSaga
  ]);
  expect(initialized.status, initialized.stderr).toBe(0);
  const sagaComparison = runCLI(sagaRepositories, [
    "compare", "--json", "--repo", sourceRepo, "--against-repo", sourceRepo, "--against-saga", prSaga, sagaRoot
  ]);
  expect(sagaComparison.status, sagaComparison.stderr).toBe(0);
  const sagaResult = JSON.parse(sagaComparison.stdout) as { mode: string; incoming: { saga_id: string }; summary: typeof directResult.summary };
  expect(sagaResult.mode).toBe("saga_to_saga");
  expect(sagaResult.incoming.saga_id).toBe("incoming-pr");
  expect(sagaResult.summary).toEqual(directResult.summary);
});
