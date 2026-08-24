import { expectNoSeriousAccessibilityViolations, expect, test } from "../support/test.js";

test("chapter review directory stays scoped and sticky, navigates lazy items, and owns their decisions", async ({ page, largeSaga }) => {
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
  await expect(chapter.locator(":scope > .chapter-head [data-review-controls]")).toHaveCount(0);
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

  await expect(destination.locator("[data-review-controls]")).toHaveCount(0);
  const decision = destinationRow.locator("[data-review-controls]");
  await expect(destinationRow).toHaveAttribute("data-review-state", "unreviewed");
  const approved = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await decision.getByRole("button", { name: /^Approve / }).click();
  await approved;
  await expect(destinationRow).toHaveAttribute("data-review-state", "approved");
  await expect(destinationRow.locator("[data-review-directory-status]")).toHaveText("Approved");

  await decision.getByRole("button", { name: /^Undo approval/ }).click();
  const form = decision.locator("[data-review-decision-form]");
  await expect(form).toBeVisible();
  const unreviewed = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await form.getByRole("button", { name: "Submit" }).click();
  await unreviewed;
  await expect(destinationRow).toHaveAttribute("data-review-state", "unreviewed");

  await decision.getByRole("button", { name: /^Request changes on/ }).click();
  await expect(form).toBeVisible();
  await form.getByRole("textbox", { name: "Optional review note" }).fill("Clarify this explanation.");
  const rejected = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await form.getByRole("button", { name: "Submit" }).click();
  await rejected;
  await expect(destinationRow).toHaveAttribute("data-review-state", "changes-requested");
  await expect(destinationRow.locator("[data-review-directory-status]")).toHaveText("Changes requested");
});
