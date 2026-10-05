import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test("two copies of a book are merged into one, with the files of both and the place read", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Merge ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const ids: string[] = [];
	for (const title of [`Kept ${stamp}`, `Merged ${stamp}`]) {
		const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
			multipart: {
				file: {
					name: `${title}.epub`,
					mimeType: "application/epub+zip",
					buffer: makeEpub(title, 2, `${title} tag`, false, { 1: `Only in ${stamp}.` }),
				},
			},
		});
		ids.push((await uploaded.json()).bookId);
	}
	const [kept, merged] = ids as [string, string];
	// A place read in the copy that goes.
	const saved = await page.request.put(`/api/v1/books/${merged}/progress/ebook`, {
		data: { locator: "epubcfi(/6/4)", fraction: 0.6, clientId: "e2e" },
	});
	expect(saved.status()).toBe(200);

	const said = `Kept ${stamp} and Merged ${stamp} have the same content, with other details.`;
	const pairs = page.getByRole("region", { name: "Pairs" });
	await expect(async () => {
		await page.goto(`/duplicates?library=${library}`);
		await expect(pairs.getByRole("listitem", { name: said })).toBeVisible({ timeout: 2_000 });
	}).toPass({ timeout: 30_000 });
	const pair = pairs.getByRole("listitem", { name: said });
	await pair.getByRole("button", { name: "Merge…" }).click();
	const form = pair.getByRole("form", { name: "Merge the two books" });
	await form.getByRole("radio", { name: `Kept ${stamp}, files: 1` }).check();
	await form.getByRole("radiogroup", { name: "Title" }).getByRole("radio", { name: `Merged ${stamp}` }).check();
	await form.getByRole("button", { name: `Merge into Kept ${stamp}` }).click();
	await expect(pairs.getByRole("listitem", { name: said })).toBeHidden();

	// The merged book's address leads to the one that stays, which has both files.
	await page.goto(`/books/${merged}`);
	await expect(page).toHaveURL(new RegExp(`/books/${kept}$`));
	await expect(page.getByRole("heading", { name: `Merged ${stamp}`, level: 1 })).toBeVisible();
	await expect(page.getByRole("link", { name: /^Read .*\.epub$/ })).toHaveCount(2);
	const progress = await (await page.request.get(`/api/v1/books/${kept}/progress`)).json();
	expect(progress.ebook.fraction).toBe(0.6);

	expect((await page.request.delete(`/api/v1/libraries/${library}`)).status()).toBe(204);
});
