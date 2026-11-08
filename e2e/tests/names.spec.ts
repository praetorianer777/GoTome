import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import {
	deleteLibrary,
	editBook,
	newLibrary,
	projectWord,
	uploadEpub,
} from "../fixtures/library";

test("an author and a series have pages, lists and quick-search hits, and a missing volume is wished for", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const word = projectWord(testInfo);
	const stamp = `${word}${Date.now()}`;
	// Early in the alphabet, so the list of authors shows it on its first page.
	const author = `Aabel, Ada ${stamp}`;
	// The name fixtures/metadata.json lists the second volume of.
	const series = `Lantern Cycle ${word}`;
	const first = `The First Lantern ${stamp}`;
	const third = `The Third Lantern ${stamp}`;
	await openSignedIn(page);
	const library = await newLibrary(page, `Names ${stamp}`);
	const books: Record<string, string> = {};
	for (const [title, index] of [
		[first, 1],
		[third, 3],
	] as const) {
		books[title] = await uploadEpub(
			page,
			library,
			title,
			makeEpub(title, 1, `names ${title}`),
		);
		await editBook(page, books[title] as string, {
			title,
			contributors: [{ name: author, role: "author" }],
			series: { name: series, index },
		});
	}

	await page.goto(`/books/${books[first]}`);
	await page.getByRole("link", { name: author, exact: true }).click();
	await expect(
		page.getByRole("heading", { name: author, level: 1 }),
	).toBeVisible();
	await expect(page.getByRole("link", { name: first, exact: true })).toBeVisible();
	await expect(page.getByRole("link", { name: third, exact: true })).toBeVisible();

	await page.goto(`/books/${books[third]}`);
	await page.getByRole("link", { name: series, exact: true }).click();
	await expect(
		page.getByRole("heading", { name: series, level: 1 }),
	).toBeVisible();
	await expect(page.getByText("Book 2 is missing")).toBeVisible();
	await expect(page.getByRole("link", { name: first, exact: true })).toBeVisible();

	// Following the series asks the sources for its books, which lists the
	// one the library lacks.
	await page.getByRole("button", { name: `Series ${series}`, exact: true }).click();
	await expect(
		page.getByRole("button", { name: `✓ Series ${series}` }),
	).toHaveAttribute("aria-pressed", "true");
	const wish = page.getByRole("button", {
		name: `Add The Second Lantern ${word} to the wishlist`,
	});
	await expect(async () => {
		await page.reload();
		await expect(wish).toBeVisible({ timeout: 1_000 });
	}).toPass({ timeout: 40_000 });
	await wish.click();
	await expect(page.getByText("Wished for", { exact: true })).toBeVisible();
	await page.goto("/wishlist");
	await expect(
		page
			.getByRole("region", { name: "Not in the library yet" })
			.getByRole("link", { name: new RegExp(`The Second Lantern ${word}`) }),
	).toBeVisible();

	const box = page.getByRole("combobox", { name: "Search books" });
	await box.fill(`Aabel Ada ${stamp}`);
	await page
		.getByRole("listbox", { name: "Books found" })
		.getByRole("option", { name: new RegExp(`^${author}\\s*Author · 2 books`) })
		.click();
	await expect(
		page.getByRole("heading", { name: author, level: 1 }),
	).toBeVisible();
	await box.fill(series);
	await page
		.getByRole("listbox", { name: "Books found" })
		.getByRole("option", { name: new RegExp(`^${series}\\s*Series · `) })
		.click();
	await expect(
		page.getByRole("heading", { name: series, level: 1 }),
	).toBeVisible();

	await page.goto("/authors");
	const authors = page.getByRole("region", { name: "A", exact: true });
	await expect(
		authors.getByRole("listitem").filter({ hasText: author }),
	).toContainText("2 books");
	await page.goto("/series");
	await page
		.getByRole("region", { name: "L", exact: true })
		.getByRole("link", { name: series, exact: true })
		.click();
	await expect(
		page.getByRole("heading", { name: series, level: 1 }),
	).toBeVisible();

	await page.getByRole("button", { name: `✓ Series ${series}` }).click();
	await expect(
		page.getByRole("button", { name: `Series ${series}`, exact: true }),
	).toHaveAttribute("aria-pressed", "false");
	await deleteLibrary(page, library);
});
