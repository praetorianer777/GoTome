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

test("an existing folder is scanned when it is added, and again when asked", async ({ page }, testInfo) => {
	const folder = `/fixtures/books-${testInfo.project.name}`;
	const name = `Shelf ${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Libraries" }).click();
	await expect(page.getByRole("heading", { name: "Libraries", level: 1 })).toBeVisible();

	// An earlier attempt that failed half-way may have left its library on
	// the folder, and a folder belongs to one library.
	const leftover = page.getByRole("listitem").filter({ hasText: folder });
	while ((await leftover.count()) > 0) {
		await leftover.first().getByRole("button", { name: "Remove" }).click();
		await leftover.first().getByRole("button", { name: "Remove" }).click();
		await expect(leftover).toHaveCount((await leftover.count()) - 1);
	}

	const form = page.getByRole("region", { name: "Add a library" });
	await form.getByLabel("Name").fill(name);
	await form.getByLabel("Kind").selectOption("An existing folder");
	await form.getByRole("textbox", { name: "Folder" }).fill(folder);
	await form.getByRole("button", { name: "Add the library" }).click();

	// Nobody asks for the first scan; the page follows it to its end.
	const row = page.getByRole("listitem", { name });
	await expect(row.getByRole("status")).toHaveText(/^Scanned .+: 3 files, 3 new\.$/);

	await row.getByRole("button", { name: "Scan now" }).click();
	await expect(row.getByRole("status")).toHaveText(/^Scanned .+: 3 files, nothing new\.$/);

	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Library", exact: true }).click();
	await expect(
		page.getByRole("region", { name }).getByText("The last scan found 3 files in this library's folder."),
	).toBeVisible();

	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Libraries" }).click();
	await row.getByRole("button", { name: "Remove" }).click();
	await row.getByRole("button", { name: "Remove" }).click();
	await expect(row).toHaveCount(0);
});
