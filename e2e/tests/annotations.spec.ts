import { annotationBubble, dragOn, openAnnotationBubble, openAnnotationTools, selectExactText } from "../support/annotations.js";
import { readJSON, reviewFiles } from "../support/fixture-builder.js";
import { expect, openSagaSlide, test } from "../support/test.js";

test("@critical drafts, undoes, redoes, submits, moves, recolors, and deletes visual annotations", async ({ page, saga }) => {
  const overview = await openSagaSlide(page, "Overview");
  const overlay = overview.locator("svg.review-overlay");
  await overview.focus();
  let tools = await openAnnotationTools(overview);
  await expect(tools).toHaveAttribute("data-annotation-target", await overview.getAttribute("data-target") ?? "");
  expect(await tools.evaluate((toolbar) => toolbar.closest(".fragment")?.getAttribute("data-fragment-title"))).toBe("Overview");

  await tools.getByRole("button", { name: "Rectangle" }).click();
  await dragOn(page, overlay, [0.15, 0.2], [0.42, 0.42]);
  const pendingRect = overlay.locator(".annotation.pending");
  await expect(pendingRect).toHaveCount(1);
  const composer = page.locator("form.annotation-compose");
  const [markBox, composerBox] = await Promise.all([pendingRect.boundingBox(), composer.boundingBox()]);
  if (!markBox || !composerBox) throw new Error("annotation mark or nearby composer has no bounding box");
  const horizontalGap = Math.max(markBox.x - composerBox.x - composerBox.width, composerBox.x - markBox.x - markBox.width, 0);
  const verticalGap = Math.max(markBox.y - composerBox.y - composerBox.height, composerBox.y - markBox.y - markBox.height, 0);
  expect(Math.hypot(horizontalGap, verticalGap)).toBeLessThanOrEqual(12);

  await tools.getByRole("button", { name: "Freehand" }).click();
  await expect(composer).not.toBeVisible();
  await overlay.scrollIntoViewIfNeeded();
  const drawingBox = await overlay.boundingBox();
  if (!drawingBox) throw new Error("freehand surface has no bounding box");
  await page.mouse.move(drawingBox.x + drawingBox.width * 0.5, drawingBox.y + drawingBox.height * 0.25);
  await page.mouse.down();
  await page.mouse.move(drawingBox.x + drawingBox.width * 0.78, drawingBox.y + drawingBox.height * 0.48, { steps: 12 });
  const liveFreehand = overlay.locator("polyline.annotation.pending");
  expect((await liveFreehand.getAttribute("points"))?.trim().split(/\s+/).length).toBeGreaterThan(2);
  expect(await liveFreehand.evaluate((line) => getComputedStyle(line).stroke)).not.toBe("none");
  await page.mouse.up();
  await expect(overlay.locator(".annotation.pending")).toHaveCount(2);
  await page.getByRole("button", { name: /^Undo / }).click();
  await expect(overlay.locator(".annotation.pending")).toHaveCount(1);
  await page.getByRole("button", { name: /^Redo / }).click();
  await expect(overlay.locator(".annotation.pending")).toHaveCount(2);

  await composer.locator('textarea[name="body"]').fill("Rectangle and freehand review marks.");
  await Promise.all([page.waitForNavigation(), composer.getByRole("button", { name: "Comment" }).click()]);
  // The comment now belongs to the mark it was drawn on, so it reads from the
  // bubble rather than from the list under the fragment.
  await expect(openAnnotationBubble(annotationBubble(page, "drawing"))).resolves.toContainText("Rectangle and freehand review marks.");

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
  tools = await openAnnotationTools(overview);
  await selectExactText(page, '[data-fragment-title="Overview"] [data-selectable]', "exact source changes");
  await tools.getByRole("button", { name: "Highlight selected text" }).click();
  await composer.locator('textarea[name="body"]').fill("Highlighted review claim.");
  await Promise.all([page.waitForNavigation(), composer.getByRole("button", { name: "Comment" }).click()]);
  await expect(openAnnotationBubble(annotationBubble(page, "text"))).resolves.toContainText("Highlighted review claim.");
  const textThread = reviewFiles(saga, /\/thread\.json$/).map((path) => readJSON<{ anchor: { type: string; text?: { exact: string } } }>(path)).find((record) => record.anchor.type === "text");
  expect(textThread?.anchor.text?.exact).toBe("exact source changes");

  await overview.focus();
  tools = await openAnnotationTools(overview);
  await tools.getByRole("button", { name: "Sticky note" }).click();
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
