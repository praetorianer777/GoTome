import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

test("an administrator adds a library, sees it, and removes it again", async ({ page }, testInfo) => {
	// Every browser project runs against the same stack, so each needs a name
	// of its own.
	const name = `Novels ${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);

	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Libraries" }).click();
	await expect(page.getByRole("heading", { name: "Libraries", level: 1 })).toBeVisible();

	const form = page.getByRole("region", { name: "Add a library" });
	await form.getByLabel("Name").fill(name);
	await form.getByRole("button", { name: "Add the library" }).click();

	const row = page.getByRole("listitem", { name });
	await expect(row).toBeVisible();
	await expect(row.getByText("Managed by GOtome")).toBeVisible();
	// The server made a folder for it under its data directory.
	await expect(row.getByText(/^\/data\/libraries\//)).toBeVisible();

	// The same name again is refused at the field.
	await form.getByLabel("Name").fill(name.toUpperCase());
	await form.getByRole("button", { name: "Add the library" }).click();
	await expect(form.getByText("Another library already has this name.")).toBeVisible();

	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Library", exact: true }).click();
	await expect(page.getByRole("heading", { name, level: 2 })).toBeVisible();

	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Libraries" }).click();
	await row.getByRole("button", { name: "Remove" }).click();
	await expect(row.getByText("Remove it from GOtome? The folder and its files stay.")).toBeVisible();
	await row.getByRole("button", { name: "Remove" }).click();
	await expect(row).toHaveCount(0);
});
