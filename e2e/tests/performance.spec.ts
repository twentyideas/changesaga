import { expect, test } from "../support/test.js";
import { largeSagaChangedLines, largeSagaScale } from "../support/fixture-builder.js";

/**
 * Budgets for a large saga in a real browser. The page describes the whole
 * comparison and carries only the file the reviewer is already looking at;
 * every other diff body arrives from /api/file-diff when that file is opened.
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
 *   document bytes   6,851,463 -> 555,974
 *   DOM elements        73,835 ->   6,315
 *   diff rows in DOM     6,240 ->      96
 */
const documentByteBudget = 1_200_000;
const domElementBudget = 20_000;
/** The Code Diff tab inlines one file; nothing else may bring rows with it. */
const domDiffRowBudget = 2 * largeSagaScale.changedLinesPerFile + 64;
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
  expect(
    measured.diffRows,
    budgetMessage("diff rows in the first-load DOM", measured.diffRows, domDiffRowBudget, `Only the file the Code Diff tab selected may be inlined, and it changes ${largeSagaScale.changedLinesPerFile} lines.`)
  ).toBeLessThanOrEqual(domDiffRowBudget);
  // The payload must have shrunk by deferring the audit, not by dropping it.
  expect(measured.diffRows).toBeGreaterThan(0);
  // Coverage lists each changed file in both directions, so every file offers
  // its body from the code-first tree and again from its explanation.
  expect(measured.changedFiles, "coverage must still list every changed file").toBe(largeSagaScale.sourceFiles);
  expect(measured.lazyCoverageFiles, "every changed file must still offer its diff on demand").toBeGreaterThanOrEqual(largeSagaScale.sourceFiles);

  expect(
    measured.domInteractive,
    budgetMessage("time to interactive", measured.domInteractive, interactiveCeilingMs, "This is a smoke ceiling; see the attached metrics for the real number.")
  ).toBeLessThanOrEqual(interactiveCeilingMs);
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
  // Each fixture module rewrites every line, so the body is the whole file.
  await expect(rows).toHaveCount(2 * largeSagaScale.changedLinesPerFile);
  await expect(file.locator("[data-diff-placeholder]")).toHaveCount(0);
  expect(requested).toHaveLength(1);
  expect(requested[0]).toContain("view=manifest");

  // Reopening the same file must not ask the server again.
  await file.locator("summary").click();
  await file.locator("summary").click();
  await expect(rows).toHaveCount(2 * largeSagaScale.changedLinesPerFile);
  expect(requested).toHaveLength(1);
});

test("loads a linked-code file diff on demand and keeps it answerable to its explanation", async ({ page, largeSaga }) => {
  await page.goto(largeSaga.baseURL, { waitUntil: "load" });
  await page.locator("[data-open-diffs]:visible").first().click();
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
