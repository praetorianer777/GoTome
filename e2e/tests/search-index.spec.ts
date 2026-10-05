import { type Page, expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

/** A word of letters alone that no other test's books hold. */
function uniqueWord(project: string): string {
	return `zr${project}${Date.now().toString(36)}`
		.toLowerCase()
		.replace(/[^a-z0-9]/g, "")
		.replace(/\d/g, (d) => "abcdefghij"[Number(d)] ?? "");
}

/** The titles the full-text search finds for the word. */
async function found(page: Page, word: string): Promise<string[]> {
	const answer = await page.request.get(`/api/v1/search?q=${word}`);
	expect(answer.status()).toBe(200);
	const { books } = (await answer.json()) as {
		books: { book: { title: string } }[];
	};
	return books.map((b) => b.book.title);
}

test("the text is read again and the index rebuilt, and search finds the book throughout", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const word = uniqueWord(testInfo.project.name);
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Index ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: {
					name: `Lantern ${stamp}.epub`,
					mimeType: "application/epub+zip",
					buffer: makeEpub(`Lantern ${stamp}`, 2, stamp, false, {
						1: `The ${word} lit the lantern at the end of the pier.`,
					}),
				},
			},
		},
	);
	const { bookId } = await uploaded.json();
	await expect
		.poll(() => found(page, word), { timeout: 30_000 })
		.toEqual([`Lantern ${stamp}`]);

	await page.goto(`/books/${bookId}`);
	await page
		.getByRole("button", { name: "Read the text again for search" })
		.click();
	await expect(
		page.getByText(/^Books whose text is read again in the background: 1\./),
	).toBeVisible();
	expect(await found(page, word)).toEqual([`Lantern ${stamp}`]);

	await page.goto("/jobs");
	const panel = page.getByRole("region", { name: "Search index" });
	await expect(
		panel.getByText(/^Books whose text search knows: \d+ of \d+\.$/),
	).toBeVisible();
	await panel.getByRole("button", { name: "Rebuild the index" }).click();
	await expect(
		panel.getByText(/^(The index is built again|A rebuild is already)/),
	).toBeVisible();
	expect(await found(page, word)).toEqual([`Lantern ${stamp}`]);
	// The panel says so while the rebuild runs, and stops once it is done.
	await expect(panel.getByText(/^The index is being built again/)).toBeHidden({
		timeout: 60_000,
	});
	expect(await found(page, word)).toEqual([`Lantern ${stamp}`]);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
