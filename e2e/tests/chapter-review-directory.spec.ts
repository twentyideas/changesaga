import { expectNoSeriousAccessibilityViolations, expect, test } from "../support/test.js";

test("chapter review directory stays scoped and sticky while section bars own synchronized decisions", async ({ page, largeSaga }) => {
  const sectionRequests: string[] = [];
  const fragmentRequests: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/api/section") sectionRequests.push(url.searchParams.get("target") ?? "");
    if (url.pathname === "/api/fragment") fragmentRequests.push(url.searchParams.get("target") ?? "");
  });

  await page.goto(largeSaga.baseURL, { waitUntil: "load" });
  const chapter = page.locator("section.chapter").first();
  await expect(chapter.locator("[data-chapter-review-directory]")).toHaveCount(0);
  const chapterDecision = chapter.locator(":scope > .chapter-head [data-review-controls]");
  await expect(chapterDecision).toHaveCount(1);
  const decidedBeforeChapterReview = await page.locator("body").getAttribute("data-review-decided");
  const chapterApproved = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await chapterDecision.getByRole("button", { name: /^Approve / }).click();
  await chapterApproved;
  await expect(chapterDecision.getByRole("button", { name: /^Approval recorded/ })).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("body")).toHaveAttribute("data-review-decided", decidedBeforeChapterReview ?? "0");
  expect(sectionRequests).toEqual([]);

  await chapter.getByRole("button", { name: /^Open / }).click();
  const directory = chapter.locator("[data-chapter-review-directory]");
  await expect(directory).toHaveAttribute("open", "");
  await expect(directory.locator("[data-review-directory-target]").first()).toBeVisible();
  expect(sectionRequests).toHaveLength(1);
  expect(await directory.evaluate((element) => getComputedStyle(element).position)).toBe("sticky");
  await expectNoSeriousAccessibilityViolations(page);

  const rows = directory.locator("[data-review-directory-target]");
  const rowCount = await rows.count();
  expect(rowCount).toBeGreaterThan(3);
  const destinationRow = rows.last();
  const target = await destinationRow.getAttribute("data-review-directory-target");
  const href = await destinationRow.getByRole("link").getAttribute("href");
  expect(target).toBeTruthy();
  expect(href).toMatch(/^#/);

  const destination = chapter.locator(href!);
  await destinationRow.getByRole("link").click();
  await expect(page).toHaveURL(new RegExp(`${href!.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`));
  await expect(destination).toBeVisible();
  await expect(destination).not.toHaveAttribute("data-fragment-href", /./);
  expect(fragmentRequests.some((requested) => requested === target)).toBe(true);

  const decision = destination.locator(":scope > .fragment-head [data-review-controls], :scope > .section-head [data-review-controls]");
  const directoryDecision = destinationRow.locator("[data-review-controls]");
  await expect(decision).toHaveCount(1);
  await expect(destinationRow).toHaveAttribute("data-review-state", "unreviewed");
  const approved = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await decision.getByRole("button", { name: /^Approve / }).click();
  await approved;
  await expect(destinationRow).toHaveAttribute("data-review-state", "approved");
  await expect(destinationRow.locator("[data-review-directory-status]")).toHaveText("Approved");
  await expect(directoryDecision.getByRole("button", { name: /^Approval recorded/ })).toHaveAttribute("aria-pressed", "true");

  await directoryDecision.getByRole("button", { name: /^Request changes on/ }).click();
  const directoryForm = directoryDecision.locator("[data-review-decision-form]");
  await expect(directoryForm).toBeVisible();
  await directoryForm.getByRole("textbox", { name: "Optional review note" }).fill("Clarify this explanation.");
  const rejected = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await directoryForm.getByRole("button", { name: "Submit" }).click();
  await rejected;
  await expect(destinationRow).toHaveAttribute("data-review-state", "changes-requested");
  await expect(destinationRow.locator("[data-review-directory-status]")).toHaveText("Changes requested");
  await expect(decision.getByRole("button", { name: /^Changes requested on/ })).toHaveAttribute("aria-pressed", "true");
});
