import { expect, test } from "@playwright/test";

test.describe("Docs", () => {
  test("serves docs home at /docs/", async ({ page }) => {
    await page.goto("/docs/");

    await expect(page).toHaveURL(/\/docs\//);
    await expect(page.getByRole("heading", { name: "CI Documentation" })).toBeVisible();
  });
});
