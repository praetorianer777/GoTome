import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

// Laying a book out keeps a loaded WebKit busy enough in CI that its
// controls take more than the usual five seconds to settle for a click.
test.use({ actionTimeout: 15_000 });

test("a copy is replaced by the one kept, the place read carries over, and the trash brings the file back", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Replace ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const ids: string[] = [];
	for (const title of [`Better ${stamp}`, `Worse ${stamp}`]) {
		const uploaded = await page.request.post(
			`/api/v1/libraries/${library}/uploads`,
			{
				multipart: {
					file: {
						name: `${title}.epub`,
						mimeType: "application/epub+zip",
						buffer: makeEpub(title, 3, `${title} tag`, false, {
							1: `Only in ${stamp}.`,
						}),
					},
				},
			},
		);
		ids.push((await uploaded.json()).bookId);
	}
	const [kept, gone] = ids as [string, string];
	const saved = await page.request.put(`/api/v1/books/${gone}/progress/ebook`, {
		data: { locator: "epubcfi(/6/4)", fraction: 0.6, clientId: "e2e" },
	});
	expect(saved.status()).toBe(200);

	const said = `Better ${stamp} and Worse ${stamp} have the same content, with other details.`;
	const pairs = page.getByRole("region", { name: "Pairs" });
	await expect(async () => {
		await page.goto(`/duplicates?library=${library}`);
		await expect(pairs.getByRole("listitem", { name: said })).toBeVisible({
			timeout: 2_000,
		});
	}).toPass({ timeout: 30_000 });
	const pair = pairs.getByRole("listitem", { name: said });
	await pair.getByRole("button", { name: "Replace…" }).click();
	await pair
		.getByRole("button", {
			name: `Keep Better ${stamp} (EPUB), trash Worse ${stamp}`,
		})
		.click();
	await expect(pairs.getByRole("listitem", { name: said })).toBeHidden();

	// The place read in the copy that went is kept by how far it was, and
	// the reader opens there in the copy kept.
	const progress = await (
		await page.request.get(`/api/v1/books/${kept}/progress`)
	).json();
	expect(progress.ebook.locator).toBe("fraction:0.6");
	await page.goto(`/books/${kept}`);
	await page.getByRole("link", { name: `Read Better ${stamp}.epub` }).click();
	await expect(page.getByText(/^\d+%$/)).toHaveText(/^(4|5|6|7)\d%$/, {
		timeout: 20_000,
	});

	await page.goto("/trash");
	const trashed = page.getByRole("listitem", { name: `Worse ${stamp}.epub` });
	await expect(
		trashed.getByRole("link", { name: `Better ${stamp}` }),
	).toBeVisible();
	await trashed.getByRole("button", { name: "Restore" }).click();
	await expect(trashed).toBeHidden();
	await page.goto(`/books/${kept}`);
	await expect(page.getByRole("link", { name: /^Read .*\.epub$/ })).toHaveCount(
		2,
	);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
