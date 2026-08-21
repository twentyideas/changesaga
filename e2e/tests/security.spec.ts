import { tmpdir } from "node:os";
import {
  canonicalFileURI,
  canonicalLineURI,
  diffURI,
  formBody,
  multipartBody,
  readJSON,
  readMutationToken,
  reviewFiles,
  runCLI,
  serverRequest,
  stagedUploads,
  startSagaServer,
  stopSagaServer,
  treeSnapshot,
  type HTTPResponse,
  type SagaFixture
} from "../support/fixture-builder.js";
import { expect, test } from "../support/test.js";

const overviewTarget = "urn:change-saga:wave-one:fragment:wave-one-overview";

type Mutation = { path: string; describe: string; send: (headers: Record<string, string>) => Promise<HTTPResponse> };

/** Every mutating endpoint with a payload that would succeed if the gate opened. */
function mutations(fixture: SagaFixture): Mutation[] {
  const uri = canonicalFileURI(fixture.identity, "src/app.go");
  const post = (path: string, payload: { body: string | Buffer; headers: Record<string, string> }) => (headers: Record<string, string>) =>
    serverRequest(fixture.baseURL, path, { method: "POST", headers: { ...payload.headers, ...headers }, body: payload.body });
  return [
    { path: "/api/thread", describe: "new comment", send: post("/api/thread", multipartBody({ target: overviewTarget, anchor: '{"type":"target"}', body: "Forged comment." })) },
    { path: "/api/reply", describe: "reply", send: post("/api/reply", multipartBody({ thread: "missing", body: "Forged reply." })) },
    { path: "/api/thread-state", describe: "resolve", send: post("/api/thread-state", formBody({ thread: "missing", state: "resolved" })) },
    { path: "/api/thread-anchor", describe: "move annotation", send: post("/api/thread-anchor", formBody({ thread: "missing", anchor: '{"type":"target"}' })) },
    { path: "/api/review", describe: "approval", send: post("/api/review", formBody({ target: overviewTarget, state: "approved", body: "Forged approval." })) },
    { path: "/api/diff-review", describe: "file reviewed", send: post("/api/diff-review", formBody({ uri, state: "reviewed" })) }
  ];
}

test("@critical rejects every mutation without a valid session token and writes nothing", async ({ saga }) => {
  const before = treeSnapshot(saga.sagaRoot);
  const token = await readMutationToken(saga.baseURL);
  expect(token.length).toBeGreaterThan(40);

  for (const mutation of mutations(saga)) {
    for (const [label, headers] of [
      ["no token", {}],
      ["empty token", { "X-Change-Saga-Mutation-Token": "" }],
      ["wrong token", { "X-Change-Saga-Mutation-Token": "not-the-session-token" }],
      ["truncated token", { "X-Change-Saga-Mutation-Token": token.slice(0, -1) }]
    ] as const) {
      const response = await mutation.send(headers);
      expect(response.status, `${mutation.path} with ${label}`).toBe(403);
      expect(response.body, `${mutation.path} with ${label}`).toContain("mutation token");
    }
  }
  expect(treeSnapshot(saga.sagaRoot), "saga tree after rejected mutations").toBe(before);

  // Positive control: the same approval succeeds with the session token, so the
  // rejections above are the token gate and not a broken payload.
  const accepted = await serverRequest(saga.baseURL, "/api/review", {
    method: "POST",
    headers: { ...formBody({}).headers, "X-Change-Saga-Mutation-Token": token },
    body: formBody({ target: overviewTarget, state: "approved", body: "Genuine approval." }).body
  });
  expect(accepted.status).toBe(303);
  expect(reviewFiles(saga, /\/___approvals\/.*-approved\.json$/)).toHaveLength(1);
});

