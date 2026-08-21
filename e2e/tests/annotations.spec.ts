import { readJSON, reviewFiles } from "../support/fixture-builder.js";
import { expect, test } from "../support/test.js";

async function dragOn(page: import("@playwright/test").Page, locator: import("@playwright/test").Locator, from: [number, number], to: [number, number], steps = 8): Promise<void> {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (!box) throw new Error("annotation surface has no bounding box");
  await page.mouse.move(box.x + box.width * from[0], box.y + box.height * from[1]);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * to[0], box.y + box.height * to[1], { steps });
  await page.mouse.up();
}

async function selectExactText(page: import("@playwright/test").Page, selector: string, exact: string): Promise<void> {
  await page.locator(selector).evaluate((root, selectedText) => {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      const start = node.textContent?.indexOf(selectedText) ?? -1;
      if (start < 0) continue;
      const range = document.createRange();
      range.setStart(node, start);
      range.setEnd(node, start + selectedText.length);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      return;
    }
    throw new Error(`text not found: ${selectedText}`);
  }, exact);
}

test("@critical drafts, undoes, redoes, submits, moves, recolors, and deletes visual annotations", async ({ page, saga }) => {
  const overview = page.locator('[data-fragment-title="Overview"]');
  const overlay = overview.locator("svg.review-overlay");
  await overview.focus();

  await page.getByRole("button", { name: "Rectangle" }).click();
  await dragOn(page, overlay, [0.15, 0.2], [0.42, 0.42]);
  await expect(overlay.locator(".annotation.pending")).toHaveCount(1);

  await page.getByRole("button", { name: "Freehand" }).click();
  await dragOn(page, overlay, [0.5, 0.25], [0.78, 0.48], 12);
  await expect(overlay.locator(".annotation.pending")).toHaveCount(2);
  await page.getByRole("button", { name: /^Undo / }).click();
  await expect(overlay.locator(".annotation.pending")).toHaveCount(1);
  await page.getByRole("button", { name: /^Redo / }).click();
  await expect(overlay.locator(".annotation.pending")).toHaveCount(2);

  const composer = page.locator("form.annotation-compose");
  await composer.locator('textarea[name="body"]').fill("Rectangle and freehand review marks.");
  await Promise.all([page.waitForNavigation(), composer.getByRole("button", { name: "Comment" }).click()]);
  await expect(page.getByText("Rectangle and freehand review marks.")).toBeVisible();

  const threadManifests = reviewFiles(saga, /\/thread\.json$/);
  expect(threadManifests).toHaveLength(1);
  const shapeThread = readJSON<{ id: string; anchor: { type: string; coordinate_space: string; shapes: Array<{ type: string; color: string }> } }>(threadManifests[0]);
  expect(shapeThread.anchor).toEqual({
    type: "drawing",
    coordinate_space: "normalized",
    shapes: [
      expect.objectContaining({ type: "rect", color: "#d04832" }),
      expect.objectContaining({ type: "path", color: "#d04832" })
    ]
  });

  await overview.focus();
  await selectExactText(page, '[data-fragment-title="Overview"] [data-selectable]', "exact source changes");
  await page.getByRole("button", { name: "Highlight selected text" }).click();
  await composer.locator('textarea[name="body"]').fill("Highlighted review claim.");
  await Promise.all([page.waitForNavigation(), composer.getByRole("button", { name: "Comment" }).click()]);
  await expect(page.getByText("Highlighted review claim.")).toBeVisible();
  const textThread = reviewFiles(saga, /\/thread\.json$/).map((path) => readJSON<{ anchor: { type: string; text?: { exact: string } } }>(path)).find((record) => record.anchor.type === "text");
  expect(textThread?.anchor.text?.exact).toBe("exact source changes");

  await overview.focus();
  await page.getByRole("button", { name: "Sticky note" }).click();
  await overlay.scrollIntoViewIfNeeded();
  const overlayBox = await overlay.boundingBox();
  if (!overlayBox) throw new Error("sticky surface has no bounding box");
  await page.mouse.click(overlayBox.x + overlayBox.width * 0.7, overlayBox.y + overlayBox.height * 0.65);
  await page.getByRole("textbox", { name: "Sticky note text" }).fill("Sticky persistence check");
  await Promise.all([page.waitForNavigation(), page.getByRole("button", { name: "Add note" }).click()]);
  await expect(page.getByRole("note", { name: /Sticky note by/ })).toContainText("Sticky persistence check");
  const noteThread = reviewFiles(saga, /\/thread\.json$/).map((path) => readJSON<{ anchor: { type: string; note?: { text: string } } }>(path)).find((record) => record.anchor.type === "note");
  expect(noteThread?.anchor.note?.text).toBe("Sticky persistence check");

  const committedGroup = page.locator(`[data-annotation-entity][data-thread-id="${shapeThread.id}"]`);
  const committedRect = committedGroup.locator('.annotation[data-shape-index="0"]');
  await committedRect.click({ force: true });
  const moved = page.waitForResponse((response) => response.url().endsWith("/api/thread-anchor") && response.request().method() === "POST");
  await dragOn(page, committedRect, [0.5, 0.5], [0.62, 0.62]);
  await moved;

  const recolored = page.waitForResponse((response) => response.url().endsWith("/api/thread-anchor") && response.request().method() === "POST");
  await page.locator("[data-annotation-color]").evaluate((input) => {
    const picker = input as HTMLInputElement;
    picker.value = "#336699";
    picker.dispatchEvent(new Event("input", { bubbles: true }));
    picker.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await recolored;
  await expect(committedRect).toHaveAttribute("stroke", "#336699");

  const removedShape = page.waitForResponse((response) => response.url().endsWith("/api/thread-anchor") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Delete selected annotation" }).click();
  await removedShape;
  await expect(committedGroup.locator(".annotation")).toHaveCount(1);
  const anchorEvents = reviewFiles(saga, new RegExp(`/threads/${shapeThread.id}\\.thread/events/.*-anchor\\.json$`)).map((path) => readJSON<{ anchor: { shapes: unknown[] } }>(path));
  expect(anchorEvents).toHaveLength(3);

  // One committed shape remains, proving deletion updated the persisted
  // multi-shape annotation rather than only hiding a DOM node.
  expect(anchorEvents.at(-1)?.anchor.shapes).toHaveLength(1);
});
