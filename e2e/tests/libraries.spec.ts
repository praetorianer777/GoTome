import { expect, test } from "@playwright/test";
import { chooseLibrary, openSignedIn } from "../fixtures/app";

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
	await chooseLibrary(page, name);
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
	// One scan runs at a time, and every browser project scans at once.
	await expect(row.getByRole("status")).toHaveText(/^Scanned .+: 3 files, nothing new\.$/, { timeout: 15_000 });

	// The books, with what their files say about them once they are read.
	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Library", exact: true }).click();
	await chooseLibrary(page, name);
	const books = page.getByRole("region", { name: "Books" });
	await expect(books.getByRole("link", { name: /^Emma/ })).toBeVisible();
	await expect(books.getByRole("link", { name: /^Persuasion/ })).toBeVisible();
	await expect(books.getByRole("link", { name: /^Emma/ })).toContainText("Jane Austen", { timeout: 15_000 });

	await books.getByRole("link", { name: /^Emma/ }).click();
	await expect(page.getByRole("heading", { name: "Emma", level: 1 })).toBeVisible();
	await expect(page.getByText("by Jane Austen")).toBeVisible();
	const files = page.getByRole("region", { name: "Files" });
	await expect(files.getByRole("link", { name: "Download Emma.epub" })).toBeVisible();
	await expect(files.getByRole("link", { name: "Download Emma.pdf" })).toBeVisible();

	// A download goes on where it broke off.
	const href = await files.getByRole("link", { name: "Download Emma.pdf" }).getAttribute("href");
	const part = await page.request.get(href ?? "", { headers: { Range: "bytes=0-7" } });
	expect(part.status()).toBe(206);
	expect(await part.text()).toBe("%PDF-1.4");

	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Libraries" }).click();
	await row.getByRole("button", { name: "Remove" }).click();
	await row.getByRole("button", { name: "Remove" }).click();
	await expect(row).toHaveCount(0);
});