test("rejects a session token minted by a different server process", async ({ saga }) => {
  const before = treeSnapshot(saga.sagaRoot);
  const other = await startSagaServer(saga);
  try {
    const ownToken = await readMutationToken(saga.baseURL);
    const otherToken = await readMutationToken(other.baseURL);
    expect(otherToken).not.toBe(ownToken);

    const response = await serverRequest(saga.baseURL, "/api/review", {
      method: "POST",
      headers: { ...formBody({}).headers, "X-Change-Saga-Mutation-Token": otherToken },
      body: formBody({ target: overviewTarget, state: "approved", body: "Token from the neighbouring process." }).body
    });
    expect(response.status).toBe(403);
    expect(response.body).toContain("mutation token");
  } finally {
    await stopSagaServer(other);
  }
  expect(treeSnapshot(saga.sagaRoot)).toBe(before);
});

test("@critical rejects cross-origin and foreign-Host requests before any handler runs", async ({ saga }) => {
  const before = treeSnapshot(saga.sagaRoot);
  const token = await readMutationToken(saga.baseURL);
  const port = new URL(saga.baseURL).port;
  const approval = formBody({ target: overviewTarget, state: "approved", body: "Cross-origin approval." });

  for (const [label, headers] of [
    ["a foreign Origin", { Origin: "http://evil.test" }],
    ["a look-alike Origin", { Origin: `http://127.0.0.1.evil.test:${port}` }],
    ["cross-site fetch metadata", { "Sec-Fetch-Site": "cross-site" }],
    ["cross-origin fetch metadata", { "Sec-Fetch-Site": "cross-origin" }]
  ] as const) {
    const response = await serverRequest(saga.baseURL, "/api/review", {
      method: "POST",
      headers: { ...approval.headers, "X-Change-Saga-Mutation-Token": token, ...headers },
      body: approval.body
    });
    expect(response.status, `mutation with ${label}`).toBe(403);
    expect(response.body, `mutation with ${label}`).toContain("Cross-origin request rejected.");
  }

  for (const host of ["attacker.test", `attacker.test:${port}`, `127.0.0.1.evil.test:${port}`, `evil.test:${port}`]) {
    const response = await serverRequest(saga.baseURL, "/", { headers: { Host: host } });
    expect(response.status, `page request with Host ${host}`).toBe(403);
    expect(response.body).toContain("Invalid request host.");
  }

  // The two hostnames that genuinely address this loopback listener still work.
  for (const host of [`127.0.0.1:${port}`, `localhost:${port}`]) {
    const response = await serverRequest(saga.baseURL, "/", { headers: { Host: host } });
    expect(response.status, `page request with Host ${host}`).toBe(200);
  }
  const sameOrigin = await serverRequest(saga.baseURL, "/api/review", {
    method: "POST",
    headers: { ...approval.headers, "X-Change-Saga-Mutation-Token": token, Origin: `http://127.0.0.1:${port}`, "Sec-Fetch-Site": "same-origin" },
    body: approval.body
  });
  expect(sameOrigin.status).toBe(303);
  expect(treeSnapshot(saga.sagaRoot)).not.toBe(before);
});

