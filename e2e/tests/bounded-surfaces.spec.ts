import { expect, test } from "../support/test.js";
import { canonicalLineURI } from "../support/fixture-builder.js";

test("bounded Code Diff retries a building snapshot, preserves its deep link, and streams every hunk", async ({ page, largeSaga }) => {
  const file = "src/component-000.ts";
  const requests: URL[] = [];
  const fileRequests: URL[] = [];
  await page.route("**/api/file-diff**", async (route) => {
    const url = new URL(route.request().url());
    fileRequests.push(url);
    const next = url.searchParams.has("cursor") ? "" : "opaque-lines-2";
    await route.fulfill({
      contentType: "text/html",
      headers: next ? { "X-Change-Saga-Next-Cursor": next } : {},
      body: `<div data-file-diff-page><div data-page-items="lines"><div class="diff-row new" data-diff-row${next ? ' id="thread-deep-link"' : ""}><code data-code>${next ? "first" : "second"} page</code></div></div></div>`
    });
  });
  await page.route("**/api/code**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    if (requests.length === 1) {
      await route.fulfill({ status: 202, headers: { "Retry-After": "10" }, body: "building" });
      return;
    }
    await route.fulfill({
      contentType: "text/html",
      body: `<div data-review-surface-response="code">
        <div data-code-sidebar-content><div class="file-tree" role="tree" aria-label="Changed files"><a role="treeitem" data-tree-file data-tree-path="${file}" href="/?view=code&amp;file=${encodeURIComponent(file)}">component-000.ts</a></div></div>
        <div data-code-panel-content><span data-code-meta-content hidden>0/2 reviewed</span><div class="code-toolbar"><button type="button" data-layout="inline">Unified</button></div><article class="file-diff" data-file-path="${file}" data-file-diff-href="/api/file-diff?file=${encodeURIComponent(file)}&amp;diff=${encodeURIComponent(diff)}"><div class="diff-surface" data-diff-surface><span data-file-diff-status>Loading every changed hunk…</span><div data-file-diff-rows data-diff-body data-page-items="lines"><p data-diff-placeholder>Loading</p></div></div></article></div>
      </div>`
    });
  });

  const diff = canonicalLineURI(largeSaga.identity, file, "new", 4, 4);
  const href = `${largeSaga.baseURL}/?view=code&file=${encodeURIComponent(file)}&diff=${encodeURIComponent(diff)}#thread-deep-link`;
  await page.goto(href, { waitUntil: "load" });
  await expect(page.locator('[data-review-surface="code"] [data-surface-status]')).toContainText("Building the source comparison");
  await expect(page).toHaveURL(href);

  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.locator(`[data-file-path="${file}"]`)).toBeVisible();
  expect(requests[1].searchParams.get("file")).toBe(file);
  expect(requests[1].searchParams.get("diff")).toContain("saga-diff://v1/line");
  await expect(page).toHaveURL(href);

  const surface = page.locator('[data-review-surface="code"]');
  await expect(surface.locator(".diff-row")).toHaveCount(2);
  await expect(surface.locator("[data-file-diff-status]")).toHaveText("All changed hunks");
  expect(fileRequests[0].searchParams.get("file")).toBe(file);
  expect(fileRequests[0].searchParams.get("diff")).toBe(diff);
  await expect(surface.getByRole("button", { name: "Load the next file chunk" })).toHaveCount(0);
  await expect(surface.getByRole("button", { name: "Unified" })).toHaveCount(1);
  expect(fileRequests[1].searchParams.get("cursor")).toBe("opaque-lines-2");
});

