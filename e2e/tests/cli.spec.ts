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
    ["approval", ["review", "--target", "overview.fragment", "--state", "approved", "--body", "Should never be stored.", sagaRoot]],
    ["rejection", ["review", "--target", ".", "--state", "rejected", sagaRoot]]
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
