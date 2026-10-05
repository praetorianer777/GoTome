import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makePdf } from "../fixtures/pdf";

test("a file moved to the trash is restored to its book, and deleted for good only when confirmed", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const name = `Binned ${stamp}.pdf`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Bin ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: {
					name,
					mimeType: "application/pdf",
					buffer: makePdf(2, 0, stamp),
				},
			},
		},
	);
	const { bookId } = await uploaded.json();

	await page.goto(`/books/${bookId}`);
	await page.getByRole("button", { name: `Move ${name} to the trash` }).click();
	await expect(page.getByRole("link", { name: `Read ${name}` })).toBeHidden();

	await page.goto("/trash");
	const item = page.getByRole("listitem", { name });
	await expect(
		item.getByText(/^Trashed on .* by e2e-admin\. Deleted for good on /),
	).toBeVisible();
	await item.getByRole("button", { name: "Restore" }).click();
	await expect(item).toBeHidden();
	await page.goto(`/books/${bookId}`);
	await expect(page.getByRole("link", { name: `Read ${name}` })).toBeVisible();

	await page.getByRole("button", { name: `Move ${name} to the trash` }).click();
	await page.goto("/trash");
	await item.getByRole("button", { name: "Delete now" }).click();
	await expect(item).toBeVisible();
	await item.getByRole("button", { name: "Delete for good" }).click();
	await expect(item).toBeHidden();
	expect(
		(
			await page.request.post(
				`/api/v1/files/${(await uploaded.json()).fileId}/restore`,
			)
		).status(),
	).toBe(404);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
