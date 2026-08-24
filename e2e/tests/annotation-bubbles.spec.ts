import { readFileSync } from "node:fs";
import {
  addHighlightComment,
  addRectangleComment,
  addStickyNoteComment,
  annotationBubble,
  boxOf,
  bubblePanel,
  bubbleToggle,
  centreOf,
  expectPinnedToCorner,
  openAnnotationBubble,
  restPointer
} from "../support/annotations.js";
import { readJSON, reviewFiles, serverRequest } from "../support/fixture-builder.js";
import { expect, expectNoSeriousAccessibilityViolations, test } from "../support/test.js";

const overviewSelector = '[data-fragment-title="Overview"]';

test("@critical pins a drawn comment to its mark, reveals it on hover and focus, and replies inside the bubble", async ({ page, saga }) => {
  const overview = page.locator(overviewSelector);
  await addRectangleComment(page, overview, "The rectangle marks the retry boundary.");
  await restPointer(page);

  const bubble = annotationBubble(page, "region");
  const panel = bubblePanel(bubble);
  await expect(bubble).toHaveCount(1);
  await expect(panel).toBeHidden();
  await expect(bubbleToggle(bubble)).toHaveAttribute("aria-expanded", "false");
  await expect(bubbleToggle(bubble)).toHaveAccessibleName("1 comment on this rectangle");

  // The comment left the list under the content and took its place on the mark.
  await expect(overview.locator("> .threads")).toHaveCount(0);
  const rectangle = overview.locator("[data-annotation-entity] .annotation").first();
  expectPinnedToCorner(await boxOf(bubble, "the bubble"), await boxOf(rectangle, "the rectangle"));
  const stage = await boxOf(overview.locator(".fragment-stage"), "the fragment stage");
  const centre = centreOf(await boxOf(bubble, "the bubble"));
  expect(centre.x, "the bubble stays inside its fragment").toBeGreaterThanOrEqual(stage.x - 1);
  expect(centre.x).toBeLessThanOrEqual(stage.x + stage.width + 1);

  // Hovering the mark itself reveals the comment, and leaving it hides it again.
  await rectangle.hover({ force: true });
  await expect(panel).toBeVisible();
  await expect(panel).toContainText("The rectangle marks the retry boundary.");
  await expect(bubbleToggle(bubble)).toHaveAttribute("aria-expanded", "true");
  await page.locator("header.topbar .brand").hover();
  await expect(panel).toBeHidden();

  // Hovering the bubble reveals the same thread.
  await bubbleToggle(bubble).hover();
  await expect(panel).toBeVisible();
  await page.locator("header.topbar .brand").hover();
  await expect(panel).toBeHidden();

  // Keyboard reviewers reach the comment by focusing the bubble, and Escape
  // closes it without stranding focus inside a hidden panel.
  await bubbleToggle(bubble).focus();
  await expect(panel).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  await expect(bubbleToggle(bubble)).toBeFocused();
  await expect(bubbleToggle(bubble)).toHaveAttribute("aria-expanded", "false");

  await expectNoSeriousAccessibilityViolations(page);
  await openAnnotationBubble(bubble);
  await expectNoSeriousAccessibilityViolations(page);

  // A reply written inside the bubble is an ordinary reply on the same thread.
  const thread = panel.locator("article.thread");
  await thread.getByRole("textbox", { name: "Reply" }).fill("Agreed, the boundary is the commit.");
  await Promise.all([page.waitForNavigation(), thread.getByRole("button", { name: "Reply" }).click()]);
  expect(reviewFiles(saga, /\/message\.json$/)).toHaveLength(2);
  expect(reviewFiles(saga, /\/body\.fragment\/content\.md$/).map((path) => readFileSync(path, "utf8")).sort()).toEqual([
    "Agreed, the boundary is the commit.\n",
    "The rectangle marks the retry boundary.\n"
  ]);
  await expect(bubbleToggle(annotationBubble(page, "region"))).toHaveAccessibleName("2 comments on this rectangle");
});

