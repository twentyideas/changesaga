import { expectNoSeriousAccessibilityViolations, expect, test } from "../support/test.js";

const focusableSelector = 'a[href], area[href], button, input, select, textarea, iframe, summary, [tabindex], [contenteditable="true"]';

/**
 * Which descendants of an element can actually take focus right now. This asks
 * the browser rather than reading attributes, so it stays true whether focus is
 * withheld by `inert`, `hidden`, `display:none`, or a negative tab index.
 */
async function focusableDescendants(locator: import("@playwright/test").Locator, selector: string): Promise<{ candidates: number; focusable: string[] }> {
  return locator.evaluate((element, candidateSelector) => {
    const candidates = [...element.querySelectorAll<HTMLElement>(candidateSelector)];
    const focusable = candidates
      .filter((candidate) => {
        candidate.focus();
        return document.activeElement === candidate;
      })
      .map((candidate) => `${candidate.tagName.toLowerCase()}${candidate.getAttribute("aria-label") ? `[${candidate.getAttribute("aria-label")}]` : ""}`);
    (document.activeElement as HTMLElement | null)?.blur();
    return { candidates: candidates.length, focusable };
  }, selector);
}

test("@critical exposes the workspace switcher as a real tablist with selection and keyboard movement", async ({ page, saga }) => {
  const tablist = page.getByRole("tablist", { name: "Workspace" });
  const tabs = tablist.getByRole("tab");
  await expect(tabs).toHaveCount(3);
  await expect(tabs).toHaveText([/Saga/, /Code Diff/, /Coverage/]);

  const sagaTab = page.getByRole("tab", { name: "Saga" });
  const codeTab = page.getByRole("tab", { name: "Code Diff" });
  const coverageTab = page.getByRole("tab", { name: "Coverage" });

  const selection = async (): Promise<string[]> => tabs.evaluateAll((elements) => elements.map((element) => `${element.textContent?.trim()}:${element.getAttribute("aria-selected")}:${(element as HTMLElement).tabIndex}`));
  // Exactly one tab is selected, and only that tab is in the sequential tab
  // order; the rest are reached with the arrow keys.
  expect(await selection()).toEqual(["Saga:true:0", "Code Diff:false:-1", "Coverage:false:-1"]);

  // Every tab names the panel it controls, and that panel is the visible one.
  for (const [tab, name] of [[sagaTab, "Saga"], [codeTab, "Code Diff"], [coverageTab, "Coverage"]] as const) {
    const controls = await tab.getAttribute("aria-controls");
    await expect(page.locator(`#${controls}`)).toHaveAttribute("aria-labelledby", (await tab.getAttribute("id")) ?? "");
    expect(await page.locator(`#${controls}`).getAttribute("role")).toBe("tabpanel");
    expect(name.length).toBeGreaterThan(0);
  }
  await expect(page.getByRole("tabpanel", { name: "Saga" })).toBeVisible();

  await codeTab.click();
  expect(await selection()).toEqual(["Saga:false:-1", "Code Diff:true:0", "Coverage:false:-1"]);
  await expect(page.getByRole("tabpanel", { name: "Code Diff" })).toBeVisible();
  await expect(page.getByRole("tabpanel", { name: "Saga" })).toBeHidden();
  await expect(page).toHaveURL(/[?&]view=code/);

  await codeTab.focus();
  await page.keyboard.press("ArrowRight");
  await expect(coverageTab).toBeFocused();
  await expect(page.getByRole("tabpanel", { name: "Coverage" })).toBeVisible();
  await page.keyboard.press("ArrowRight");
  await expect(sagaTab).toBeFocused();
  await expect(page.getByRole("tabpanel", { name: "Saga" })).toBeVisible();
  await expect(page.locator("#annotation-toolbox")).toBeHidden();
  await page.keyboard.press("ArrowLeft");
  await expect(coverageTab).toBeFocused();
  await page.keyboard.press("Home");
  await expect(sagaTab).toBeFocused();
  await page.keyboard.press("End");
  await expect(coverageTab).toBeFocused();
  expect(await selection()).toEqual(["Saga:false:-1", "Code Diff:false:-1", "Coverage:true:0"]);

  await sagaTab.click();
  await expect(page.getByRole("tabpanel", { name: "Saga" })).toBeVisible();
  const overview = page.locator('[data-fragment-title="Overview"]');
  await overview.getByRole("button", { name: "Show annotation tools for Overview" }).click();
  const annotationTools = overview.getByRole("toolbar", { name: "Annotation tools for Overview" });
  await expect(annotationTools).toBeVisible();
  await codeTab.click();
  await expect(annotationTools).toBeHidden();
  await sagaTab.click();
  await expect(annotationTools).toBeVisible();
  expect(saga.baseURL).toMatch(/^http:\/\/127\.0\.0\.1:/);
});

