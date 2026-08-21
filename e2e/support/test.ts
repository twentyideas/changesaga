import { AxeBuilder } from "@axe-core/playwright";
import { test as base, expect, type Page } from "@playwright/test";
import { attachFailureState, createSagaFixture, stopSagaFixture, type SagaFixture } from "./fixture-builder.js";

type Fixtures = {
  saga: SagaFixture;
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
  }
});

export { expect };

export async function expectNoSeriousAccessibilityViolations(page: Page, include?: string): Promise<void> {
  const builder = new AxeBuilder({ page });
  if (include) builder.include(include);
  const results = await builder.analyze();
  expect(results.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical"), "serious or critical axe violations").toEqual([]);
}
