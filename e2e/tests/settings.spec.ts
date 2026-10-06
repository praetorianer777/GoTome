import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

test("a token is saved encrypted and never shown again", async ({
	page,
}, testInfo) => {
	// Settings are global: the browser projects, which share one stack and
	// run at once, would change them under each other's feet.
	test.skip(
		testInfo.project.name !== "chromium",
		"settings are global to the stack",
	);
	const token = `hc-${Date.now()}`;
	await openSignedIn(page);

	await page
		.getByRole("navigation", { name: "Main" })
		.getByRole("link", { name: "Settings" })
		.click();
	await expect(
		page.getByRole("heading", { name: "Settings", level: 1 }),
	).toBeVisible();
	await page.getByLabel("Hardcover API token").fill(token);
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByRole("status")).toHaveText("Saved.");

	await page.reload();
	await expect(page.getByLabel("Hardcover API token")).toHaveValue("");
	// Of this token: other secrets may be set beside it.
	await expect(page.getByLabel("Hardcover API token")).toHaveAccessibleDescription(/Saved on/);
	const answer = await page.request.get("/api/v1/settings");
	expect(await answer.text()).not.toContain(token);

	await page
		.getByRole("button", { name: "Remove the Hardcover API token" })
		.click();
	await expect(page.getByLabel("Hardcover API token")).toHaveAccessibleDescription(/Not set\./);
});