test("@critical keeps the closed linked-code drawer inert with no focusable descendants", async ({ page, saga }) => {
  // Located by class, not by role: a correctly closed drawer is absent from the
  // accessibility tree, so a role query could not see it at all.
  const drawer = page.locator("aside.diff-drawer");
  await expect(drawer).toHaveAttribute("aria-hidden", "true");
  await expect(drawer).toHaveAttribute("inert", "");

  const closed = await focusableDescendants(drawer, focusableSelector);
  // The drawer really does hold focusable-looking controls; they simply cannot
  // be focused while it is hidden. Without this the assertion below is vacuous.
  expect(closed.candidates).toBeGreaterThan(0);
  expect(closed.focusable, "focusable descendants of the closed drawer").toEqual([]);

  const overview = page.locator('[data-fragment-title="Overview"]');
  await overview.scrollIntoViewIfNeeded();
  // Linked-code counts arrive from the same intent prefetch a reviewer starts
  // by pointing at the explanation.
  await overview.hover();
  const opener = overview.locator(":scope > .fragment-head").getByRole("button", { name: /Open linked code with \d+ additions? and \d+ deletions?/ });
  await expect(opener).toBeVisible();
  await opener.click();

  await expect(drawer).toHaveAttribute("aria-hidden", "false");
  await expect(drawer).not.toHaveAttribute("inert", /.*/);
  await expect(drawer.getByRole("button", { name: "Close linked code" })).toBeFocused();
  const open = await focusableDescendants(drawer, focusableSelector);
  expect(open.focusable.length, "focusable descendants of the open drawer").toBeGreaterThan(0);
  await expectNoSeriousAccessibilityViolations(page);

  // Probing focusability moved focus around; put it back where opening the
  // drawer left it so the close below starts from the real reviewer state.
  await drawer.getByRole("button", { name: "Close linked code" }).focus();
  await page.keyboard.press("Escape");
  await expect(drawer).toHaveAttribute("aria-hidden", "true");
  await expect(drawer).toHaveAttribute("inert", "");
  // Closing must hand focus back rather than strand it on an inert node.
  await expect(opener).toBeFocused();
  const reclosed = await focusableDescendants(drawer, focusableSelector);
  expect(reclosed.candidates).toBeGreaterThan(0);
  expect(reclosed.focusable, "focusable descendants after the drawer closed again").toEqual([]);
});

test("@critical has no serious or critical axe violations on any workspace view", async ({ page, saga }) => {
  await expectNoSeriousAccessibilityViolations(page);

  await page.getByRole("tab", { name: "Code Diff" }).click();
  await expect(page.locator("article.file-diff").first()).toBeVisible();
  await expectNoSeriousAccessibilityViolations(page);

  await page.getByRole("tab", { name: "Coverage" }).click();
  await expect(page.getByRole("button", { name: "Code → Saga" })).toHaveAttribute("aria-pressed", "true");
  await expectNoSeriousAccessibilityViolations(page);

  await page.getByRole("tab", { name: "Saga" }).click();
  await page.goto(`${saga.baseURL}/chapters/architecture`);
  await expect(page.getByRole("tabpanel", { name: "Saga" }).getByText("The renderer and persistence boundary stay independent.")).toBeVisible();
  await expectNoSeriousAccessibilityViolations(page);
});
