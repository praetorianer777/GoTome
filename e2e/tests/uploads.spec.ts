import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

test("a book is uploaded, found in the library, and not taken twice", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const library = `Uploads ${stamp}`;
	const title = `Upload ${stamp}`;
	// Content of its own: the same bytes anywhere on the stack would be a
	// duplicate, and every browser project runs this test.
	const file = {
		name: `${title}.pdf`,
		mimeType: "application/pdf",
		buffer: Buffer.from(`%PDF-1.4\n% ${stamp}\n`),
	};
	await openSignedIn(page);

	const created = await page.request.post("/api/v1/libraries", {
		data: { name: library, mode: "managed", visibility: "shared" },
	});
	expect(created.status()).toBe(201);
	const { id } = await created.json();

	// Loaded afresh: the app knew the libraries before this one was made.
	await page.goto("/upload");
	await expect(
		page.getByRole("heading", { name: "Add books", level: 1 }),
	).toBeVisible();
	const choice = page.getByRole("combobox", { name: "Into the library" });
	if (await choice.isVisible()) {
		await choice.selectOption({ label: library });
	}
	await page.getByLabel("Choose files").setInputFiles(file);

	const row = page.getByRole("listitem", { name: file.name });
	await expect(row.getByText("Added.")).toBeVisible();

	await page
		.getByLabel("Choose files")
		.setInputFiles({ ...file, name: "the same again.pdf" });
	const again = page.getByRole("listitem", { name: "the same again.pdf" });
	await expect(
		again.getByText(
			`Already in the library as “${title}”, so not added again.`,
		),
	).toBeVisible();

	await row.getByRole("link", { name: "Open the book" }).click();
	await expect(
		page.getByRole("heading", { name: title, level: 1 }),
	).toBeVisible();
	const files = page.getByRole("region", { name: "Files" });
	await expect(
		files.getByRole("link", { name: `Download ${file.name}` }),
	).toBeVisible();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