test("@critical keeps an annotation comment on its mark across a reload", async ({ page, saga }) => {
  const overview = page.locator(overviewSelector);
  await addRectangleComment(page, overview, "Persisted rectangle comment.");
  const threads = reviewFiles(saga, /\/thread\.json$/);
  expect(threads).toHaveLength(1);
  const stored = readJSON<{ id: string; anchor: { shapes: Array<{ x: number; y: number; width: number }> } }>(threads[0]);

  await page.reload();
  const bubble = page.locator(`[data-annotation-bubble][data-thread-id="${stored.id}"]`);
  await expect(bubble).toHaveCount(1);
  await expect(bubblePanel(bubble)).toBeHidden();

  const rectangle = page.locator(`[data-annotation-entity][data-thread-id="${stored.id}"] .annotation`).first();
  expectPinnedToCorner(await boxOf(bubble, "the reloaded bubble"), await boxOf(rectangle, "the reloaded rectangle"));

  // The bubble above was measured after script ran. The explanation the page
  // fetches already carries the placement, so the mark and its comment agree
  // from the first paint of that content rather than jumping into position.
  const target = await page.locator(overviewSelector).getAttribute("data-target");
  const markup = await serverRequest(saga.baseURL, `/api/fragment?target=${encodeURIComponent(target!)}`);
  const served = new RegExp(`data-thread-id="${stored.id}"[^>]*style="left:([\\d.]+)%;top:([\\d.]+)%"`).exec(markup.body);
  expect(served, "the served explanation placed the bubble itself").not.toBeNull();
  expect(Number(served?.[1])).toBeCloseTo((stored.anchor.shapes[0].x + stored.anchor.shapes[0].width) * 100, 3);
  expect(Number(served?.[2])).toBeCloseTo(stored.anchor.shapes[0].y * 100, 3);

  await expect(openAnnotationBubble(bubble)).resolves.toContainText("Persisted rectangle comment.");
});

test("@critical pins sticky note and highlight comments to their own marks", async ({ page, saga }) => {
  const overview = page.locator(overviewSelector);
  await addStickyNoteComment(page, overview, "Sticky bubble check");
  await restPointer(page);

  const noteBubble = annotationBubble(page, "note");
  const note = page.getByRole("note", { name: /Sticky note by/ });
  await expect(noteBubble).toHaveCount(1);
  await expect(bubblePanel(noteBubble)).toBeHidden();
  expectPinnedToCorner(await boxOf(noteBubble, "the note bubble"), await boxOf(note, "the sticky note"));

  // The note is the annotation, so hovering the paper reveals its thread.
  await note.hover();
  await expect(bubblePanel(noteBubble)).toBeVisible();
  await expect(bubblePanel(noteBubble)).toContainText("Sticky bubble check");
  await page.locator("header.topbar .brand").hover();
  await expect(bubblePanel(noteBubble)).toBeHidden();

  await addHighlightComment(page, overview, "exact source changes", "The highlighted claim needs a citation.");
  await restPointer(page);
  const textBubble = annotationBubble(page, "text");
  const mark = page.locator(`${overviewSelector} mark[data-text-mark]`);
  await expect(mark).toHaveCount(1);
  // A highlight has no stored geometry: the browser measures the rendered text.
  expectPinnedToCorner(await boxOf(textBubble, "the highlight bubble"), await boxOf(mark, "the highlight"));

  await bubbleToggle(textBubble).focus();
  await expect(bubblePanel(textBubble)).toBeVisible();
  await expect(bubblePanel(textBubble)).toContainText("The highlighted claim needs a citation.");
  // Both marks still carry their own comment; revealing one does not open both.
  await expect(bubblePanel(noteBubble)).toBeHidden();
});

