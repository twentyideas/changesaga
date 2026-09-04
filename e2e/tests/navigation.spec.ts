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
  await overview.hover();
  await overview.locator(":scope > .fragment-head").getByRole("button", { name: /Open linked code with \d+ additions? and \d+ deletions?/ }).click();
  const drawer = page.getByRole("complementary", { name: "Linked code" });
  await expect(drawer).toHaveAttribute("aria-hidden", "false");
  await expect(drawer.locator("details.attached-file")).toHaveCount(2);
  await expect(drawer.getByText("src/app.go", { exact: true })).toBeVisible();
  await expect(drawer.getByText("docs/guide.md", { exact: true })).toBeVisible();
  await drawer.locator("details.attached-file").filter({ hasText: "src/app.go" }).locator("summary").click();
  await drawer.getByRole("link", { name: "Open in Code Diff" }).click();

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
  const explanationLink = related.locator("a.related-fragment").first();
  await explanationLink.click();
  const explanationDrawer = page.getByRole("complementary", { name: "Related explanation" });
  await expect(explanationDrawer).toHaveAttribute("aria-hidden", "false");
  await expect(explanationDrawer.locator("article.fragment")).toHaveCount(1);
  await expect(explanationDrawer.getByText("Wave 1 connects the story to the exact source changes.")).toBeVisible();
  await expect(page.getByRole("tab", { name: "Code Diff" })).toHaveAttribute("aria-selected", "true");
  await explanationDrawer.getByRole("button", { name: "Close related explanation" }).click();
  await expect(explanationLink).toBeFocused();
  await expect(page.locator("aside.diff-drawer")).toHaveAttribute("aria-hidden", "true");

  await related.locator("a.related-chapter-link").first().click();
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
  const sourceTarget = page.locator("details.manifest-target").filter({
    has: page.locator(".manifest-target-title > strong").filter({ hasText: /^Overview$/ })
  }).first();
  await sourceTarget.locator(":scope > summary").click();
  // Target files are intentionally fetched only after their explanation is
  // opened, so locate the file after the target-detail response arrives.
  const targetFile = sourceTarget.locator("details.manifest-target-file").filter({ hasText: "src/app.go" });
  await expect(targetFile).toBeVisible();
  await targetFile.locator("summary").click();
  await targetFile.getByRole("link", { name: /Open in Code Diff/ }).click();
  await expect(page.getByRole("tab", { name: "Code Diff" })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator('[data-file-path="src/app.go"].file-diff')).toBeVisible();
  expect(saga.sourceRepo).not.toBe(saga.sagaRepo);
});

test("deeply indented Code Diff paths scroll horizontally without truncation", async ({ page, saga }) => {
  await page.goto(saga.baseURL);
  await page.getByRole("tab", { name: "Code Diff" }).click();

  const tree = page.getByRole("tree", { name: "Changed files" });
  const file = tree.locator("[data-tree-file]").first();
  const name = file.locator(".tree-name");
  const fullName = "a-very-long-component-name-that-must-remain-readable-without-an-ellipsis.ts";
  await file.evaluate((element, value) => {
    element.style.setProperty("--depth", "14");
    const label = element.querySelector(".tree-name");
    if (label) label.textContent = value;
  }, fullName);

  await expect(name).toHaveText(fullName);
  await expect(name).toHaveCSS("text-overflow", "clip");
  await expect.poll(() => tree.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true);
  await tree.evaluate(element => element.scrollTo({ left: element.scrollWidth }));
  await expect.poll(() => tree.evaluate(element => element.scrollLeft)).toBeGreaterThan(0);
});

test("renders Markdown, SVG, raster, and interactive HTML fragments", async ({ page, saga }) => {
  const markdown = page.locator('[data-fragment-title="Overview"] [data-selectable]');
  await expect(markdown).toContainText("Reviewer path");
  await expect(markdown.locator("table")).toContainText("Linked narrative");
  await expect(markdown.locator("strong")).toHaveText("the behavior");
  await expect(markdown.locator("code")).toHaveText("linked code");
  await expect(markdown.locator(":scope > ol").first().locator(":scope > li")).toHaveCount(2);
  const citation = markdown.locator("a.footnote-ref");
  await page.locator('[data-fragment-title="Overview"]').hover();
  await expect(citation).toHaveAttribute("data-open-diffs", /diffs-/);
  await citation.click();
  const citationDrawer = page.getByRole("complementary", { name: "Linked code" });
  await expect(citationDrawer).toHaveAttribute("aria-hidden", "false");
  await expect(citationDrawer.getByText("src/app.go", { exact: true })).toBeVisible();
  await citationDrawer.getByRole("button", { name: "Close linked code" }).click();
  const diagram = page.frameLocator('iframe[title="Architecture Diagram"]');
  await expect(diagram.getByRole("img", { name: "Review flow diagram" })).toBeVisible();
  const diagramFragment = page.locator('[data-fragment-title="Architecture Diagram"]');
  const elementHotspot = diagramFragment.locator('[data-auto-landmark-hotspot="true"][data-element-id="render-boundary"]');
  await expect(elementHotspot).toBeVisible();
  await elementHotspot.hover();
  await elementHotspot.getByRole("button", { name: /Open linked code with \d+ additions? and \d+ deletions?/ }).click();
  const elementDrawer = page.getByRole("complementary", { name: "Linked code" });
  await expect(elementDrawer).toHaveAttribute("aria-hidden", "false");
  await expect(elementDrawer.locator("details.attached-file")).toHaveCount(1);
  const elementFile = elementDrawer.locator("details.attached-file");
  await elementFile.locator("summary").click();
  await expect(elementFile.locator("[data-file-diff-status]")).toHaveText("All changed hunks · linked lines highlighted");
  await expect(elementFile.locator(".diff-row.new")).toHaveCount(3);
  await expect(elementFile.locator(".diff-row.linked-evidence")).toHaveCount(1);
  await expect(elementFile).toContainText(".review {");
  await expect(elementFile).not.toContainText("Linked ranges only");
  await elementDrawer.getByRole("button", { name: "Close linked code" }).click();
  const edgeHotspot = diagramFragment.locator('[data-auto-landmark-hotspot="true"][data-element-id="evidence-handoff"]');
  await expect(edgeHotspot).toBeVisible();
  await edgeHotspot.hover();
  await edgeHotspot.getByRole("button", { name: /Open linked code with \d+ additions? and \d+ deletions?/ }).click();
  await expect(elementDrawer).toHaveAttribute("aria-hidden", "false");
  await expect(elementDrawer.locator("details.attached-file")).toHaveCount(1);
  await elementDrawer.getByRole("button", { name: "Close linked code" }).click();
  await expect(page.locator('[data-fragment-title="Raster Preview"] img[alt="Raster Preview"]')).toBeVisible();

  const demo = page.frameLocator('iframe[title="Interactive Demo"]');
  await expect(demo.getByRole("button", { name: "Run demo" })).toBeVisible();
  await demo.getByRole("button", { name: "Run demo" }).click();
  await expect(demo.locator("output")).toHaveText("interactive ready");
  expect(saga.baseURL).toMatch(/^http:\/\/127\.0\.0\.1:/);
});
