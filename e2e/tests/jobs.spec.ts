import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

test("a file that cannot be read shows on the jobs page and its book, and is read again", async ({ page }, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Jobs ${stamp}`, mode: "managed", visibility: "shared" },
	});
	const { id } = await created.json();
	// Not a zip, so not an EPUB: reading it fails for good.
	const uploaded = await page.request.post(`/api/v1/libraries/${id}/uploads`, {
		multipart: { file: { name: `Broken ${stamp}.epub`, mimeType: "application/epub+zip", buffer: Buffer.from(`not a zip ${stamp}`) } },
	});
	expect(uploaded.status()).toBe(200);
	const { bookId } = await uploaded.json();

	await page.goto(`/books/${bookId}`);
	await expect(page.getByText(/^Could not be read: /)).toBeVisible({ timeout: 15_000 });
	await page.getByRole("button", { name: `Read Broken ${stamp}.epub again` }).click();
	await expect(page.getByText(/^Could not be read: /)).toBeVisible({ timeout: 15_000 });

	await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Jobs" }).click();
	await expect(page.getByRole("heading", { name: "Background jobs", level: 1 })).toBeVisible();
	await expect(page.getByRole("link", { name: new RegExp(`^Read .*Broken ${stamp}\\.epub$`) }).first()).toBeVisible();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(204);
});