test("@critical leaves comments that were not drawn on the content in the list below it", async ({ page, saga }) => {
  const overview = page.locator(overviewSelector);
  await overview.getByRole("button", { name: "Comment on Overview" }).click();
  const composer = page.locator("form.annotation-compose");
  await composer.locator('textarea[name="body"]').fill("A comment on the whole explanation.");
  await Promise.all([page.waitForNavigation(), composer.getByRole("button", { name: "Comment" }).click()]);

  // Visible with no hover, no focus, and no bubble anywhere on the page.
  const fragmentThread = overview.locator("> .threads article.thread");
  await expect(fragmentThread).toHaveCount(1);
  await expect(fragmentThread).toBeVisible();
  await expect(fragmentThread).toContainText("A comment on the whole explanation.");
  await expect(page.locator("[data-annotation-bubble]")).toHaveCount(0);

  // A chapter comment keeps the same placement it always had: in the list
  // inside the chapter. The page ships that chapter as a summary, so reading
  // the comment means opening the chapter, exactly as seeing it always did.
  const chapter = page.locator("section.chapter", { has: page.getByRole("link", { name: "Architecture" }) });
  await chapter.getByRole("button", { name: "Open Architecture" }).click();
  await chapter.getByRole("button", { name: "Comment on Architecture" }).first().click();
  await composer.locator('textarea[name="body"]').fill("A comment on the whole chapter.");
  await Promise.all([page.waitForNavigation(), composer.getByRole("button", { name: "Comment" }).click()]);

  // Navigating to the chapter opens it, which is what reading anything inside
  // it has always meant; the page now fetches the body at the same moment.
  await page.goto(`${saga.baseURL}/#${await chapter.getAttribute("id")}`);
  await expect(chapter.getByRole("button", { name: "Close Architecture" })).toHaveAttribute("aria-expanded", "true");

  const chapterThread = page.locator("article.thread").filter({ hasText: "A comment on the whole chapter." });
  await expect(chapterThread).toHaveCount(1);
  await expect(chapterThread.locator("xpath=ancestor::*[@data-annotation-bubble]")).toHaveCount(0);
  await expect(page.locator("[data-annotation-bubble]")).toHaveCount(0);

  const anchors = reviewFiles(saga, /\/thread\.json$/).map((path) => readJSON<{ anchor: { type: string } }>(path).anchor.type);
  expect(anchors.sort()).toEqual(["target", "target"]);
});

test("@critical keeps an annotated comment's mark selectable, movable, and removable", async ({ page, saga }) => {
  const overview = page.locator(overviewSelector);
  await addStickyNoteComment(page, overview, "Movable sticky");
  await restPointer(page);

  const note = page.getByRole("note", { name: /Sticky note by/ });
  const bubble = annotationBubble(page, "note");
  const before = await boxOf(note, "the sticky note");
  const placed = readJSON<{ anchor: { note: { x: number } } }>(reviewFiles(saga, /\/thread\.json$/)[0]).anchor.note.x;

  // Selecting the note and nudging it with the keyboard still works, and the
  // comment travels with the mark instead of being left behind.
  await note.click();
  await expect(note).toBeFocused();
  const moved = page.waitForResponse((response) => response.url().endsWith("/api/thread-anchor") && response.request().method() === "POST");
  await page.keyboard.press("Shift+ArrowRight");
  await moved;
  const after = await boxOf(note, "the moved sticky note");
  expect(after.x, "the note moved right").toBeGreaterThan(before.x + 10);
  expectPinnedToCorner(await boxOf(bubble, "the bubble after the move"), after);
  // The move is appended as an anchor event; the original record is untouched.
  const anchorEvents = reviewFiles(saga, /\/events\/.*-anchor\.json$/);
  expect(anchorEvents).toHaveLength(1);
  const movedNote = readJSON<{ anchor: { note: { x: number; text: string } } }>(anchorEvents[0]).anchor.note;
  expect(movedNote.x, "Shift+Arrow moves the note by a twentieth of the stage").toBeCloseTo(placed + 0.05, 5);
  expect(movedNote.text).toBe("Movable sticky");
  expect(readJSON<{ anchor: { note: { x: number } } }>(reviewFiles(saga, /\/thread\.json$/)[0]).anchor.note.x).toBe(placed);

  // Deleting the mark withdraws its comment, so the bubble goes with it.
  await Promise.all([
    page.waitForNavigation(),
    page.getByRole("button", { name: "Delete selected annotation" }).click()
  ]);
  await expect(note).toHaveCount(0);
  await expect(page.locator("[data-annotation-bubble]")).toHaveCount(0);
  await expect(page.getByText("Movable sticky")).toHaveCount(0);
});
