import { expect, test } from "../support/test.js";
import { largeSagaChangedLines, largeSagaScale } from "../support/fixture-builder.js";

/**
 * Budgets for a large saga in a real browser. The page describes the whole
 * comparison and the whole story, and carries neither: it is only the saga
 * identity, overview descriptors, chapter summaries, and navigation shell.
 * Code, coverage, diff bodies, chapter bodies, and explanations all arrive
 * through bounded endpoints when the reviewer reaches them.
 *
 * Byte and element counts are hard budgets: the fixture is fixed, so they are
 * deterministic and a breach always means the payload changed shape. Times are
 * diagnostic — they are attached to the test result and recorded in
 * docs/performance.md, and only an order-of-magnitude ceiling is asserted,
 * because a shared CI runner varies far more than any regression worth catching.
 *
 * These are deliberately not @critical: that tag marks the mutation-heavy flows
 * the suite repeats to hunt flakes, and a budget over a fixed fixture is already
 * deterministic. Repeating it would only cost CI time.
 *
 * Measured on this fixture (1,536 changed lines across 32 files):
 *
 * The current shell contains no comparison rows or coverage file models at
 * all. Separate endpoint tests below prove those surfaces still exist.
 */
const documentByteBudget = 1_200_000;
const domElementBudget = 6_000;
/** A generous smoke ceiling, not a performance target. */
const interactiveCeilingMs = 10_000;

function budgetMessage(name: string, measured: number, budget: number, why: string): string {
  return `${name} exceeded its budget: ${measured} > ${budget} (${(measured / budget).toFixed(1)}x). ${why} See docs/performance.md before raising this budget.`;
}

