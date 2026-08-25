import { AxeBuilder } from "@axe-core/playwright";
import { test as base, expect, type Page } from "@playwright/test";
import { rmSync } from "node:fs";
import {
  attachFailureState,
  createLargeSagaFixture,
  createSagaFixture,
  createSagaRepositories,
  stopSagaFixture,
  type SagaFixture,
  type SagaRepositories
} from "./fixture-builder.js";

type Fixtures = {
  saga: SagaFixture;
  /** A deliberately large saga, served but not yet opened in the browser, so a
   * budget test controls its own navigation and measures the first load. */
  largeSaga: SagaFixture;
  /** Both repositories with no server and no browser page, for subprocess tests. */
  sagaRepositories: SagaRepositories;
  browserEvents: string[];
};

export const test = base.extend<Fixtures>({
  browserEvents: async ({ page }, use) => {
    const events: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error" || message.type() === "warning") events.push(`console:${message.type()}: ${message.text()}`);
    });
    page.on("pageerror", (error) => events.push(`pageerror: ${error.message}`));
    page.on("requestfailed", (request) => events.push(`requestfailed: ${request.method()} ${request.url()} ${request.failure()?.errorText ?? "unknown"}`));
    page.on("response", (response) => {
      if (response.status() >= 400) events.push(`response:${response.status()}: ${response.request().method()} ${response.url()}`);
    });
    await use(events);
  },
  saga: async ({ page, browserEvents }, use, testInfo) => {
    const fixture = await createSagaFixture(testInfo);
    await page.goto(fixture.baseURL);
    await waitForSettledSaga(page);
    try {
      await use(fixture);
    } finally {
      await attachFailureState(fixture, testInfo, browserEvents);
      await stopSagaFixture(fixture);
    }
  },
  largeSaga: async ({ browserEvents }, use, testInfo) => {
    const fixture = await createLargeSagaFixture(testInfo);
    try {
      await use(fixture);
    } finally {
      await attachFailureState(fixture, testInfo, browserEvents);
      await stopSagaFixture(fixture);
    }
  },
  sagaRepositories: async ({}, use, testInfo) => {
    const repositories = createSagaRepositories(testInfo);
    try {
      await use(repositories);
    } finally {
      await attachFailureState(repositories, testInfo, []);
      rmSync(repositories.root, { recursive: true, force: true });
    }
  }
});

export { expect };

type SeriousViolation = { id: string; impact: string; nodes: string[] };

/**
 * The page arrives as a shell and fills in the explanations that are on screen.
 * A reviewer reads and acts on the settled page, so every test that is not
 * itself about the shell starts from there. Explanations further down are
 * deliberately still pending: they are fetched when the reviewer reaches them.
 */
export async function waitForSettledSaga(page: Page): Promise<void> {
  await page.locator("body[data-shell-ready]").waitFor();
}

async function seriousAccessibilityViolations(page: Page, include?: string): Promise<SeriousViolation[]> {
  const builder = new AxeBuilder({ page });
  if (include) builder.include(include);
  const results = await builder.analyze();
  return results.violations
    .filter((violation) => violation.impact === "serious" || violation.impact === "critical")
    .map((violation) => ({ id: violation.id, impact: violation.impact ?? "unknown", nodes: violation.nodes.map((node) => node.target.join(" ")).sort() }))
    .sort((left, right) => left.id.localeCompare(right.id));
}

export async function expectNoSeriousAccessibilityViolations(page: Page, include?: string): Promise<void> {
  expect(
    await seriousAccessibilityViolations(page, include),
    `serious or critical axe violations${include ? ` in ${include}` : " on the whole page"}`
  ).toEqual([]);
}