test("linked code streams every changed hunk and expands collapsed context from either edge", async ({ page, saga }) => {
  const fileRequests: URL[] = [];
  const context = (start: number, end: number): string => Array.from({ length: end - start + 1 }, (_, offset) => {
    const line = start + offset;
    return `<div class="diff-row context" data-context-row><span class="line-no">${line}</span><span class="line-no">${line}</span><span></span><code data-code>line ${line}</code><span></span></div>`;
  }).join("");
  const changed = (line: number): string => `<div class="diff-row new" data-diff-row data-line="${line}"><span></span><button type="button" data-line-select>${line}</button><span>+</span><code data-code>changed ${line}</code><span></span></div>`;

  await page.route("**/api/file-diff**", async (route) => {
    const url = new URL(route.request().url());
    fileRequests.push(url);
    const second = url.searchParams.has("cursor");
    await route.fulfill({
      contentType: "text/html",
      headers: second ? {} : { "X-Change-Saga-Next-Cursor": "linked-hunks-2" },
      body: `<div data-file-diff-page><div data-page-items="lines">${
        second ? context(30, 42) + changed(43) + context(44, 58) : context(1, 15) + changed(16) + context(17, 29)
      }</div></div>`
    });
  });

  await page.goto(saga.baseURL, { waitUntil: "load" });
  const overview = page.locator('[data-view="saga"] article.fragment').filter({ has: page.locator(".fragment-markdown") }).first();
  await expect(overview).toBeVisible();
  await overview.hover();
  await overview.locator("[data-open-diffs]:visible").first().click();
  const drawer = page.locator(".diff-drawer.open");
  const attached = drawer.locator("details.attached-file").filter({ hasText: "src/app.go" });
  await attached.locator("summary").click();

  await expect(attached.locator("[data-file-diff-status]")).toHaveText(/All changed hunks/);
  expect(fileRequests).toHaveLength(2);
  expect(fileRequests[1].searchParams.get("cursor")).toBe("linked-hunks-2");
  await expect(attached.locator(".diff-row.new:visible")).toHaveCount(2);
  await expect(attached.locator("[data-context-row]:visible")).toHaveCount(12);
  await expect(attached.locator(".context-expander")).toHaveCount(3);
  await expect(attached.getByRole("button", { name: "Load the next file chunk" })).toHaveCount(0);

  const leadingGap = attached.locator(".context-expander").first();
  await expect(leadingGap.getByRole("button", { name: "Show previous 10 unchanged lines" })).toBeVisible();
  await expect(leadingGap.getByRole("button", { name: /Show next/ })).toHaveCount(0);
  const trailingGap = attached.locator(".context-expander").last();
  await expect(trailingGap.getByRole("button", { name: "Show next 10 unchanged lines" })).toBeVisible();
  await expect(trailingGap.getByRole("button", { name: /Show previous/ })).toHaveCount(0);

  const middleGap = attached.locator(".context-expander").nth(1);
  const expandAll = middleGap.getByRole("button", { name: "Expand all 20 unchanged lines", exact: true });
  await expect(expandAll).not.toHaveAttribute("aria-label");
  const next = middleGap.getByRole("button", { name: "Show next 10 unchanged lines" });
  await next.focus();
  await next.press("Enter");
  await expect(next).toBeFocused();
  await expect(middleGap.getByRole("button", { name: "Expand all 10 unchanged lines", exact: true })).toBeVisible();
  const previous = middleGap.getByRole("button", { name: "Show previous 10 unchanged lines" });
  await previous.focus();
  await previous.press("Enter");
  await expect(attached.locator(".context-expander")).toHaveCount(2);
  await expect(previous).not.toBeAttached();
  await expect(attached.locator('[data-line="43"] [data-line-select]')).toBeFocused();
  await expect(attached.locator(".diff-row.new:visible")).toHaveCount(2);
});

test("collapsing a linked file cancels its remaining diff pages", async ({ page, saga }) => {
  const fileRequests: URL[] = [];
  await page.route("**/api/file-diff**", async (route) => {
    const url = new URL(route.request().url());
    fileRequests.push(url);
    const cursor = url.searchParams.get("cursor");
    const pageNumber = cursor ? Number(cursor.split("-").at(-1)) : 1;
    if (pageNumber > 1) await new Promise((resolve) => setTimeout(resolve, 250));
    await route.fulfill({
      contentType: "text/html",
      headers: {
        "X-Change-Saga-Next-Cursor": `page-${pageNumber + 1}`,
        "X-Change-Saga-Total": "100",
        "X-Change-Saga-Returned": "1"
      },
      body: `<div data-file-diff-page><div data-page-items="lines"><div class="diff-row new" data-diff-row data-line="${pageNumber}"><code data-code>page ${pageNumber}</code></div></div></div>`
    });
  });

  await page.goto(saga.baseURL, { waitUntil: "load" });
  const overview = page.locator('[data-view="saga"] article.fragment').filter({ has: page.locator(".fragment-markdown") }).first();
  await overview.hover();
  await overview.locator("[data-open-diffs]:visible").first().click();
  const attached = page.locator(".diff-drawer.open details.attached-file").first();
  await attached.locator("summary").click();
  await expect(attached.locator("[data-file-diff-status]")).toHaveText("Loaded 1 of 100 diff lines…");
  await attached.locator("summary").click();
  await page.waitForTimeout(800);
  expect(fileRequests.length).toBeLessThanOrEqual(2);
});

