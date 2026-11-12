import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";

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

test("Enter in the header's search lists every book found, as the library does", async ({
	page,
}) => {
	// Letters drawn at random share no trigram worth counting with the
	// titles other projects add at the same time.
	const word = Array.from({ length: 10 }, () =>
		String.fromCharCode(97 + Math.floor(Math.random() * 26)),
	).join("");
	const titles = ["Eins", "Zwei", "Drei"].map((n) => `${word} ${n}`);
	await openSignedIn(page);
	const { id } = await libraryOfBooks(page, `Every match ${word}`, titles);
	// The page opened before the library was made; it learns of it anew.
	await page.goto("/");

	const box = page.getByRole("combobox", { name: "Search books" });
	await box.fill(word);
	await expect(
		page
			.getByRole("listbox", { name: "Books found" })
			.getByRole("option", { name: `Every book like “${word}”` }),
	).toBeVisible();
	await box.press("Enter");

	await expect(
		page.getByRole("heading", { name: `Books like “${word}”`, level: 1 }),
	).toBeVisible();
	await expect(page).toHaveURL(new RegExp(`[?&]q=${word}`));
	const found = page
		.getByRole("region", { name: "Books" })
		.getByRole("link", { name: new RegExp(word) });
	await expect(found).toHaveCount(3);

	await page.getByRole("button", { name: "List", exact: true }).click();
	await page.reload();
	await expect(found).toHaveCount(3);
	await page.getByRole("button", { name: "Show the whole library" }).click();
	await expect(
		page.getByRole("heading", { name: "Library", level: 1 }),
	).toBeVisible();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
