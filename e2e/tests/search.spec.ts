import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

test("a misspelt title is found from the header and opened with the keyboard", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const title = `Quicksearch ${stamp}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Search ${stamp}`, mode: "managed", visibility: "shared" },
	});
	expect(created.status()).toBe(201);
	const { id } = await created.json();
	const uploaded = await page.request.post(`/api/v1/libraries/${id}/uploads`, {
		multipart: {
			file: {
				name: `${title}.pdf`,
				mimeType: "application/pdf",
				buffer: Buffer.from(`%PDF-1.4\n% ${stamp}\n`),
			},
		},
	});
	expect(uploaded.status()).toBe(200);

	const box = page.getByRole("combobox", { name: "Search books" });
	await box.fill(`Quiksearch ${stamp}`);
	const hit = page
		.getByRole("listbox", { name: "Books found" })
		.getByRole("option", { name: new RegExp(`^${title}`) });
	await expect(hit).toBeVisible();
	await box.press("ArrowDown");
	await box.press("Enter");
	await expect(
		page.getByRole("heading", { name: title, level: 1 }),
	).toBeVisible();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
