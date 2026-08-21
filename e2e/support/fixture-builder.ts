import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from "node:child_process";
import { request as httpRequest } from "node:http";
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, relative, resolve } from "node:path";
import type { TestInfo } from "@playwright/test";
import { binaryPath } from "./global-setup.js";

const reviewer = { name: "E2E Reviewer", email: "reviewer@example.test" };
const author = { name: "Source Author", email: "author@example.test" };

export const declaredRepository = "https://example.test/acme/change-saga-demo.git";

type StatusAtom = { uri: string; path: string; side?: string; line?: number };
type StatusReport = { uncovered: StatusAtom[] | null; repository: string; base_oid: string; head_oid: string };

/** The exact comparison identity every diff URI in this fixture must carry. */
export type DiffIdentity = { repository: string; base: string; head: string };

export type SagaRepositories = {
  root: string;
  sourceRepo: string;
  sagaRepo: string;
  sagaRoot: string;
  /** Private TMPDIR handed to every server subprocess so upload staging is observable. */
  tempDir: string;
  identity: DiffIdentity;
};

export type SagaServer = {
  baseURL: string;
  server: ChildProcessWithoutNullStreams;
  serverLog: string[];
};

export type SagaFixture = SagaRepositories & SagaServer;

function write(path: string, contents: string | Buffer): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, contents);
}

function command(file: string, args: string[], cwd: string, env?: NodeJS.ProcessEnv): string {
  const result = run(file, args, cwd, env);
  if (result.status !== 0) {
    throw new Error(`${file} ${args.join(" ")} failed (${result.status})\n${result.stdout}\n${result.stderr}`);
  }
  return result.stdout.trim();
}

export type CommandResult = { status: number | null; stdout: string; stderr: string };

function run(file: string, args: string[], cwd: string, env?: NodeJS.ProcessEnv): CommandResult {
  // A bounded timeout keeps a command that unexpectedly starts serving from
  // hanging the worker; every command here is otherwise short-lived.
  const result = spawnSync(file, args, { cwd, env: { ...process.env, ...env }, encoding: "utf8", timeout: 60_000 });
  return { status: result.status, stdout: result.stdout ?? "", stderr: result.stderr ?? "" };
}

export function git(cwd: string, ...args: string[]): string {
  return command("git", args, cwd);
}

/** Runs the real CLI and hands the test its exit status instead of throwing. */
export function runCLI(fixture: SagaRepositories, args: string[], cwd = dirname(fixture.sagaRoot)): CommandResult {
  return run(binaryPath, args, cwd, { TMPDIR: fixture.tempDir });
}

function configureGit(cwd: string, identity: { name: string; email: string }): void {
  git(cwd, "config", "user.name", identity.name);
  git(cwd, "config", "user.email", identity.email);
  git(cwd, "config", "commit.gpgsign", "false");
}

function runSaga(args: string[], cwd: string): string {
  return command(binaryPath, args, cwd);
}

function statusReport(sourceRepo: string, sagaRoot: string): StatusReport {
  const result = spawnSync(binaryPath, ["status", "--json", "--repo", sourceRepo, sagaRoot], {
    cwd: dirname(sagaRoot),
    encoding: "utf8"
  });
  // `status` deliberately exits 3 while a valid saga still has uncovered
  // changes. Fixture construction consumes that report and closes the gaps.
  if (result.status !== 0 && result.status !== 3) {
    throw new Error(`status failed (${result.status})\n${result.stdout}\n${result.stderr}`);
  }
  return JSON.parse(result.stdout) as StatusReport;
}

