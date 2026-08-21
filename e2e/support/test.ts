import { AxeBuilder } from "@axe-core/playwright";
import { test as base, expect, type Page } from "@playwright/test";
import { rmSync } from "node:fs";
import {
  attachFailureState,
  createSagaFixture,
  createSagaRepositories,
  stopSagaFixture,
  type SagaFixture,
  type SagaRepositories
} from "./fixture-builder.js";

type Fixtures = {
  saga: SagaFixture;
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

/**
 * Pins the serious/critical rules a surface is still known to fail. Nothing is
 * excluded from the scan and no rule is disabled: the full result is compared
 * against an exact list, so a new violation, a new rule, or a fixed rule all
 * fail this assertion and force the list to be revisited.
 */
export async function expectOnlyKnownAccessibilityGaps(page: Page, expectedRuleIDs: string[]): Promise<void> {
  const violations = await seriousAccessibilityViolations(page);
  expect(
    violations.map((violation) => violation.id),
    `serious or critical axe rules on the whole page (details: ${JSON.stringify(violations)})`
  ).toEqual([...expectedRuleIDs].sort());
}
