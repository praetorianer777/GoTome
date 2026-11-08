import { expect, type Page, test } from "@playwright/test";
import { chooseLibrary, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import {
	deleteLibrary,
	newLibrary,
	projectWord,
	uploadEpub,
	waitUntilRead,
} from "../fixtures/library";

// The stack looks books up in the records of fixtures/metadata.json
// (GOTOME_TEST_METADATA), whose titles each name a browser project: a
// book is found by a search when its title holds every word of a record's.

/** Expects a field's box on the comparison to be ticked or not. */
async function ticked(page: Page, field: string, wanted: boolean) {
	await expect(
		page.getByRole("checkbox", { name: `Take ${field}` }),
	).toBeChecked({ checked: wanted });
}

/** Ticks a field's box on the comparison, or takes the tick off. */
async function take(page: Page, field: string, wanted: boolean) {
	const box = page.getByRole("checkbox", { name: `Take ${field}` });
	if ((await box.isChecked()) !== wanted) {
		await box.click();
	}
	await expect(box).toBeChecked({ checked: wanted });
}

test("details found online are compared with the book's, the fields worth taking ticked", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const word = projectWord(testInfo);
	const stamp = `${word} ${Date.now()}`;
	const title = `Quiet Harbour ${stamp}`;
	await openSignedIn(page);
	const library = await newLibrary(page, `Find ${stamp}`);
	const book = await uploadEpub(
		page,
		library,
		title,
		makeEpub(title, 1, `find ${stamp}`),
	);
	await waitUntilRead(page, book);

	await page.goto(`/books/${book}`);
	await page.getByRole("link", { name: "Find details online" }).click();
	await expect(
		page.getByRole("heading", { name: `Find details for ${title}` }),
	).toBeVisible();
	const found = page.getByRole("list", { name: "What the sources found" });
	await found
		.getByRole("button", { name: new RegExp(`^Quiet Harbour ${word}`) })
		.click();
	const compare = page.getByRole("region", { name: "What would change" });
	await expect(compare).toContainText("A harbour town waits out the winter.");
	// The title the file gives stays; what the book lacks is taken.
	await ticked(page, "Title", false);
	await expect(compare).toContainText("Would replace what the file says");
	await ticked(page, "Description", true);
	await ticked(page, "Publisher", true);
	await page.getByRole("button", { name: "Take what is ticked" }).click();

	await expect(page).toHaveURL(new RegExp(`/books/${book}$`));
	await expect(
		page.getByRole("heading", { name: title, level: 1 }),
	).toBeVisible();
	await expect(
		page.getByText("A harbour town waits out the winter."),
	).toBeVisible();
	const detail = await (await page.request.get(`/api/v1/books/${book}`)).json();
	expect(detail.title).toBe(title);
	expect(detail.publisher).toBe("Tidewater Press");

	await deleteLibrary(page, library);
});

/**
 * Brings the review queue to the book of that title. Other projects' books
 * wait in the same queue, in the order their matches were found.
 */
async function reviewBook(page: Page, title: string) {
	const shown = page.getByRole("article").getByRole("heading", { level: 2 });
	await expect(async () => {
		if ((await shown.textContent()) === title) {
			return;
		}
		const next = page.getByRole("button", { name: "Next book" });
		if (await next.isEnabled()) {
			await next.click();
		} else {
			await page.reload();
		}
		expect(await shown.textContent()).toBe(title);
	}).toPass({ timeout: 15_000 });
}

test("details fetched for many books wait for review, where one is taken and one rejected", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const word = projectWord(testInfo);
	const stamp = `${word} ${Date.now()}`;
	const amber = `Amber Road ${stamp}`;
	const fen = `Silver Fen ${stamp}`;
	await openSignedIn(page);
	const name = `Review ${stamp}`;
	const library = await newLibrary(page, name);
	const books: Record<string, string> = {};
	for (const title of [amber, fen]) {
		books[title] = await uploadEpub(
			page,
			library,
			title,
			makeEpub(title, 1, `review ${title}`),
		);
	}
	for (const id of Object.values(books)) {
		await waitUntilRead(page, id);
	}

	await page.goto("/");
	await chooseLibrary(page, name);
	await expect(page.getByRole("link", { name: amber })).toBeVisible();
	await page.getByRole("button", { name: "Select books" }).click();
	await page
		.getByRole("button", { name: "Select every book that matches" })
		.click();
	await page.getByRole("button", { name: "Fetch details" }).click();

	await expect(
		page.getByRole("heading", { name: "Fetching details for many books" }),
	).toBeVisible();
	await expect(page.getByText("2 of 2 books done.")).toBeVisible({
		timeout: 15_000,
	});
	await expect(
		page.getByRole("button", { name: "Matches to review: 2" }),
	).toBeVisible();
	await page.getByRole("link", { name: "Review the matches found" }).click();

	await expect(
		page.getByRole("heading", { name: "Review matches", level: 1 }),
	).toBeVisible();
	await reviewBook(page, amber);
	await expect(
		page.getByRole("button", { name: new RegExp(`^1\\. Amber Road ${word}`) }),
	).toHaveAttribute("aria-pressed", "true");
	await ticked(page, "Title", false);
	await ticked(page, "Description", true);
	await take(page, "Published", false);
	await page.getByRole("button", { name: "Take what is ticked" }).click();
	await expect(
		page.getByRole("heading", { name: amber, level: 2 }),
	).toHaveCount(0, { timeout: 15_000 });

	await reviewBook(page, fen);
	await page.getByRole("button", { name: "Reject" }).click();
	await expect(
		page.getByRole("heading", { name: fen, level: 2 }),
	).toHaveCount(0, { timeout: 15_000 });

	const taken = await (
		await page.request.get(`/api/v1/books/${books[amber]}`)
	).json();
	expect(taken.title).toBe(amber);
	expect(taken.description).toBe("Traders cross the steppe with amber.");
	expect(taken.published ?? "").toBe("");
	const rejected = await (
		await page.request.get(`/api/v1/books/${books[fen]}`)
	).json();
	expect(rejected.description ?? "").toBe("");
	const waiting = await (await page.request.get("/api/v1/matches")).json();
	expect(JSON.stringify(waiting)).not.toContain(books[fen]);

	await deleteLibrary(page, library);
});