function addCoverage(sourceRepo: string, sagaRoot: string): DiffIdentity {
  const initial = statusReport(sourceRepo, sagaRoot);
  const uncovered = initial.uncovered ?? [];
  if (uncovered.length === 0) throw new Error("fixture source comparison unexpectedly has no changed atoms");
  const overview = uncovered.filter((atom) => atom.path === "src/app.go" || atom.path === "docs/guide.md");
  const architecture = uncovered.filter((atom) => !overview.includes(atom));
  for (const [target, name, atoms] of [
    ["overview.fragment", "overview-linked", overview],
    ["architecture.chapter/overview.fragment", "architecture-linked", architecture]
  ] as const) {
    if (atoms.length === 0) throw new Error(`fixture has no atoms for ${target}`);
    runSaga([
      "cover", "--repo", sourceRepo, "--target", target, "--name", name,
      ...atoms.flatMap((atom) => ["--uri", atom.uri]),
      sagaRoot
    ], dirname(sagaRoot));
  }
  const report = statusReport(sourceRepo, sagaRoot);
  if ((report.uncovered ?? []).length !== 0) throw new Error(`fixture coverage left ${report.uncovered?.length ?? 0} atoms uncovered`);
  if (report.repository !== declaredRepository) throw new Error(`fixture repository identity drifted to ${report.repository}`);
  return { repository: report.repository, base: report.base_oid, head: report.head_oid };
}

function buildSourceRepository(root: string): { sourceRepo: string; base: string; head: string } {
  const sourceRepo = join(root, "source-repo");
  mkdirSync(sourceRepo, { recursive: true });
  git(sourceRepo, "init", "-b", "main");
  configureGit(sourceRepo, author);
  // A real reviewed checkout has an origin, and the CLI verifies the declared
  // repository against it on every read. Without this the fixture could only be
  // built by opting out of the identity check the product exists to enforce.
  git(sourceRepo, "remote", "add", "origin", declaredRepository);
  write(join(sourceRepo, "src", "app.go"), `package demo\n\nfunc Greeting() string {\n\treturn "hello"\n}\n`);
  write(join(sourceRepo, "docs", "guide.md"), `# Guide\n\nThe original workflow is synchronous.\n`);
  write(join(sourceRepo, "assets", "ui", "existing.txt"), "existing asset\n");
  git(sourceRepo, "add", ".");
  git(sourceRepo, "commit", "-m", "base source");
  const base = git(sourceRepo, "rev-parse", "HEAD");

  git(sourceRepo, "checkout", "-b", "feature/wave-one");
  write(join(sourceRepo, "src", "app.go"), `package demo\n\nfunc Greeting(name string) string {\n\treturn "hello, " + name\n}\n\nfunc Ready() bool {\n\treturn true\n}\n`);
  write(join(sourceRepo, "docs", "guide.md"), `# Guide\n\nThe workflow now records each review step.\nReloading preserves the review history.\n`);
  write(join(sourceRepo, "assets", "ui", "theme.css"), `.review {\n  color: #24364b;\n}\n`);
  git(sourceRepo, "add", ".");
  git(sourceRepo, "commit", "-m", "add review workflow");
  return { sourceRepo, base, head: git(sourceRepo, "rev-parse", "HEAD") };
}