test("a repeated file-diff cursor stops instead of spinning from cache", async ({ page, saga }) => {
  const fileRequests: URL[] = [];
  await page.route("**/api/file-diff**", async (route) => {
    fileRequests.push(new URL(route.request().url()));
    await route.fulfill({
      contentType: "text/html",
      headers: { "X-Change-Saga-Next-Cursor": "repeat" },
      body: `<div data-file-diff-page><div data-page-items="lines"><div class="diff-row new" data-diff-row><code data-code>one bounded page</code></div></div></div>`
    });
  });

  await page.goto(saga.baseURL, { waitUntil: "load" });
  const overview = page.locator('[data-view="saga"] article.fragment').filter({ has: page.locator(".fragment-markdown") }).first();
  await overview.hover();
  await overview.locator("[data-open-diffs]:visible").first().click();
  const attached = page.locator(".diff-drawer.open details.attached-file").first();
  await attached.locator("summary").click();
  await expect(attached.locator("[data-file-diff-status]")).toHaveText("Could not load every changed hunk");
  expect(fileRequests).toHaveLength(2);
  await expect(attached.locator(".diff-row")).toHaveCount(0);
});

test("linked code uses the shared side-by-side layout selected in Code Diff", async ({ page, saga }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${saga.baseURL}/?view=code`, { waitUntil: "load" });
  const split = page.getByRole("button", { name: "Side-by-side diff" });
  await split.click();
  await expect(split).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("tab", { name: "Saga" }).click();
  const overview = page.locator('[data-view="saga"] article.fragment').filter({ has: page.locator(".fragment-markdown") }).first();
  await overview.hover();
  await overview.locator("[data-open-diffs]:visible").first().click();
  const attached = page.locator(".diff-drawer.open details.attached-file").first();
  await attached.locator("summary").click();
  await expect(attached.locator("[data-file-diff-status]")).toHaveText("All changed hunks");
  await expect(attached.locator("[data-diff-surface]")).toHaveAttribute("data-layout", "split");
});

test("Coverage continuously loads one direction at a time and keeps its controls while appending", async ({ page, largeSaga }) => {
  const requests: URL[] = [];
  await page.route("**/api/coverage**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    const mode = url.searchParams.get("mode") === "saga" ? "saga" : "code";
    const cursor = url.searchParams.get("cursor");
    if (cursor) {
      await route.fulfill({
        contentType: "text/html",
        body: `<div data-review-surface-response="manifest" data-page-key="coverage-code"><div data-page-items="coverage-code"><div data-manifest-search="second.ts">second page</div></div></div>`
      });
      return;
    }
    await route.fulfill({
      contentType: "text/html",
      headers: mode === "code" ? { "X-Change-Saga-Next-Cursor": "opaque-coverage-2" } : {},
      body: `<div data-review-surface-response="manifest" data-page-key="coverage-${mode}"><div class="manifest-wrap">
        <div class="manifest-tools"><div class="manifest-modes"><button type="button" data-manifest-mode="code">Code → Saga</button><button type="button" data-manifest-mode="saga">Saga → Code</button></div></div>
        <section data-manifest-panel="${mode}"><div data-page-items="coverage-${mode}"><div data-manifest-search="first.ts">${mode} first page</div></div></section>
      </div></div>`
    });
  });

  await page.goto(largeSaga.baseURL, { waitUntil: "load" });
  expect(requests).toEqual([]);
  await page.getByRole("tab", { name: "Coverage" }).click();
  const coverage = page.locator('[data-review-surface="manifest"]');
  await expect(coverage.getByText("code first page")).toBeVisible();
  // Coverage summary pages continue automatically. The reviewer only controls
  // which direction is active and which individual item is expanded.
  await expect(coverage.getByText("second page")).toBeVisible();
  expect(requests).toHaveLength(2);
  await expect(coverage.getByRole("button", { name: "Saga → Code" })).toHaveCount(1);
  expect(requests[1].searchParams.get("cursor")).toBe("opaque-coverage-2");

  await coverage.getByRole("button", { name: "Saga → Code" }).click();
  await expect(coverage.getByText("saga first page")).toBeVisible();
  await expect(page).toHaveURL(/view=manifest.*mode=saga|mode=saga.*view=manifest/);
  await expect.poll(() => requests.length).toBe(3);
  expect(requests[2].searchParams.get("mode")).toBe("saga");
});