test("@critical refuses malformed, non-canonical, and cross-repository diff URIs without writing", async ({ page, saga }) => {
  const canonical = canonicalFileURI(saga.identity, "src/app.go");
  await page.goto(`${saga.baseURL}/?view=code&file=${encodeURIComponent("src/app.go")}`);
  const rendered = await page.locator('article.file-diff[data-file-path="src/app.go"] form.file-review input[name="uri"]').getAttribute("value");
  // Positive control for the whole table below: the URI this suite builds is
  // byte-for-byte the canonical identity the product itself renders.
  expect(rendered).toBe(canonical);
  expect(canonical).toContain("path=src%2Fapp.go");

  const [, query] = canonical.split("?");
  const rejected: Array<[string, string]> = [
    ["empty", ""],
    ["not a URI", "not-a-diff-uri"],
    ["wrong scheme", canonical.replace("saga-diff://", "https://")],
    ["wrong version host", canonical.replace("saga-diff://v1/", "saga-diff://v2/")],
    ["unknown kind", canonical.replace("/file?", "/blob?")],
    ["reordered parameters", `saga-diff://v1/file?${query.split("&").reverse().join("&")}`],
    ["unescaped path separator", canonical.replace("path=src%2Fapp.go", "path=src/app.go")],
    ["lowercase percent escape", canonical.replace("path=src%2Fapp.go", "path=src%2fapp.go")],
    ["extra parameter", `${canonical}&view=split`],
    ["duplicated parameter", `${canonical}&path=src%2Fapp.go`],
    ["trailing fragment", `${canonical}#top`],
    ["userinfo", canonical.replace("saga-diff://v1/", "saga-diff://reviewer@v1/")],
    ["missing head", `saga-diff://v1/file?${query.split("&").filter((pair) => !pair.startsWith("head=")).join("&")}`],
    ["path traversal", diffURI("file", { repository: saga.identity.repository, base: saga.identity.base, head: saga.identity.head, path: "../../../etc/passwd" })],
    ["absolute path", diffURI("file", { repository: saga.identity.repository, base: saga.identity.base, head: saga.identity.head, path: "/etc/passwd" })],
    ["non-canonical repository casing", canonicalFileURI({ ...saga.identity, repository: "HTTPS://EXAMPLE.TEST/acme/change-saga-demo.git" }, "src/app.go")],
    ["repository with credentials", canonicalFileURI({ ...saga.identity, repository: "https://token@example.test/acme/change-saga-demo.git" }, "src/app.go")],
    ["cross-repository", canonicalFileURI({ ...saga.identity, repository: "https://example.test/acme/other-service.git" }, "src/app.go")],
    ["cross-host repository", canonicalFileURI({ ...saga.identity, repository: "https://evil.test/acme/change-saga-demo.git" }, "src/app.go")],
    ["line identity, not a file", canonicalLineURI(saga.identity, "src/app.go", "new", 3, 4)]
  ];

  const token = await readMutationToken(saga.baseURL);
  const before = treeSnapshot(saga.sagaRoot);
  for (const [label, uri] of rejected) {
    const payload = formBody({ uri, state: "reviewed", file: "src/app.go" });
    const response = await serverRequest(saga.baseURL, "/api/diff-review", {
      method: "POST",
      headers: { ...payload.headers, "X-Change-Saga-Mutation-Token": token },
      body: payload.body
    });
    expect(response.status, `diff review with ${label} URI`).toBe(400);
    expect(response.body, `diff review with ${label} URI`).not.toContain(saga.root);
  }
  expect(treeSnapshot(saga.sagaRoot), "saga tree after rejected diff identities").toBe(before);

  const accepted = formBody({ uri: canonical, state: "reviewed", file: "src/app.go" });
  const response = await serverRequest(saga.baseURL, "/api/diff-review", {
    method: "POST",
    headers: { ...accepted.headers, "X-Change-Saga-Mutation-Token": token },
    body: accepted.body
  });
  expect(response.status).toBe(303);
  const records = reviewFiles(saga, /\/___review\/diffs\/.*-reviewed\.json$/);
  expect(records).toHaveLength(1);
});

