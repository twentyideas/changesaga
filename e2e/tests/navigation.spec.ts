import { expectNoSeriousAccessibilityViolations, expect, test } from "../support/test.js";

test("@critical navigates the saga, linked code, code tree, and coverage in both directions", async ({ page, saga }) => {
  await expect(page).toHaveTitle("Wave One Review · Change Saga");
  await expect(page.getByRole("heading", { name: "Wave One Review" })).toBeVisible();
  await expect(page.getByText("Wave 1 connects the story to the exact source changes.")).toBeVisible();
  // The whole page, chrome included: the workspace tablist and the closed
  // linked-code drawer are now correct, so nothing is scoped out of this scan.
  await expectNoSeriousAccessibilityViolations(page);

  const contents = page.getByRole("navigation", { name: "Contents" });
  await contents.getByRole("link", { name: "Architecture", exact: true }).click();
  await expect(page).toHaveURL(/#.+chapter-architecture-/);
  await expect(page.getByRole("button", { name: "Close Architecture" })).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("tabpanel", { name: "Saga" }).getByText("The renderer and persistence boundary stay independent.")).toBeVisible();
  await page.goto(`${saga.baseURL}/chapters/architecture`);
  await expect(page).toHaveURL(/\/#.+chapter-architecture-/);
  await expect(page.getByRole("button", { name: "Close Architecture" })).toHaveAttribute("aria-expanded", "true");

  const overview = page.locator('[data-fragment-title="Overview"]');
  await overview.scrollIntoViewIfNeeded();
  await overview.getByRole("button", { name: /Open the \d+ linked changes/ }).click();
  const drawer = page.getByRole("complementary", { name: "Linked code" });
  await expect(drawer).toHaveAttribute("aria-hidden", "false");
  await expect(drawer.locator("details.attached-file")).toHaveCount(2);
  await expect(drawer.getByText("src/app.go", { exact: true })).toBeVisible();
  await expect(drawer.getByText("docs/guide.md", { exact: true })).toBeVisible();
  await drawer.locator("details.attached-file").filter({ hasText: "src/app.go" }).locator("summary").click();
  await drawer.getByRole("link", { name: "Open full file in Code Diff" }).click();

  await expect(page.getByRole("tab", { name: "Code Diff" })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator('[data-file-path="src/app.go"].file-diff')).toBeVisible();
  const changedFiles = page.getByRole("tree", { name: "Changed files" });
  const docsFile = changedFiles.locator('[role="treeitem"][data-tree-path="docs/guide.md"]');
  const docsFolder = changedFiles.locator('details[data-tree-folder]:has([data-tree-path="docs/guide.md"])');
  if ((await docsFolder.getAttribute("open")) === null) await docsFolder.locator(":scope > summary").click();
  await expect(docsFolder).toHaveAttribute("open", "");
  await docsFolder.locator(":scope > summary").click();
  await expect(docsFolder).not.toHaveAttribute("open", "");
  await docsFolder.locator(":scope > summary").click();
  await expect(docsFolder).toHaveAttribute("open", "");
  await docsFile.click();
  await expect(page.locator('[data-file-path="docs/guide.md"].file-diff')).toBeVisible();
  const related = page.getByRole("complementary", { name: "Explanations for this file" });
  await related.getByRole("link", { name: "Overview", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Saga" })).toHaveAttribute("aria-selected", "true");
  await expect(overview).toBeVisible();

  await page.getByRole("tab", { name: "Coverage" }).click();
  await expect(page.getByRole("button", { name: "Code → Saga" })).toHaveAttribute("aria-pressed", "true");
  const appCoverage = page.locator('details.manifest-file[data-manifest-search="src/app.go"]');
  await appCoverage.locator("summary").click();
  await appCoverage.getByRole("link", { name: /^Overview/ }).first().click();
  await expect(page.getByRole("tab", { name: "Saga" })).toHaveAttribute("aria-selected", "true");
  await expect(overview).toBeVisible();

  await page.getByRole("tab", { name: "Coverage" }).click();
  await page.getByRole("button", { name: "Saga → Code" }).click();
  const overviewTarget = page.locator("details.manifest-target").filter({ hasText: "Overview" }).first();
  await overviewTarget.locator(":scope > summary").click();
  const targetFile = overviewTarget.locator("details.manifest-target-file").filter({ hasText: "src/app.go" });
  await targetFile.locator("summary").click();
  await targetFile.getByRole("link", { name: /Open in Code Diff/ }).click();
  await expect(page.getByRole("tab", { name: "Code Diff" })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator('[data-file-path="src/app.go"].file-diff')).toBeVisible();
  expect(saga.sourceRepo).not.toBe(saga.sagaRepo);
});

test("renders Markdown, SVG, raster, and interactive HTML fragments", async ({ page, saga }) => {
  const markdown = page.locator('[data-fragment-title="Overview"] [data-selectable]');
  await expect(markdown).toContainText("Reviewer path");
  await expect(markdown.locator("table")).toContainText("Linked narrative");
  await expect(markdown.locator("strong")).toHaveText("the behavior");
  await expect(markdown.locator("code")).toHaveText("linked code");
  await expect(markdown.locator("ol > li")).toHaveCount(2);
  const diagram = page.frameLocator('iframe[title="Architecture Diagram"]');
  await expect(diagram.getByRole("img", { name: "Review flow diagram" })).toBeVisible();
  await expect(page.locator('[data-fragment-title="Raster Preview"] img[alt="Raster Preview"]')).toBeVisible();

  const demo = page.frameLocator('iframe[title="Interactive Demo"]');
  await expect(demo.getByRole("button", { name: "Run demo" })).toBeVisible();
  await demo.getByRole("button", { name: "Run demo" }).click();
  await expect(demo.locator("output")).toHaveText("interactive ready");
  expect(saga.baseURL).toMatch(/^http:\/\/127\.0\.0\.1:/);
});
