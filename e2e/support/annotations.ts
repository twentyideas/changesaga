import { expect, type Locator, type Page } from "@playwright/test";
import { waitForSettledSaga } from "./test.js";

/** Drags across a locator in fractions of its own box, the way a reviewer draws. */
export async function dragOn(page: Page, locator: Locator, from: [number, number], to: [number, number], steps = 8): Promise<void> {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (!box) throw new Error("annotation surface has no bounding box");
  await page.mouse.move(box.x + box.width * from[0], box.y + box.height * from[1]);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * to[0], box.y + box.height * to[1], { steps });
  await page.mouse.up();
}

export async function selectExactText(page: Page, selector: string, exact: string): Promise<void> {
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

async function submitComposer(page: Page, body: string): Promise<void> {
  const composer = page.locator("form.annotation-compose");
  await composer.locator('textarea[name="body"]').fill(body);
  await Promise.all([page.waitForNavigation(), composer.getByRole("button", { name: "Comment" }).click()]);
  await waitForSettledSaga(page);
}

/** Draws a rectangle over the fragment's overlay and comments on it. */
export async function addRectangleComment(page: Page, fragment: Locator, body: string, from: [number, number] = [0.15, 0.2], to: [number, number] = [0.45, 0.4]): Promise<void> {
  await fragment.focus();
  await page.getByRole("button", { name: "Rectangle" }).click();
  await dragOn(page, fragment.locator("svg.review-overlay"), from, to);
  await submitComposer(page, body);
}

/** Highlights an exact phrase inside the fragment's prose and comments on it. */
export async function addHighlightComment(page: Page, fragment: Locator, exact: string, body: string): Promise<void> {
  await fragment.focus();
  await selectExactText(page, `${await fragmentSelector(fragment)} [data-selectable]`, exact);
  await page.getByRole("button", { name: "Highlight selected text" }).click();
  await submitComposer(page, body);
}

async function fragmentSelector(fragment: Locator): Promise<string> {
  const title = await fragment.getAttribute("data-fragment-title");
  return `[data-fragment-title="${title}"]`;
}

/** Places a sticky note on the fragment's overlay. Its text is the comment. */
export async function addStickyNoteComment(page: Page, fragment: Locator, text: string, at: [number, number] = [0.7, 0.65]): Promise<void> {
  await fragment.focus();
  await page.getByRole("button", { name: "Sticky note" }).click();
  const overlay = fragment.locator("svg.review-overlay");
  await overlay.scrollIntoViewIfNeeded();
  const box = await overlay.boundingBox();
  if (!box) throw new Error("sticky surface has no bounding box");
  await page.mouse.click(box.x + box.width * at[0], box.y + box.height * at[1]);
  await page.getByRole("textbox", { name: "Sticky note text" }).fill(text);
  await Promise.all([page.waitForNavigation(), page.getByRole("button", { name: "Add note" }).click()]);
  await waitForSettledSaga(page);
}

/**
 * A comment drawn onto the content renders as a bubble on its mark rather than
 * in the list below, so a test that wants to read the comment has to open it
 * the way a reviewer would.
 */
export function annotationBubble(page: Page, anchorType: "region" | "drawing" | "text" | "note"): Locator {
  return page.locator(`[data-annotation-bubble][data-anchor-type="${anchorType}"]`);
}

export function bubbleToggle(bubble: Locator): Locator {
  return bubble.locator("[data-annotation-bubble-toggle]");
}

export function bubblePanel(bubble: Locator): Locator {
  return bubble.locator("[data-annotation-bubble-panel]");
}

export async function openAnnotationBubble(bubble: Locator): Promise<Locator> {
  await bubbleToggle(bubble).click();
  const panel = bubblePanel(bubble);
  await expect(panel).toBeVisible();
  return panel;
}

/**
 * Parks the pointer away from every mark and bubble. Creating an annotation
 * leaves the pointer on the mark it just made, which legitimately reveals that
 * mark's comment; a test that wants to prove the reveal has to start from rest.
 */
export async function restPointer(page: Page): Promise<void> {
  await page.mouse.move(2, 2);
}

export type Box = { x: number; y: number; width: number; height: number };

export async function boxOf(locator: Locator, what: string): Promise<Box> {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (!box) throw new Error(`${what} has no bounding box`);
  return box;
}

export function centreOf(box: Box): { x: number; y: number } {
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

/**
 * Asserts that a bubble sits on the mark it belongs to: its centre is within
 * `tolerance` pixels of the mark's top-right corner, which is where both the
 * server's placement and the browser's measurement put it.
 */
export function expectPinnedToCorner(bubble: Box, mark: Box, tolerance = 26): void {
  const centre = centreOf(bubble);
  expect(Math.abs(centre.x - (mark.x + mark.width)), `bubble horizontal offset from the mark's right edge`).toBeLessThanOrEqual(tolerance);
  expect(Math.abs(centre.y - mark.y), `bubble vertical offset from the mark's top edge`).toBeLessThanOrEqual(tolerance);
}