test("@critical rejects oversized and mistyped uploads, cleaning up every staged file", async ({ saga }) => {
  const token = await readMutationToken(saga.baseURL);
  const before = treeSnapshot(saga.sagaRoot);
  const fields = { target: overviewTarget, anchor: '{"type":"target"}', body: "Attachment check." };
  const send = async (payload: { body: Buffer; headers: Record<string, string> }): Promise<HTTPResponse> =>
    serverRequest(saga.baseURL, "/api/thread", {
      method: "POST",
      headers: { ...payload.headers, "X-Change-Saga-Mutation-Token": token },
      body: payload.body
    });

  const oversized = await send(multipartBody(fields, [{ field: "attachment", filename: "large.txt", content: Buffer.alloc(10 * 1024 * 1024 + 1, "x") }]));
  expect(oversized.status, "single attachment above the per-file limit").toBe(413);
  expect(oversized.body).toContain("size or file count");
  expect(stagedUploads(saga), "staged files after an oversized upload").toEqual([]);

  const tooMany = await send(multipartBody(fields, Array.from({ length: 9 }, (_, index) => ({ field: "attachment", filename: `note-${index}.txt`, content: "hello\n" }))));
  expect(tooMany.status, "more attachments than the limit allows").toBe(413);
  expect(stagedUploads(saga), "staged files after too many attachments").toEqual([]);

  const disguised = await send(multipartBody(fields, [{ field: "attachment", filename: "screenshot.png", content: "#!/bin/sh\necho nope\n" }]));
  expect(disguised.status, "declared PNG whose bytes are a shell script").toBe(400);
  expect(disguised.body).toContain("supported image");
  expect(stagedUploads(saga), "staged files after a sniffed-type mismatch").toEqual([]);

  const svgWithScript = await send(multipartBody(fields, [{ field: "attachment", filename: "diagram.svg", content: "<html><body>not an svg</body></html>" }]));
  expect(svgWithScript.status, "declared SVG whose bytes are HTML").toBe(400);
  expect(stagedUploads(saga), "staged files after a declared-SVG mismatch").toEqual([]);

  expect(treeSnapshot(saga.sagaRoot), "saga tree after rejected uploads").toBe(before);

  // Positive control: a genuine text attachment is accepted and staged cleanly.
  const accepted = await send(multipartBody(fields, [{ field: "attachment", filename: "note.txt", content: "reviewer note\n" }]));
  expect(accepted.status).toBe(303);
  const attachmentManifests = reviewFiles(saga, /\/attachment-01\.fragment\/fragment\.json$/);
  expect(attachmentManifests).toHaveLength(1);
  // The server names the stored file after its staged copy rather than the
  // upload, so the manifest entrypoint is the contract, not the original name.
  const attachment = readJSON<{ media_type: string; entrypoint: string }>(attachmentManifests[0]);
  expect(attachment.media_type).toBe("text/plain");
  expect(reviewFiles(saga, new RegExp(`/attachment-01\\.fragment/${attachment.entrypoint.replaceAll(".", "\\.")}$`))).toHaveLength(1);
  expect(stagedUploads(saga), "staged files after a successful upload").toEqual([]);
});

test("@critical never exposes filesystem paths in browser-facing responses", async ({ saga }) => {
  const token = await readMutationToken(saga.baseURL);
  const responses: HTTPResponse[] = [
    await serverRequest(saga.baseURL, "/"),
    await serverRequest(saga.baseURL, "/does-not-exist"),
    await serverRequest(saga.baseURL, "/chapters/missing-chapter"),
    await serverRequest(saga.baseURL, "/f/diagram/..%2F..%2F..%2Fetc%2Fpasswd"),
    await serverRequest(saga.baseURL, "/f/no-such-fragment/content.md"),
    await serverRequest(saga.baseURL, "/", { headers: { Host: "attacker.test" } })
  ];
  const payload = formBody({ uri: "not-a-diff-uri", state: "reviewed" });
  responses.push(
    await serverRequest(saga.baseURL, "/api/diff-review", { method: "POST", headers: { ...payload.headers, "X-Change-Saga-Mutation-Token": token }, body: payload.body }),
    await serverRequest(saga.baseURL, "/api/thread-state", { method: "POST", headers: formBody({}).headers, body: formBody({ thread: "../../escape", state: "resolved" }).body })
  );

  const secrets = [saga.root, saga.sagaRoot, saga.sourceRepo, saga.tempDir, tmpdir()];
  const phrases = ["no such file", "permission denied", "goroutine", "/private/var/folders"];
  for (const response of responses) {
    for (const secret of secrets) {
      expect(response.body, `response leaked ${secret}`).not.toContain(secret);
    }
    for (const phrase of phrases) {
      expect(response.body.toLowerCase(), `response leaked ${phrase}`).not.toContain(phrase);
    }
  }
});

test("@critical refuses to serve on a non-loopback address", async ({ sagaRepositories }) => {
  for (const address of ["0.0.0.0:0", "[::]:0", "192.0.2.10:7342", "example.test:7342"]) {
    const result = runCLI(sagaRepositories, ["serve", "--addr", address, sagaRepositories.sagaRoot]);
    expect(result.status, `serve --addr ${address}`).not.toBe(0);
    expect(`${result.stdout}${result.stderr}`, `serve --addr ${address}`).toContain("non-loopback");
    expect(result.stdout, `serve --addr ${address}`).not.toContain("Change Saga is available at");
  }
});
