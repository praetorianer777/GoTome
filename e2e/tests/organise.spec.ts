import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { editBook, newLibrary, uploadEpub } from "../fixtures/library";

test("a library's files are previewed, moved by the pattern, and moved back by undoing the run", async ({
	page,
}, testInfo) => {
	test.setTimeout(150_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const name = `Organise ${stamp}`;
	const title = `Organised ${stamp}`;
	const author = `Organise Author ${stamp}`;
	await openSignedIn(page);
	const library = await newLibrary(page, name);
	const book = await uploadEpub(page, library, title, makeEpub(title, 3, stamp));
	await editBook(page, book, {
		contributors: [{ name: author, role: "author" }],
	});
	const pathOf = async () => {
		const detail = await (await page.request.get(`/api/v1/books/${book}`)).json();
		return detail.files[0].relPath as string;
	};
	const first = await pathOf();

	await page.goto("/admin/libraries");
	const row = page.getByRole("listitem", { name });
	await row.getByRole("button", { name: "Preview" }).click();
	const moves = row.getByRole("list", { name: "Moves" });
	await expect(moves.getByText(title, { exact: true })).toBeVisible();
	await expect(moves.getByText(`${author}/${title}.epub`, { exact: false })).toBeVisible();
	expect(await pathOf()).toBe(first);

	await row.getByRole("button", { name: /^Move \d+ files$/ }).click();
	await row.getByRole("link", { name: "Follow the run under Changes" }).click();
	await expect(page.getByRole("heading", { name: "Changes", level: 1 })).toBeVisible();
	await expect
		.poll(pathOf, { timeout: 90_000 })
		.toBe(`${author}/${title}.epub`);

	await expect(page.getByRole("listitem", { name: /^Files organised/ }).first()).toBeVisible();
	await page
		.getByRole("listitem", { name: /^Files organised/ })
		.first()
		.getByRole("button", { name: /^Undo run of/ })
		.click();
	await expect.poll(pathOf, { timeout: 60_000 }).toBe(first);
});
