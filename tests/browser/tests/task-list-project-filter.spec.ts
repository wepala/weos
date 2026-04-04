import { test, expect } from "@playwright/test";

// User Story: As an admin, I should be able to see which project a task belongs
// to and filter by project when I'm looking at the task list.

test.describe("Task List — Project Column & Filter", () => {
  test("task list shows a Project column with project names", async ({
    page,
  }) => {
    await page.goto("/resources/task");

    // The table should have a "Project" column header
    await expect(
      page.locator("th, .ant-table-cell").getByText("Project")
    ).toBeVisible();

    // At least one task should show a project name (not a raw ID)
    // Seeded tasks are linked to "WeOS Development" or "Marketing Site"
    const projectCells = page.locator("td.ant-table-cell").filter({
      hasText: /WeOS Development|Marketing Site/,
    });
    await expect(projectCells.first()).toBeVisible();
  });

  test("task list has a project filter dropdown", async ({ page }) => {
    await page.goto("/resources/task");

    // Should have a project filter select with placeholder
    const filterSelect = page.locator(".ant-select").filter({
      hasText: /Filter by Project/i,
    });
    await expect(filterSelect).toBeVisible();
  });

  test("filtering by project shows only that project's tasks", async ({
    page,
    request,
  }) => {
    await page.goto("/resources/task");

    // Get the seeded projects to know the names
    const projResp = await request.get("/api/project");
    const projects = (await projResp.json()).data;
    const firstProject = projects[0];

    // Open the project filter and select the first project
    const filterSelect = page.locator(".ant-select").filter({
      hasText: /Filter by Project/i,
    });
    await filterSelect.click();

    // Select the project from the dropdown
    await page
      .locator(".ant-select-item-option")
      .filter({ hasText: firstProject.name })
      .click();

    // Wait for the table to update
    await page.waitForTimeout(500);

    // All visible tasks should belong to the selected project
    // The table should NOT show tasks from other projects
    const otherProject = projects[1];
    await expect(
      page.locator("td.ant-table-cell").filter({ hasText: otherProject.name })
    ).toHaveCount(0);

    // Should still show at least one task from the selected project
    await expect(
      page.locator("td.ant-table-cell").filter({ hasText: firstProject.name }).first()
    ).toBeVisible();
  });
});
