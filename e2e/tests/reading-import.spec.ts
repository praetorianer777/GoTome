import { expect, test } from "@playwright/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";

test("a Goodreads export is previewed, imported, and lists books the library lacks", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const held = `Dune ${stamp}`;
	const missing = `Foundation ${stamp}`;
	await openSignedIn(page);
	const { books } = await libraryOfBooks(page, `Import ${stamp}`, [held]);
	const edited = await page.request.patch(`/api/v1/books/${books[held]}`, {
		data: { contributors: [{ name: "Frank Herbert", role: "author" }] },
	});
	expect(edited.status()).toBe(200);

	const csv = [
		"Title,Author,Additional Authors,ISBN,ISBN13,My Rating,Date Read,Bookshelves,Exclusive Shelf,My Review",
		`${held},Frank Herbert,,"=""""","=""""",5,2021/03/14,favorites,read,Spice must flow.`,
		`${missing},Isaac Asimov,,"=""""","=""""",4,2019/07/02,,read,`,
	].join("\n");

	await page.goto("/profile");
	const section = page.getByRole("region", { name: "Import reading history" });
	await section.getByLabel("Export file (CSV)").setInputFiles({
		name: "goodreads.csv",
		mimeType: "text/csv",
		buffer: Buffer.from(csv),
	});
	await section.getByRole("button", { name: "Preview" }).click();
	await expect(section.getByText("Goodreads export")).toBeVisible();
	await expect(section.getByText("1 books already in your libraries")).toBeVisible();
	await expect(
		section.getByText("1 books it does not hold, to be added as books you read"),
	).toBeVisible();

	await section.getByRole("button", { name: "Import" }).click();
	await expect(section.getByText("Done.")).toBeVisible({ timeout: 30_000 });
	await expect(section.getByText("1 books added")).toBeVisible();

	await page.goto(`/books/${books[held]}`);
	await expect(
		page.getByRole("combobox", { name: "Your status" }),
	).toHaveValue("completed");
	await expect(page.getByRole("textbox", { name: "Private note" })).toHaveValue(
		"Spice must flow.",
	);

	await page.goto("/stats");
	const history = page.getByRole("region", { name: "Everything you read" });
	await history.getByRole("searchbox", { name: "Title or author" }).fill(stamp);
	await expect(history.getByRole("link", { name: new RegExp(`^${held}`) })).toBeVisible();
	const lacking = history.getByRole("listitem").filter({ hasText: missing });
	await expect(lacking).toContainText("Not in the library");
	await history.getByRole("combobox", { name: "Year" }).selectOption("2019");
	await expect(history.getByRole("link", { name: new RegExp(`^${held}`) })).toHaveCount(0);
	await expect(lacking).toBeVisible();
});
