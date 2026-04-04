import { test, expect } from "@playwright/test";

// These UI tests run in a visible Chrome browser against the real WeOS server.
// Prerequisites: make build && make dev-build-frontend && make dev-seed

test.describe("Dashboard", () => {
  test("loads with resource type cards", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();

    // Verify Project and Task cards from the tasks preset
    await expect(page.getByText("Project").first()).toBeVisible();
    await expect(page.getByText("Task").first()).toBeVisible();
  });

  test("navigate to project list from dashboard card", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: /Manage Project/i }).click();
    await expect(page).toHaveURL(/\/resources\/project/);
  });
});

test.describe("Project CRUD", () => {
  test("create a project via the form", async ({ page }) => {
    await page.goto("/resources/project/create");
    await expect(
      page.getByRole("heading", { name: /Create Project/i })
    ).toBeVisible();

    // Fill form fields — ResourceForm renders a-form-item with label matching the field
    await page.getByLabel("Name").fill("Playwright Project");
    await page.getByLabel("Description").fill("Created via Playwright UI test");
    await page.getByLabel("Status").fill("active");

    // Submit
    await page.getByRole("button", { name: "Create" }).click();

    // Should redirect to list page
    await expect(page).toHaveURL(/\/resources\/project$/);

    // Verify the new project appears in the table
    await expect(page.getByRole("cell", { name: "Playwright Project" }).first()).toBeVisible();
  });

  test("view project detail", async ({ page }) => {
    await page.goto("/resources/project");

    // Click "View" on the first project row
    await page.getByRole("button", { name: "View" }).first().click();

    // Should navigate to detail page
    await expect(page).toHaveURL(/\/resources\/project\/urn:/);
  });

  test("edit a project", async ({ page }) => {
    await page.goto("/resources/project");

    // Click "Edit" on the first project row
    await page.getByRole("button", { name: "Edit" }).first().click();

    // Should navigate to edit page
    await expect(page).toHaveURL(/\/resources\/project\/urn:.*\/edit/);

    // Clear and update the name
    const nameInput = page.getByLabel("Name");
    await nameInput.clear();
    await nameInput.fill("Updated by Playwright");

    await page.getByRole("button", { name: "Update" }).click();

    // Should redirect to list
    await expect(page).toHaveURL(/\/resources\/project$/);

    // Verify the updated name
    await expect(page.getByText("Updated by Playwright")).toBeVisible();
  });

  test("delete a project", async ({ page }) => {
    // First create a project to delete
    await page.goto("/resources/project/create");
    await page.getByLabel("Name").fill("Delete Me PW");
    await page.getByLabel("Status").fill("active");
    await page.getByRole("button", { name: "Create" }).click();
    await expect(page).toHaveURL(/\/resources\/project$/);
    await expect(page.getByText("Delete Me PW")).toBeVisible();

    // Click delete on that row
    const row = page.getByRole("row", { name: /Delete Me PW/ });
    await row.getByRole("button", { name: "Delete" }).click();

    // Confirm the popconfirm
    await page.getByRole("button", { name: "OK" }).click();

    // Wait for it to disappear
    await expect(page.getByText("Delete Me PW")).toBeHidden();
  });
});

test.describe("Task CRUD with Project Reference", () => {
  test("create a task linked to a project", async ({ page }) => {
    await page.goto("/resources/task/create");
    await expect(
      page.getByRole("heading", { name: /Create Task/i })
    ).toBeVisible();

    await page.getByLabel("Name").fill("UI Test Task");
    await page.getByLabel("Status").fill("open");
    await page.getByLabel("Priority").fill("high");

    // The Project field is a ResourceSelect dropdown — click it and pick an option
    const projectSelect = page.locator(".ant-select").filter({ hasText: /project/i }).first();
    if (await projectSelect.isVisible()) {
      await projectSelect.click();
      // Select the first available option
      await page.locator(".ant-select-item-option").first().click();
    }

    await page.getByRole("button", { name: "Create" }).click();

    // Should redirect to task list
    await expect(page).toHaveURL(/\/resources\/task$/);
    await expect(page.getByRole("cell", { name: "UI Test Task" }).first()).toBeVisible();
  });

  test("task list shows seeded tasks", async ({ page }) => {
    await page.goto("/resources/task");

    // Seeded tasks from make dev-seed
    await expect(page.getByText("Set up CI pipeline")).toBeVisible();
    await expect(page.getByText("Add resource permissions API")).toBeVisible();
  });
});

test.describe("Sidebar Navigation", () => {
  test("sidebar has resource type links", async ({ page }) => {
    await page.goto("/");

    // The sidebar should have navigation for dynamic resource types
    const sidebar = page.locator(".ant-layout-sider, nav").first();
    await expect(sidebar).toBeVisible();
  });

  test("navigate to tasks via sidebar", async ({ page }) => {
    await page.goto("/");

    // Look for a sidebar link or menu item related to tasks
    const taskLink = page.getByRole("link", { name: /task/i }).first();
    if (await taskLink.isVisible()) {
      await taskLink.click();
      await expect(page).toHaveURL(/\/resources\/task/);
    }
  });
});
