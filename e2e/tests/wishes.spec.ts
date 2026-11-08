import { expect, test } from "@playwright/test";
import { chooseLibraryIn, openSignedIn } from "../fixtures/app";
import { deleteLibrary, newLibrary, projectWord } from "../fixtures/library";

test("a book the library lacks is looked up and wished for", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const word = projectWord(testInfo);
	const stamp = `${word} ${Date.now()}`;
	// A record of fixtures/metadata.json, which the stack looks books up in.
	const title = `Glass Orchard ${word}`;
	await openSignedIn(page);
	const name = `Wishes ${stamp}`;
	const library = await newLibrary(page, name);

	await page.goto("/wishlist");
	const find = page.getByRole("region", { name: "Wish for a book" });
	await find.getByRole("textbox", { name: "Title" }).fill(title);
	await find.getByRole("button", { name: "Look it up" }).click();
	const wish = find.getByRole("button", { name: `Wish for ${title}` });
	await expect(wish).toBeVisible();
	await chooseLibraryIn(page, find.getByRole("combobox", { name: "Library it is to go into" }), name);
	await wish.click();

	await expect(
		page.getByRole("heading", { name: title, level: 1 }),
	).toBeVisible();
	await expect(page.getByRole("link", { name: "Ida Brandt" })).toBeVisible();
	const wished = new URL(page.url()).pathname;
	await page.goto("/wishlist");
	// Earlier runs on the same stack may have left a wish of the same title.
	await expect(
		page
			.getByRole("region", { name: "Not in the library yet" })
			.locator(`a[href="${wished}"]`),
	).toBeVisible();

	await deleteLibrary(page, library);
});
