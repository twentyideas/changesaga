import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, relative, resolve } from "node:path";
import type { TestInfo } from "@playwright/test";
import { binaryPath } from "./global-setup.js";

const reviewer = { name: "E2E Reviewer", email: "reviewer@example.test" };
const author = { name: "Source Author", email: "author@example.test" };

type StatusAtom = { uri: string; path: string; side?: string; line?: number };
type StatusReport = { uncovered: StatusAtom[] | null };

export type SagaFixture = {
  root: string;
  sourceRepo: string;
  sagaRepo: string;
  sagaRoot: string;
  baseURL: string;
  server: ChildProcessWithoutNullStreams;
  serverLog: string[];
};

function write(path: string, contents: string | Buffer): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, contents);
}

function command(file: string, args: string[], cwd: string, env?: NodeJS.ProcessEnv): string {
  const result = spawnSync(file, args, {
    cwd,
    env: { ...process.env, ...env },
    encoding: "utf8"
  });
  if (result.status !== 0) {
    throw new Error(`${file} ${args.join(" ")} failed (${result.status})\n${result.stdout}\n${result.stderr}`);
  }
  return result.stdout.trim();
}

export function git(cwd: string, ...args: string[]): string {
  return command("git", args, cwd);
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

function addCoverage(sourceRepo: string, sagaRoot: string): void {
  const uncovered = statusReport(sourceRepo, sagaRoot).uncovered ?? [];
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
}

function buildSourceRepository(root: string): { sourceRepo: string; base: string; head: string } {
  const sourceRepo = join(root, "source-repo");
  mkdirSync(sourceRepo, { recursive: true });
  git(sourceRepo, "init", "-b", "main");
  configureGit(sourceRepo, author);
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

function buildSagaRepository(root: string, source: { sourceRepo: string; base: string; head: string }): { sagaRepo: string; sagaRoot: string } {
  const sagaRepo = join(root, "saga-repo");
  const sagaRoot = join(sagaRepo, "wave-one.saga");
  mkdirSync(sagaRepo, { recursive: true });
  git(sagaRepo, "init", "-b", "main");
  configureGit(sagaRepo, reviewer);

  runSaga([
    "init", "--repo", source.sourceRepo, "--repository", "https://example.test/acme/change-saga-demo.git",
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

  addCoverage(source.sourceRepo, sagaRoot);
  git(sagaRepo, "add", ".");
  git(sagaRepo, "commit", "-m", "add Wave 1 saga fixture");
  return { sagaRepo, sagaRoot };
}

async function startServer(sourceRepo: string, sagaRoot: string): Promise<{ server: ChildProcessWithoutNullStreams; baseURL: string; serverLog: string[] }> {
  const server = spawn(binaryPath, ["serve", "--addr", "127.0.0.1:0", "--repo", sourceRepo, sagaRoot], {
    cwd: dirname(sagaRoot),
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

export async function createSagaFixture(testInfo: TestInfo): Promise<SagaFixture> {
  const safeTitle = testInfo.title.replace(/[^a-z0-9]+/gi, "-").replace(/^-|-$/g, "").slice(0, 48).toLowerCase() || "test";
  const root = mkdtempSync(join(tmpdir(), `change-saga-e2e-${testInfo.workerIndex}-${safeTitle}-`));
  try {
    const source = buildSourceRepository(root);
    const saga = buildSagaRepository(root, source);
    const running = await startServer(source.sourceRepo, saga.sagaRoot);
    return { root, sourceRepo: source.sourceRepo, ...saga, ...running };
  } catch (error) {
    rmSync(root, { recursive: true, force: true });
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

export async function attachFailureState(fixture: SagaFixture, testInfo: TestInfo, browserEvents: string[]): Promise<void> {
  if (testInfo.status === testInfo.expectedStatus) return;
  const state = {
    paths: { root: "<fixture-root>", sourceRepo: "<fixture-root>/source-repo", sagaRepo: "<fixture-root>/saga-repo" },
    sourceGit: git(fixture.sourceRepo, "status", "--short"),
    sagaGit: git(fixture.sagaRepo, "status", "--short"),
    sagaLog: git(fixture.sagaRepo, "log", "--format=%h %an <%ae> %s", "-5"),
    files: snapshotTree(fixture.root)
  };
  const artifacts = [
    { name: "server.log", body: fixture.serverLog.join(""), contentType: "text/plain" },
    { name: "browser-events.json", body: `${JSON.stringify(browserEvents, null, 2)}\n`, contentType: "application/json" },
    { name: "sanitized-fixture-state.json", body: `${JSON.stringify(state, null, 2)}\n`, contentType: "application/json" }
  ];
  for (const artifact of artifacts) {
    const path = testInfo.outputPath(artifact.name);
    writeFileSync(path, artifact.body);
    await testInfo.attach(artifact.name, { path, contentType: artifact.contentType });
  }
}

export async function stopSagaFixture(fixture: SagaFixture): Promise<void> {
  if (fixture.server.exitCode === null) {
    fixture.server.kill("SIGINT");
    await Promise.race([
      new Promise<void>((resolveExit) => fixture.server.once("exit", () => resolveExit())),
      new Promise<void>((resolveTimeout) => setTimeout(resolveTimeout, 5_000))
    ]);
    if (fixture.server.exitCode === null) fixture.server.kill("SIGKILL");
  }
  rmSync(fixture.root, { recursive: true, force: true });
}

export function reviewFiles(fixture: SagaFixture, pattern: RegExp): string[] {
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

export function readJSON<T = Record<string, unknown>>(path: string): T {
  return JSON.parse(readFileSync(path, "utf8")) as T;
}

export function relativeToSaga(fixture: SagaFixture, path: string): string {
  return relative(fixture.sagaRoot, resolve(path)).split("\\").join("/");
}

export function fileName(path: string): string {
  return basename(path);
}