function buildSagaRepository(root: string, source: { sourceRepo: string; base: string; head: string }): { sagaRepo: string; sagaRoot: string; identity: DiffIdentity } {
  const sagaRepo = join(root, "saga-repo");
  const sagaRoot = join(sagaRepo, "wave-one.saga");
  mkdirSync(sagaRepo, { recursive: true });
  git(sagaRepo, "init", "-b", "main");
  configureGit(sagaRepo, reviewer);

  runSaga([
    "init", "--repo", source.sourceRepo, "--repository", declaredRepository,
    "--base", source.base, "--head", source.head, "--id", "wave-one", "--title", "Wave One Review", sagaRoot
  ], sagaRepo);
  write(join(sagaRoot, "overview.fragment", "content.md"), `# Review overview {#review-overview}\n\nWave 1 connects the story to the exact source changes.\n\n## Reviewer path {#reviewer-path}\n\nStart with the behavior, then follow the linked code.\n`);
  runSaga(["add-chapter", "--id", "architecture", "--title", "Architecture", sagaRoot, "architecture"], sagaRepo);
  write(join(sagaRoot, "architecture.chapter", "overview.fragment", "content.md"), `# Architecture path {#architecture-path}\n\nThe renderer and persistence boundary stay independent.\n`);

  const mediaRoot = join(root, "media");
  const interactiveRoot = join(mediaRoot, "interactive");
  write(join(mediaRoot, "diagram.svg"), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 80" role="img" aria-label="Review flow diagram"><rect width="240" height="80" fill="#eef4ff"/><text x="20" y="46">Saga to source</text></svg>\n`);
  write(join(mediaRoot, "pixel.png"), Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"));
  write(join(interactiveRoot, "index.html"), `<!doctype html><button id="run">Run demo</button><output id="result">idle</output><script src="app.js"></script>\n`);
  write(join(interactiveRoot, "app.js"), `document.querySelector('#run').addEventListener('click', () => { document.querySelector('#result').textContent = 'interactive ready'; });\n`);
  runSaga(["add-fragment", "--section", ".", "--type", "svg", "--name", "diagram", "--id", "diagram", "--title", "Architecture Diagram", "--source", join(mediaRoot, "diagram.svg"), sagaRoot], sagaRepo);
  runSaga(["add-fragment", "--section", ".", "--type", "image", "--name", "pixel", "--id", "pixel", "--title", "Raster Preview", "--source", join(mediaRoot, "pixel.png"), sagaRoot], sagaRepo);
  runSaga(["add-fragment", "--section", ".", "--type", "html", "--name", "interactive", "--id", "interactive", "--title", "Interactive Demo", "--source", interactiveRoot, sagaRoot], sagaRepo);

  const identity = addCoverage(source.sourceRepo, sagaRoot);
  git(sagaRepo, "add", ".");
  git(sagaRepo, "commit", "-m", "add Wave 1 saga fixture");
  return { sagaRepo, sagaRoot, identity };
}

export async function startSagaServer(repositories: SagaRepositories, extraArgs: string[] = []): Promise<SagaServer> {
  const server = spawn(binaryPath, ["serve", "--addr", "127.0.0.1:0", "--repo", repositories.sourceRepo, ...extraArgs, repositories.sagaRoot], {
    cwd: dirname(repositories.sagaRoot),
    // A private TMPDIR keeps every staged upload this process creates inside the
    // fixture, so a test can prove rejected uploads leave nothing behind.
    env: { ...process.env, TMPDIR: repositories.tempDir },
    stdio: ["pipe", "pipe", "pipe"]
  });
  const serverLog: string[] = [];
  const ready = new Promise<string>((resolveReady, reject) => {
    const timeout = setTimeout(() => reject(new Error(`server did not become ready\n${serverLog.join("")}`)), 15_000);
    const capture = (chunk: Buffer): void => {
      const text = chunk.toString();
      serverLog.push(text);
      const match = serverLog.join("").match(/Change Saga is available at (http:\/\/127\.0\.0\.1:\d+)/);
      if (match) {
        clearTimeout(timeout);
        resolveReady(match[1]);
      }
    };
    server.stdout.on("data", capture);
    server.stderr.on("data", capture);
    server.once("error", (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    server.once("exit", (code) => {
      clearTimeout(timeout);
      reject(new Error(`server exited before ready (${code})\n${serverLog.join("")}`));
    });
  });
  return { server, baseURL: await ready, serverLog };
}

/** Builds both Git repositories without starting a server or a browser page. */
export function createSagaRepositories(testInfo: TestInfo): SagaRepositories {
  const safeTitle = testInfo.title.replace(/[^a-z0-9]+/gi, "-").replace(/^-|-$/g, "").slice(0, 48).toLowerCase() || "test";
  const root = mkdtempSync(join(tmpdir(), `change-saga-e2e-${testInfo.workerIndex}-${safeTitle}-`));
  try {
    const tempDir = join(root, "server-tmp");
    mkdirSync(tempDir, { recursive: true });
    const source = buildSourceRepository(root);
    const saga = buildSagaRepository(root, source);
    return { root, sourceRepo: source.sourceRepo, tempDir, ...saga };
  } catch (error) {
    rmSync(root, { recursive: true, force: true });
    throw error;
  }
}

export async function createSagaFixture(testInfo: TestInfo): Promise<SagaFixture> {
  const repositories = createSagaRepositories(testInfo);
  try {
    return { ...repositories, ...(await startSagaServer(repositories)) };
  } catch (error) {
    rmSync(repositories.root, { recursive: true, force: true });
    throw error;
  }
}

function snapshotTree(root: string): Record<string, string | { size: number }> {
  const result: Record<string, string | { size: number }> = {};
  const visit = (directory: string): void => {
    for (const entry of readdirSync(directory).sort()) {
      if (entry === ".git") continue;
      const path = join(directory, entry);
      const key = relative(root, path).split("\\").join("/");
      const stats = statSync(path);
      if (stats.isDirectory()) visit(path);
      else if (stats.size <= 128 * 1024 && !/\.(png|jpg|jpeg|gif|webp)$/i.test(entry)) result[key] = readFileSync(path, "utf8").replaceAll(root, "<fixture-root>");
      else result[key] = { size: stats.size };
    }
  };
  visit(root);
  return result;
}

export async function attachFailureState(fixture: SagaRepositories & Partial<SagaServer>, testInfo: TestInfo, browserEvents: string[]): Promise<void> {
  if (testInfo.status === testInfo.expectedStatus) return;
  const state = {
    paths: { root: "<fixture-root>", sourceRepo: "<fixture-root>/source-repo", sagaRepo: "<fixture-root>/saga-repo" },
    sourceGit: git(fixture.sourceRepo, "status", "--short"),
    sagaGit: git(fixture.sagaRepo, "status", "--short"),
    sagaLog: git(fixture.sagaRepo, "log", "--format=%h %an <%ae> %s", "-5"),
    files: snapshotTree(fixture.root)
  };
  const artifacts = [
    { name: "server.log", body: (fixture.serverLog ?? []).join(""), contentType: "text/plain" },
    { name: "browser-events.json", body: `${JSON.stringify(browserEvents, null, 2)}\n`, contentType: "application/json" },
    { name: "sanitized-fixture-state.json", body: `${JSON.stringify(state, null, 2)}\n`, contentType: "application/json" }
  ];
  for (const artifact of artifacts) {
    const path = testInfo.outputPath(artifact.name);
    writeFileSync(path, artifact.body);
    await testInfo.attach(artifact.name, { path, contentType: artifact.contentType });
  }
}

export async function stopSagaServer(running: SagaServer): Promise<void> {
  if (running.server.exitCode === null) {
    running.server.kill("SIGINT");
    await Promise.race([
      new Promise<void>((resolveExit) => running.server.once("exit", () => resolveExit())),
      new Promise<void>((resolveTimeout) => setTimeout(resolveTimeout, 5_000))
    ]);
    if (running.server.exitCode === null) running.server.kill("SIGKILL");
  }
}

export async function stopSagaFixture(fixture: SagaFixture): Promise<void> {
  await stopSagaServer(fixture);
  rmSync(fixture.root, { recursive: true, force: true });
}

export function reviewFiles(fixture: Pick<SagaRepositories, "sagaRoot">, pattern: RegExp): string[] {
  const files: string[] = [];
  const visit = (directory: string): void => {
    if (!statSync(directory).isDirectory()) return;
    for (const entry of readdirSync(directory)) {
      const path = join(directory, entry);
      if (statSync(path).isDirectory()) visit(path);
      else if (pattern.test(path)) files.push(path);
    }
  };
  visit(fixture.sagaRoot);
  return files.sort();
}

/**
 * Every path and size under a directory, so a test can prove that a rejected
 * request created, deleted, or rewrote nothing at all.
 */
export function treeSnapshot(root: string): string {
  const lines: string[] = [];
  const visit = (directory: string): void => {
    for (const entry of readdirSync(directory).sort()) {
      const path = join(directory, entry);
      const stats = statSync(path);
      const key = relative(root, path).split("\\").join("/");
      if (stats.isDirectory()) {
        lines.push(`${key}/`);
        visit(path);
      } else {
        lines.push(`${key} ${stats.size}`);
      }
    }
  };
  visit(root);
  return lines.sort().join("\n");
}

/** Upload staging files the server has left behind in its private TMPDIR. */
export function stagedUploads(fixture: SagaRepositories): string[] {
  return readdirSync(fixture.tempDir).filter((entry) => entry.startsWith("change-saga-attachment-")).sort();
}

export function readJSON<T = Record<string, unknown>>(path: string): T {
  return JSON.parse(readFileSync(path, "utf8")) as T;
}

export function relativeToSaga(fixture: Pick<SagaRepositories, "sagaRoot">, path: string): string {
  return relative(fixture.sagaRoot, resolve(path)).split("\\").join("/");
}

export function fileName(path: string): string {
  return basename(path);
}

/**
 * Reproduces Go's `url.Values.Encode` escaping: every byte outside the
 * unreserved set is percent-encoded with uppercase hex, and a space becomes `+`.
 * `canonicalDiffURI` therefore builds byte-for-byte the same string the product
 * considers canonical, which a positive control in the suite verifies.
 */
function encodeQueryComponent(value: string): string {
  return [...new TextEncoder().encode(value)]
    .map((byte) => {
      const char = String.fromCharCode(byte);
      if (/[A-Za-z0-9\-_.~]/.test(char)) return char;
      if (char === " ") return "+";
      return `%${byte.toString(16).toUpperCase().padStart(2, "0")}`;
    })
    .join("");
}

export function diffURI(kind: "file" | "line" | "event", parameters: Record<string, string | number>): string {
  const query = Object.keys(parameters)
    .sort()
    .map((key) => `${encodeQueryComponent(key)}=${encodeQueryComponent(String(parameters[key]))}`)
    .join("&");
  return `saga-diff://v1/${kind}?${query}`;
}

export function canonicalFileURI(identity: DiffIdentity, path: string): string {
  return diffURI("file", { repository: identity.repository, base: identity.base, head: identity.head, path });
}

export function canonicalLineURI(identity: DiffIdentity, path: string, side: "old" | "new", start: number, end: number): string {
  return diffURI("line", { repository: identity.repository, base: identity.base, head: identity.head, path, side, start, end });
}

export type HTTPResponse = { status: number; body: string; headers: Record<string, string | string[] | undefined> };

/**
 * Talks to the running server process over raw HTTP so a test can control the
 * headers a browser refuses to forge — `Host` and `Origin` above all.
 */
export function serverRequest(
  baseURL: string,
  path: string,
  options: { method?: string; headers?: Record<string, string>; body?: string | Buffer } = {}
): Promise<HTTPResponse> {
  const target = new URL(path, baseURL);
  const body = options.body === undefined ? undefined : Buffer.from(options.body);
  return new Promise((resolveResponse, reject) => {
    const outgoing = httpRequest(
      {
        hostname: target.hostname,
        port: target.port,
        path: target.pathname + target.search,
        method: options.method ?? "GET",
        headers: { ...(body ? { "Content-Length": String(body.length) } : {}), ...options.headers }
      },
      (response) => {
        const chunks: Buffer[] = [];
        response.on("data", (chunk: Buffer) => chunks.push(chunk));
        response.on("end", () => resolveResponse({ status: response.statusCode ?? 0, body: Buffer.concat(chunks).toString("utf8"), headers: response.headers }));
      }
    );
    outgoing.setTimeout(15_000, () => outgoing.destroy(new Error(`request to ${path} timed out`)));
    outgoing.on("error", reject);
    if (body) outgoing.write(body);
    outgoing.end();
  });
}

export async function readMutationToken(baseURL: string): Promise<string> {
  const page = await serverRequest(baseURL, "/");
  const match = page.body.match(/<meta name="change-saga-mutation-token" content="([^"]*)">/);
  if (!match) throw new Error(`page did not carry a mutation token\n${page.body.slice(0, 400)}`);
  return match[1];
}

export function formBody(fields: Record<string, string>): { body: string; headers: Record<string, string> } {
  const parameters = new URLSearchParams(fields);
  return { body: parameters.toString(), headers: { "Content-Type": "application/x-www-form-urlencoded" } };
}

export function multipartBody(
  fields: Record<string, string>,
  files: Array<{ field: string; filename: string; content: Buffer | string }> = []
): { body: Buffer; headers: Record<string, string> } {
  const boundary = "----changesagae2e0000000000000001";
  const parts: Buffer[] = [];
  for (const [name, value] of Object.entries(fields)) {
    parts.push(Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="${name}"\r\n\r\n${value}\r\n`));
  }
  for (const file of files) {
    parts.push(Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="${file.field}"; filename="${file.filename}"\r\nContent-Type: application/octet-stream\r\n\r\n`));
    parts.push(Buffer.from(file.content));
    parts.push(Buffer.from("\r\n"));
  }
  parts.push(Buffer.from(`--${boundary}--\r\n`));
  return { body: Buffer.concat(parts), headers: { "Content-Type": `multipart/form-data; boundary=${boundary}` } };
}
