import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test("an author is followed from a book's page, listed on the new books page, and let go again", async ({
	page,
}, testInfo) => {
	// The browser projects share one account: each follows an author of its own.
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const author = `Writer, Wanda ${stamp}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Follow ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: {
					name: `Followed ${stamp}.epub`,
					mimeType: "application/epub+zip",
					buffer: makeEpub(`Followed ${stamp}`, 1, `tag ${stamp}`, false),
				},
			},
		},
	);
	const book = (await uploaded.json()).bookId as string;
	const edited = await page.request.patch(`/api/v1/books/${book}`, {
		data: { contributors: [{ name: author, role: "author" }] },
	});
	expect(edited.status()).toBe(200);

	await page.goto(`/books/${book}`);
	await page.getByRole("button", { name: author, exact: true }).click();
	await expect(
		page.getByRole("button", { name: `✓ ${author}` }),
	).toHaveAttribute("aria-pressed", "true");

	await page.getByRole("link", { name: "New books" }).click();
	const following = page.getByRole("region", { name: "You follow" });
	await expect(following.getByText(author, { exact: true })).toBeVisible();
	await following
		.getByRole("button", { name: `Stop following ${author}` })
		.click();
	await expect(following.getByText(author, { exact: true })).toBeHidden();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
