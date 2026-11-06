import { expect, type Page, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeShortEpub } from "../fixtures/ebooks";
import {
	deleteLibrary,
	editBook,
	newLibrary,
	uploadEpub,
} from "../fixtures/library";

// The stack embeds with vectors made from the words of a text
// (GOTOME_TEST_EMBEDDINGS): books that share words are near, which is all
// these tests need of a model.

async function suggested(
	page: Page,
	library: string,
	book: string,
): Promise<string[]> {
	const answer = await (
		await page.request.get(
			`/api/v1/genres/suggestions?library=${library}&limit=100`,
		)
	).json();
	return answer.suggestions
		.filter((s: { book: { id: string } }) => s.book.id === book)
		.map((s: { genre: string }) => s.genre);
}

test("a book's page shows the books most like it, whose genres are suggested for it and reviewed", async ({
	page,
}, testInfo) => {
	test.setTimeout(240_000);
	const stamp = `${testInfo.project.name}${Date.now()}`;
	const text = `The keeper of the Moorland light ${stamp} walks the cliffs at dusk and counts the ships.`;
	await openSignedIn(page);
	const library = await newLibrary(page, `Similar ${stamp}`);
	const books: Record<string, string> = {};
	const tagged = ["one", "two", "three"].map((n) => `Moorland ${stamp} ${n}`);
	const open = ["four", "five"].map((n) => `Moorland ${stamp} ${n}`);
	for (const title of [...tagged, ...open]) {
		books[title] = await uploadEpub(
			page,
			library,
			title,
			makeShortEpub(title, `similar ${title}`, `${text} ${title}.`),
		);
	}
	for (const title of tagged) {
		await editBook(page, books[title] as string, { tags: ["Fantasy"] });
	}

	const [accepted, rejected] = open as [string, string];
	for (const title of open) {
		await page.goto(`/books/${books[title]}`);
		const similar = page.getByRole("region", { name: "Similar books" });
		// One worker embeds and suggests genres for the whole stack, and may
		// be busy with the other projects' books for a while.
		await expect(async () => {
			await page.reload();
			for (const other of tagged) {
				await expect(similar.getByRole("link", { name: other })).toBeVisible({
					timeout: 1_000,
				});
			}
		}).toPass({ timeout: 120_000 });
	}
	await page
		.getByRole("region", { name: "Similar books" })
		.getByRole("link", { name: tagged[0] as string })
		.click();
	await expect(
		page.getByRole("heading", { name: tagged[0], level: 1 }),
	).toBeVisible();

	// Neighbours suggest the genres of the books most like a book. The
	// setting stays: the projects share the stack, and a change of it asks
	// for a pass, which is what each of them needs.
	await expect(async () => {
		const set = await page.request.patch("/api/v1/settings", {
			data: { values: { "genres.suggest": "neighbours" } },
		});
		expect(set.status()).toBe(200);
		for (const title of open) {
			expect(await suggested(page, library, books[title] as string)).toEqual([
				"fantasy",
			]);
		}
	}).toPass({ timeout: 90_000, intervals: [2_000] });

	await page.goto("/genres");
	const suggestion = (title: string) =>
		page.getByRole("listitem").filter({
			has: page.getByRole("link", { name: title, exact: true }),
		});
	await expect(suggestion(accepted)).toContainText(
		"Fantasy from the books most like it",
	);
	await suggestion(accepted)
		.getByRole("button", { name: "Accept Fantasy" })
		.click();
	await expect(suggestion(accepted)).toHaveCount(0);
	await suggestion(rejected).getByRole("button", { name: "Reject" }).click();
	await expect(suggestion(rejected)).toHaveCount(0);
	expect(await suggested(page, library, books[accepted] as string)).toEqual([]);
	expect(await suggested(page, library, books[rejected] as string)).toEqual([]);

	// An accepted genre is found by the library's filter, a rejected one not.
	const found = await (
		await page.request.get(
			`/api/v1/books?library=${library}&filter=${encodeURIComponent(
				JSON.stringify({
					field: "genre",
					op: "in",
					values: ["fantasy"],
				}),
			)}`,
		)
	).json();
	const titles = found.books.map((b: { title: string }) => b.title);
	expect(titles).toContain(accepted);
	expect(titles).not.toContain(rejected);

	await deleteLibrary(page, library);
});