test("a large saga's first load stays within its payload budgets", async ({ page, largeSaga }, testInfo) => {
  const started = Date.now();
  const response = await page.goto(largeSaga.baseURL, { waitUntil: "load" });
  const loadMs = Date.now() - started;
  expect(response?.status()).toBe(200);
  const documentBytes = (await response!.body()).length;

  const measured = await page.evaluate(() => {
    const navigation = performance.getEntriesByType("navigation")[0] as PerformanceNavigationTiming;
    const paint = performance.getEntriesByName("first-contentful-paint")[0];
    return {
      elements: document.getElementsByTagName("*").length,
      diffRows: document.querySelectorAll(".diff-row").length,
      lazyCoverageFiles: document.querySelectorAll("[data-manifest-diff-href]").length,
      changedFiles: document.querySelectorAll("details.manifest-file").length,
      domInteractive: Math.round(navigation.domInteractive),
      firstContentfulPaint: paint ? Math.round(paint.startTime) : null
    };
  });
  await testInfo.attach("first-load-metrics.json", {
    body: `${JSON.stringify({ changedLines: largeSagaChangedLines, documentBytes, loadMs, ...measured }, null, 2)}\n`,
    contentType: "application/json"
  });

  expect(
    documentBytes,
    budgetMessage("first-load document bytes", documentBytes, documentByteBudget, "The page must describe the comparison, not contain it.")
  ).toBeLessThanOrEqual(documentByteBudget);
  expect(
    measured.elements,
    budgetMessage("first-load DOM elements", measured.elements, domElementBudget, "Every element here is parsed, laid out, and retained by the browser.")
  ).toBeLessThanOrEqual(domElementBudget);
  expect(measured.diffRows, "the root shell must not contain source comparison rows").toBe(0);
  expect(measured.changedFiles, "the root shell must not contain the coverage file model").toBe(0);
  expect(measured.lazyCoverageFiles, "the root shell must not contain deferred coverage-file descriptors").toBe(0);
  await expect(page.getByRole("tab", { name: "Code Diff" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Coverage" })).toBeVisible();

  expect(
    measured.domInteractive,
    budgetMessage("time to interactive", measured.domInteractive, interactiveCeilingMs, "This is a smoke ceiling; see the attached metrics for the real number.")
  ).toBeLessThanOrEqual(interactiveCeilingMs);
});

test("ships the saga as a shell and fetches each chapter and explanation once, when it is reached", async ({ page, largeSaga }, testInfo) => {
  const sectionRequests: string[] = [];
  const fragmentRequests: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/api/section") sectionRequests.push(url.searchParams.get("target") ?? "");
    if (url.pathname === "/api/fragment") fragmentRequests.push(url.searchParams.get("target") ?? "");
  });
  await page.goto(largeSaga.baseURL, { waitUntil: "load" });

  const sagaView = page.locator('[data-view="saga"]');
  await expect(sagaView.locator("section.chapter")).toHaveCount(largeSagaScale.chapters);
  await expect(sagaView.locator("[data-section-href]")).toHaveCount(largeSagaScale.chapters);
  // The overview owns its own explanation; every other one is inside a chapter
  // nobody has opened, so it is named by a summary and nothing more.
  const shell = await page.evaluate(() => ({
    elements: window.document.querySelector('[data-view="saga"]')!.getElementsByTagName("*").length,
    explanations: window.document.querySelectorAll('[data-view="saga"] article.fragment').length
  }));
  await testInfo.attach("shell-metrics.json", {
    body: `${JSON.stringify({ chapters: largeSagaScale.chapters, ...shell }, null, 2)}\n`,
    contentType: "application/json"
  });
  expect(shell.explanations, "only the overview's own explanations are on the page").toBeLessThan(largeSagaScale.chapters);
  expect(sectionRequests, "no chapter is fetched before it is opened").toEqual([]);

  // The overview's own explanation is fetched because it is on screen, and it
  // is the only one: the rest are inside chapters that are still closed.
  await expect(page.locator(".fragment-markdown").first()).toBeVisible();
  expect(fragmentRequests.length, "only the explanations on screen are fetched").toBeLessThanOrEqual(2);

  const chapter = sagaView.locator("section.chapter").first();
  await chapter.getByRole("button", { name: /^Open / }).click();
  const chapterExplanations = chapter.locator("[data-chapter-body] article.fragment");
  await expect(chapterExplanations.first()).toBeAttached();
  expect(await chapterExplanations.count(), "an opened chapter names every explanation it holds")
    .toBeGreaterThanOrEqual(largeSagaScale.fragmentsPerChapter);
  expect(sectionRequests, "opening one chapter fetches exactly that chapter").toHaveLength(1);
  await expect(chapter.locator(".fragment-markdown").first()).toBeVisible();

  // Closing and reopening the same chapter asks the server nothing again.
  await chapter.getByRole("button", { name: /^Close / }).click();
  await chapter.getByRole("button", { name: /^Open / }).click();
  expect(sectionRequests, "reopening a chapter must not fetch it again").toHaveLength(1);

  // A deep link into a chapter nobody has opened still resolves: the anchor is
  // located, its chapter is fetched, and the page scrolls to it.
  const deepLink = await chapterExplanations.last().getAttribute("id");
  await page.goto(`${largeSaga.baseURL}/#${deepLink}`, { waitUntil: "load" });
  const destination = page.locator(`[id="${deepLink}"]`);
  await expect(destination).toBeVisible();
  await expect(destination.locator(".fragment-markdown")).toBeVisible();
});

test("loads a coverage file diff only once the reviewer opens that file", async ({ page, largeSaga }) => {
  const requested: string[] = [];
  page.on("request", (request) => {
    if (request.url().includes("/api/file-diff")) requested.push(new URL(request.url()).search);
  });
  await page.goto(largeSaga.baseURL, { waitUntil: "load" });
  await page.click("#view-tab-manifest");
  expect(requested, "switching to Coverage must not fetch any diff body").toEqual([]);

  const file = page.locator("details.manifest-file").first();
  await expect(file.locator("[data-manifest-diff-rows] .diff-row")).toHaveCount(0);
  await file.locator("summary").click();

  const rows = file.locator("[data-manifest-diff-rows] .diff-row");
  await rows.first().waitFor();
  await expect(rows).toHaveCount(50);
  await file.getByRole("button", { name: "Load the next file chunk" }).click();
  await expect(rows).toHaveCount(2 * largeSagaScale.changedLinesPerFile);
  await expect(file.locator("[data-diff-placeholder]")).toHaveCount(0);
  expect(requested).toHaveLength(2);
  expect(requested[0]).toContain("view=manifest");

  // Reopening the same file must not ask the server again.
  await file.locator("summary").click();
  await file.locator("summary").click();
  await expect(rows).toHaveCount(2 * largeSagaScale.changedLinesPerFile);
  expect(requested).toHaveLength(2);
});

test("loads a linked-code file diff on demand and keeps it answerable to its explanation", async ({ page, largeSaga }) => {
  await page.goto(largeSaga.baseURL, { waitUntil: "load" });
  const overview = page.locator('[data-view="saga"] article.fragment').first();
  await expect(overview.locator(".fragment-markdown")).toBeVisible();
  await overview.hover();
  const opener = overview.locator("[data-open-diffs]:visible").first();
  await expect(opener).toHaveAttribute("aria-label", /Open the \d+ linked changes/);
  await opener.click();
  const drawer = page.locator(".diff-drawer.open");
  await drawer.waitFor();

  const attached = drawer.locator("details.attached-file").first();
  const target = await attached.getAttribute("data-attached-target");
  expect(target).toBeTruthy();
  await expect(attached.locator(".diff-row")).toHaveCount(0);
  await attached.locator("summary").click();

  const rows = attached.locator("[data-linked-diff-rows] .diff-row");
  await rows.first().waitFor();
  await expect(attached.locator("[data-full-diff-status]")).toHaveText(/Full file diff/);
  // The server marks the rows this explanation owns, so the drawer still shows
  // its own evidence inside the surrounding file it fetched for context.
  const linked = attached.locator(".diff-row.linked-evidence");
  await expect(linked).toHaveCount(50);
  await attached.getByRole("button", { name: "Load the next file chunk" }).click();
  await expect(linked).toHaveCount(2 * largeSagaScale.changedLinesPerFile);

  // A comment written here must carry this file's exact line identity and the
  // narrative target whose drawer it was written from, both of which now come
  // from the row rather than from attributes repeated on every button.
  const row = rows.filter({ has: page.locator("[data-diff-action]") }).first();
  const rowReference = await row.getAttribute("data-diff-ref");
  expect(rowReference).toMatch(/^saga-diff:\/\/v1\/line\?/);
  await row.getByRole("button", { name: "Comment on this line" }).click();
  const composer = page.locator("form.diff-compose");
  await expect(composer).toHaveClass(/open/);
  expect(await composer.locator('[name="target"]').inputValue()).toBe(target);
  expect(JSON.parse(await composer.locator('[name="anchor"]').inputValue())).toEqual({ type: "diff", diff: { uri: rowReference } });

  // The suggestion composer prefills from the row's rendered code.
  await composer.locator("[data-close-diff-compose]").click();
  await row.getByRole("button", { name: "Suggest a replacement for this line" }).click();
  expect(await composer.locator('[name="replacement"]').inputValue()).toBe(await row.locator("[data-code]").textContent());
});
