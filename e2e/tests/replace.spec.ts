import { type Locator, type Page, expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn, gotoSignedIn } from "../fixtures/app";
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
		await gotoSignedIn(page, `/duplicates?library=${library}`);
		await expect(pairs.getByRole("listitem", { name: said })).toBeVisible({
			timeout: 2_000,
		});
	}).toPass({ timeout: 30_000 });
	const pair = pairs.getByRole("listitem", { name: said });
	await pair
		.getByRole("button", { name: `Keep this one: Better ${stamp} (EPUB)` })
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

/**
 * Adds one of the folders of e2e/fixtures/twins as an external library and
 * waits for the pair of its two books; a pair with a book of another
 * library, such as another browser project's, offers no "Keep this one".
 */
async function twins(
	page: Page,
	folder: string,
	writable: boolean,
): Promise<{ library: string; pair: Locator }> {
	const { libraries } = await (await page.request.get("/api/v1/libraries")).json();
	for (const old of libraries as { id: string; rootPath?: string }[]) {
		if (old.rootPath === folder) {
			await page.request.delete(`/api/v1/libraries/${old.id}`);
		}
	}
	const created = await page.request.post("/api/v1/libraries", {
		data: {
			name: `Twins ${folder} ${Date.now()}`,
			mode: "external",
			rootPath: folder,
			visibility: "private",
			writable,
		},
	});
	expect(created.status()).toBe(201);
	const library = (await created.json()).id as string;
	const pair = page
		.getByRole("region", { name: "Pairs" })
		.getByRole("listitem", {
			name: "Lady Susan and Lady Susan are the same file.",
		})
		.filter({ has: page.getByRole("button", { name: /^Keep this one/ }) });
	await expect(async () => {
		await gotoSignedIn(page, `/duplicates?library=${library}`);
		await expect(pair).toBeVisible({ timeout: 2_000 });
	}).toPass({ timeout: 45_000 });
	return { library, pair };
}

test("keeping one of two books whose library's trash cannot be written says why, at the button", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	await openSignedIn(page);
	const folder = `/fixtures/shelf-${testInfo.project.name}`;
	const { library, pair } = await twins(page, folder, true);

	const card = pair.getByRole("article").first();
	const keep = card.getByRole("button", { name: /^Keep this one/ });
	await keep.click();
	await expect(card.getByRole("alert")).toHaveText(
		`GOtome cannot write to ${folder}/.trash now (permission denied). If its mount was made read-only, mount it writable again; otherwise the app's user 10001 needs write access to it. The Libraries page says more.`,
	);
	await expect(keep).toBeEnabled();
	await expect(pair).toBeVisible();
	const after = await (
		await page.request.get(`/api/v1/libraries/${library}`)
	).json();
	expect(after.unwritableReason).toBe(`permission denied (${folder}/.trash)`);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("keeping one of two books of a read-only library merges them, files and all", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	await openSignedIn(page);
	const { library, pair } = await twins(
		page,
		`/fixtures/twins-${testInfo.project.name}`,
		false,
	);

	const card = pair.getByRole("article").first();
	const keep = card.getByRole("button", { name: /^Keep this one/ });
	await expect(keep).toHaveAccessibleDescription(
		/^GOtome does not move files in this library/,
	);
	await keep.click();
	await expect(pair).toBeHidden();
	const books = await (
		await page.request.get(`/api/v1/books?library=${library}`)
	).json();
	expect(books.books).toHaveLength(1);
	await page.goto(`/books/${books.books[0].id}`);
	await expect(
		page.getByRole("region", { name: "Files" }).getByRole("link", {
			name: "Download Lady Susan.epub",
		}),
	).toHaveCount(2);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
