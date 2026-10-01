import { expect, test } from "@playwright/test";
import { chooseLibrary, openSignedIn } from "../fixtures/app";

test("the books are narrowed by a filter, which the address keeps", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const library = `Filters ${stamp}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: library, mode: "managed", visibility: "shared" },
	});
	expect(created.status()).toBe(201);
	const { id } = await created.json();
	// Two books of their own, told apart by their format.
	for (const name of [`Paper ${stamp}.pdf`, `Kindle ${stamp}.mobi`]) {
		const uploaded = await page.request.post(
			`/api/v1/libraries/${id}/uploads`,
			{
				multipart: {
					file: {
						name,
						mimeType: "application/octet-stream",
						buffer: Buffer.from(`${name}\n`),
					},
				},
			},
		);
		expect(uploaded.status()).toBe(200);
	}

	await page.goto("/");
	await chooseLibrary(page, library);
	const books = page.getByRole("region", { name: "Books" });
	await expect(books.getByRole("link")).toHaveCount(2);

	// On a phone the filters are behind a button.
	const toggle = page.getByRole("button", { name: "Filters" });
	if (await toggle.isVisible()) {
		await toggle.click();
	}
	const filters = page.getByRole("complementary", { name: "Filters" });
	await filters.getByRole("checkbox", { name: "MOBI (1)" }).check();
	await expect(books.getByRole("link")).toHaveCount(1);
	await expect(
		books.getByRole("link", { name: new RegExp(`^Kindle ${stamp}`) }),
	).toBeVisible();

	await page.reload();
	await expect(books.getByRole("link")).toHaveCount(1);
	await expect(
		books.getByRole("link", { name: new RegExp(`^Kindle ${stamp}`) }),
	).toBeVisible();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
