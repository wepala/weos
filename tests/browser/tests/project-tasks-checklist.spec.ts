import { test, expect } from "@playwright/test";

// User Story: As an admin, I should be able to create tasks on the project
// detail page. Tasks should display as a checklist with checkboxes.
// Each task should have an edit link to change priority, etc.

test.describe("Project Detail — Task Checklist", () => {
  let projectUrl: string;

  test.beforeEach(async ({ request }) => {
    const resp = await request.get("/api/project");
    const body = await resp.json();
    const project = body.data[0];
    projectUrl = `/resources/project/${project.id}`;
  });

  test("shows a task checklist section on the project page", async ({
    page,
  }) => {
    await page.goto(projectUrl);
    await expect(page.getByRole("heading", { name: /task/i })).toBeVisible();
    const checkboxes = page.locator(".task-checklist .ant-checkbox");
    await expect(checkboxes.first()).toBeVisible();
  });

  test("seeded tasks appear as checklist items", async ({ page }) => {
    await page.goto(projectUrl);
    const checklist = page.locator(".task-checklist");
    await expect(checklist).toBeVisible();
    const items = checklist.locator(".ant-list-item");
    const count = await items.count();
    expect(count).toBeGreaterThanOrEqual(2);
  });

  test("create a task inline by typing and pressing Enter", async ({
    page,
  }) => {
    await page.goto(projectUrl);
    const checklist = page.locator(".task-checklist");
    await expect(checklist.locator(".ant-list-item").first()).toBeVisible();
    const countBefore = await checklist.locator(".ant-list-item").count();

    const input = page.getByPlaceholder("Add a task...");
    await expect(input).toBeVisible();
    await input.fill("New checklist task");
    await input.press("Enter");

    await expect(page.getByText("New checklist task")).toBeVisible();
    await expect(checklist.locator(".ant-list-item")).toHaveCount(
      countBefore + 1
    );
    await expect(input).toHaveValue("");
  });

  test("check off a task to mark it as done", async ({ page }) => {
    await page.goto(projectUrl);
    const firstItem = page.locator(".task-checklist .ant-list-item").first();
    await expect(firstItem).toBeVisible();
    const checkbox = firstItem.locator(".ant-checkbox");
    await checkbox.click();
    await expect(firstItem.locator(".task-done")).toBeVisible();
  });

  test("each task has an edit link", async ({ page }) => {
    await page.goto(projectUrl);

    // Wait for checklist to load
    const firstItem = page.locator(".task-checklist .ant-list-item").first();
    await expect(firstItem).toBeVisible();

    // Each task item should have an Edit link
    const editLink = firstItem.getByRole("link", { name: /edit/i });
    await expect(editLink).toBeVisible();

    // The edit link should point to the task edit page
    const href = await editLink.getAttribute("href");
    expect(href).toMatch(/\/resources\/task\/urn:.*\/edit/);
  });

  test("clicking edit link navigates to task edit page", async ({ page }) => {
    await page.goto(projectUrl);

    const firstItem = page.locator(".task-checklist .ant-list-item").first();
    await expect(firstItem).toBeVisible();

    // Click the edit link
    await firstItem.getByRole("link", { name: /edit/i }).click();

    // Should navigate to the task edit page
    await expect(page).toHaveURL(/\/resources\/task\/urn:.*\/edit/);

    // The edit form should be visible
    await expect(page.getByLabel("Priority")).toBeVisible();
  });
});
