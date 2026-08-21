import { readFileSync } from "node:fs";
import { canonicalLineURI, git, readJSON, relativeToSaga, reviewFiles } from "../support/fixture-builder.js";
import { expect, test } from "../support/test.js";

async function submitWithNavigation(page: import("@playwright/test").Page, action: () => Promise<void>): Promise<void> {
  await Promise.all([page.waitForNavigation(), action()]);
}

test("@critical creates a comment and reply, then resolves and reopens the thread with exact append-only records", async ({ page, saga }) => {
  const overview = page.locator('[data-fragment-title="Overview"]');
  await overview.getByRole("button", { name: "Comment on Overview" }).click();
  const composer = page.locator("form.annotation-compose");
  await composer.locator('textarea[name="body"]').fill("Please explain the retry boundary.");
  await submitWithNavigation(page, () => composer.getByRole("button", { name: "Comment" }).click());

  const thread = page.locator("article.thread").filter({ hasText: "Please explain the retry boundary." });
  await expect(thread).toBeVisible();
  const threadManifests = reviewFiles(saga, /\/thread\.json$/);
  const messageManifests = reviewFiles(saga, /\/message\.json$/);
  const bodies = reviewFiles(saga, /\/body\.fragment\/content\.md$/);
  expect(threadManifests).toHaveLength(1);
  expect(messageManifests).toHaveLength(1);
  expect(bodies).toHaveLength(1);
  const threadRecord = readJSON<{ version: number; id: string; target: string; anchor: { type: string }; kind: string; created_at: string }>(threadManifests[0]);
  expect(threadRecord).toEqual({
    version: 2,
    id: expect.stringMatching(/^\d{8}T/),
    target: "urn:change-saga:wave-one:fragment:wave-one-overview",
    anchor: { type: "target" },
    kind: "comment",
    created_at: expect.stringMatching(/Z$/)
  });
  expect(readFileSync(bodies[0], "utf8")).toBe("Please explain the retry boundary.\n");

  await thread.getByRole("textbox", { name: "Reply" }).fill("The boundary is the repository commit.");
  await submitWithNavigation(page, () => thread.getByRole("button", { name: "Reply" }).click());
  await expect(page.getByText("The boundary is the repository commit.")).toBeVisible();
  expect(reviewFiles(saga, /\/message\.json$/)).toHaveLength(2);
  expect(reviewFiles(saga, /\/body\.fragment\/content\.md$/).map((path) => readFileSync(path, "utf8")).sort()).toEqual([
    "Please explain the retry boundary.\n",
    "The boundary is the repository commit.\n"
  ]);

  const reloadedThread = page.locator("article.thread").filter({ hasText: "Please explain the retry boundary." });
  await submitWithNavigation(page, () => reloadedThread.getByRole("button", { name: "Resolve" }).click());
  await expect(page.locator("article.thread.resolved")).toContainText("Please explain the retry boundary.");
  await submitWithNavigation(page, () => page.locator("article.thread.resolved").getByRole("button", { name: "Reopen" }).click());
  await expect(page.locator("article.thread.open")).toContainText("Please explain the retry boundary.");

  const events = reviewFiles(saga, /\/events\/.*-(resolved|open)\.json$/).map((path) => ({ path: relativeToSaga(saga, path), record: readJSON(path) }));
  expect(events).toHaveLength(2);
  expect(events.map(({ record }) => record)).toEqual([
    expect.objectContaining({ version: 2, state: "resolved", id: expect.any(String), created_at: expect.stringMatching(/Z$/) }),
    expect.objectContaining({ version: 2, state: "open", id: expect.any(String), created_at: expect.stringMatching(/Z$/) })
  ]);
  expect(events.every(({ path }) => path.startsWith(`___review/threads/${threadRecord.id}.thread/events/`))).toBe(true);
});

test("approves, rejects, undoes, updates the progress map, and marks a source file reviewed", async ({ page, saga }) => {
  const progress = page.locator("[data-review-progress]");
  const initialDecided = Number(await page.locator("body").getAttribute("data-review-decided"));
  const overviewControls = page.locator('[data-review-controls][data-review-title="Overview"]');

  const approved = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await overviewControls.getByRole("button", { name: "Approve Overview" }).click();
  await approved;
  await expect(overviewControls.getByRole("button", { name: /Undo approval for Overview/ })).toHaveAttribute("aria-pressed", "true");
  await expect(progress).toHaveAttribute("aria-label", `Review progress: ${initialDecided + 1} of ${await page.locator("body").getAttribute("data-review-total")} decisions`);

  await overviewControls.getByRole("button", { name: /Undo approval for Overview/ }).click();
  const undoForm = overviewControls.locator("[data-review-decision-form]");
  await undoForm.getByRole("textbox", { name: "Optional review note" }).fill("Rechecking the implementation.");
  const undone = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await undoForm.getByRole("button", { name: "Submit" }).click();
  await undone;
  await expect(overviewControls.getByRole("button", { name: "Approve Overview" })).toHaveAttribute("aria-pressed", "false");

  await overviewControls.getByRole("button", { name: "Request changes on Overview" }).click();
  const rejectForm = overviewControls.locator("[data-review-decision-form]");
  await rejectForm.getByRole("textbox", { name: "Optional review note" }).fill("Cover the rollback path.");
  const rejected = page.waitForResponse((response) => response.url().endsWith("/api/review") && response.request().method() === "POST");
  await rejectForm.getByRole("button", { name: "Submit" }).click();
  await rejected;
  await expect(overviewControls.getByRole("button", { name: /Undo request for changes on Overview/ })).toHaveAttribute("aria-pressed", "true");
  await expect(progress.locator('[data-review-progress-note="Cover the rollback path."]')).toHaveCount(1);

  await page.getByRole("tab", { name: "Code Diff" }).click();
  const fileMenu = page.locator('summary[aria-label="Mark this file reviewed"]');
  await fileMenu.click();
  await submitWithNavigation(page, () => page.getByRole("button", { name: "Mark reviewed" }).click());
  await expect(page.getByText(/Reviewed · Local \/ uncommitted/)).toBeVisible();
  const diffReviews = reviewFiles(saga, /\/___review\/diffs\/.*-reviewed\.json$/);
  expect(diffReviews).toHaveLength(1);
  expect(readJSON(diffReviews[0])).toEqual({
    version: 2,
    id: expect.stringMatching(/^\d{8}T/),
    uri: expect.stringMatching(/^saga-diff:\/\/v1\/file\?/),
    state: "reviewed",
    created_at: expect.stringMatching(/Z$/)
  });
});

