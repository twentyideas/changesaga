import { expect, test } from "../support/test.js";
import { canonicalLineURI } from "../support/fixture-builder.js";

test("bounded Code Diff retries a building snapshot, preserves its deep link, and appends pages", async ({ page, largeSaga }) => {
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
        <div data-code-panel-content><span data-code-meta-content hidden>0/2 reviewed</span><div class="code-toolbar"><button type="button" data-layout="inline">Unified</button></div><article class="file-diff" data-file-path="${file}" data-code-file-href="/api/file-diff?file=${encodeURIComponent(file)}&amp;diff=${encodeURIComponent(diff)}"><div class="diff-surface" data-diff-surface><div data-diff-body data-page-items="lines"><p data-diff-placeholder>Loading</p></div></div></article></div>
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
  await expect(surface.locator(".diff-row")).toHaveCount(1);
  expect(fileRequests[0].searchParams.get("file")).toBe(file);
  expect(fileRequests[0].searchParams.get("diff")).toBe(diff);
  await surface.getByRole("button", { name: "Load the next file chunk" }).click();
  await expect(surface.locator(".diff-row")).toHaveCount(2);
  await expect(surface.getByRole("button", { name: "Unified" })).toHaveCount(1);
  expect(fileRequests[1].searchParams.get("cursor")).toBe("opaque-lines-2");
});

test("Coverage loads one direction at a time and keeps its controls while appending", async ({ page, largeSaga }) => {
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
  expect(requests).toHaveLength(1);

  await coverage.getByRole("button", { name: "Load the next page" }).click();
  await expect(coverage.getByText("second page")).toBeVisible();
  await expect(coverage.getByRole("button", { name: "Saga → Code" })).toHaveCount(1);
  expect(requests[1].searchParams.get("cursor")).toBe("opaque-coverage-2");

  await coverage.getByRole("button", { name: "Saga → Code" }).click();
  await expect(coverage.getByText("saga first page")).toBeVisible();
  await expect(page).toHaveURL(/view=manifest.*mode=saga|mode=saga.*view=manifest/);
  expect(requests[2].searchParams.get("mode")).toBe("saga");
});