test("@critical keeps saga and source repositories separate and reloads Git-derived review attribution", async ({ page, saga }) => {
  expect(saga.sagaRepo).not.toBe(saga.sourceRepo);
  const sourceHeadBefore = git(saga.sourceRepo, "rev-parse", "HEAD");
  const sourceStatusBefore = git(saga.sourceRepo, "status", "--short");

  const overview = page.locator('[data-fragment-title="Overview"]');
  await overview.getByRole("button", { name: "Comment on Overview" }).click();
  const composer = page.locator("form.annotation-compose");
  await composer.locator('textarea[name="body"]').fill("Committed attribution check.");
  await submitWithNavigation(page, () => composer.getByRole("button", { name: "Comment" }).click());
  const controls = page.locator('[data-review-controls][data-review-title="Overview"]');
  const reviewResponse = page.waitForResponse((response) => response.url().endsWith("/api/review"));
  await controls.getByRole("button", { name: "Approve Overview" }).click();
  await reviewResponse;

  expect(git(saga.sagaRepo, "status", "--short")).toMatch(/___review|___approvals/);
  git(saga.sagaRepo, "add", "wave-one.saga/___review", "wave-one.saga/overview.fragment/___approvals");
  git(saga.sagaRepo, "commit", "-m", "record browser review");
  expect(git(saga.sourceRepo, "rev-parse", "HEAD")).toBe(sourceHeadBefore);
  expect(git(saga.sourceRepo, "status", "--short")).toBe(sourceStatusBefore);

  await page.reload();
  const committedThread = page.locator("article.thread").filter({ hasText: "Committed attribution check." });
  await expect(committedThread.locator(".thread-meta").first()).toContainText("E2E Reviewer");
  await expect(committedThread.locator(".thread-meta").first().getByTitle(/reviewer@example\.test.*committed/)).toBeVisible();
  await expect(page.locator('[data-review-controls][data-review-title="Overview"]')).toHaveAttribute("data-review-author", "E2E Reviewer");
  await expect(page.locator('[data-review-controls][data-review-title="Overview"]')).toHaveAttribute("data-review-detail", /reviewer@example\.test.*committed/);
});

test("@critical comments on a selected diff line and stores this saga's exact line identity", async ({ page, saga }) => {
  await page.goto(`${saga.baseURL}/?view=code&file=${encodeURIComponent("src/app.go")}`);
  const file = page.locator('article.file-diff[data-file-path="src/app.go"]');
  const row = file.locator('[data-diff-row][data-side="new"]').first();
  const line = Number(await row.getAttribute("data-line"));
  expect(line).toBeGreaterThan(0);

  // The line controls live inside the diff surface; this whole path is dead if
  // an ancestor of the row swallows the click before the line handlers run.
  await row.locator("[data-line-select]").click();
  await expect(row.locator("[data-line-select]")).toHaveAttribute("aria-pressed", "true");
  const toolbar = page.locator("[data-selection-toolbar]");
  await expect(toolbar).toHaveClass(/open/);
  await expect(toolbar.locator("[data-selection-label]")).toHaveText("1 line selected");

  await toolbar.getByRole("button", { name: "Comment on the selected lines" }).click();
  const composer = page.locator("form.diff-compose");
  await expect(composer).toHaveClass(/open/);
  await composer.locator('textarea[name="body"]').fill("This line needs a rollback note.");
  await submitWithNavigation(page, () => composer.getByRole("button", { name: "Add" }).click());
  await expect(page.getByText("This line needs a rollback note.")).toBeVisible();

  const threads = reviewFiles(saga, /\/thread\.json$/).map((path) => readJSON<{ anchor: { type: string; diff?: { uri: string } } }>(path));
  expect(threads).toHaveLength(1);
  expect(threads[0].anchor.type).toBe("diff");
  // The stored identity is this saga's declared repository and comparison, in
  // the one canonical spelling the product accepts back.
  expect(threads[0].anchor.diff?.uri).toBe(canonicalLineURI(saga.identity, "src/app.go", "new", line, line));
});
