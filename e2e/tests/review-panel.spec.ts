import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import { bookFrame, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { newLibrary, uploadEpub } from "../fixtures/library";

// The queues are answered here, since no stack holds a doubtful match or a
// suggested genre on demand; the book, its panel and its reader are real.
async function queued(page: Page, stamp: string) {
	await openSignedIn(page);
	const title = `Panel Book ${stamp}`;
	const library = await newLibrary(page, `Panel library ${stamp}`);
	const id = await uploadEpub(page, library, title, makeEpub(title, 3, stamp));
	const book = await (await page.request.get(`/api/v1/books/${id}`)).json();
	const summary = {
		id: book.id,
		libraryId: book.libraryId,
		title: book.title,
		authors: [],
		authorKeys: [],
		formats: ["epub"],
		addedAt: book.addedAt,
		status: "unread",
		placeholder: false,
		filesMissing: false,
		ereaderFindings: [],
	};
	await page.route(/\/api\/v1\/matches\?/, (route) =>
		route.fulfill({
			json: {
				total: 1,
				books: [
					{
						book,
						matches: [
							{
								matchId: "00000000-0000-7000-8000-000000000001",
								provider: "openlibrary",
								id: "OL1",
								score: 0.6,
								title,
								subtitle: "A different subtitle",
								publisher: "Elsewhere Press",
								contributors: [],
								tags: [],
								identifiers: [],
								fields: {},
							},
						],
					},
				],
			},
		}),
	);
	await page.route(/\/api\/v1\/genres\/suggestions/, (route) =>
		route.fulfill({
			json: {
				waiting: [{ genre: "crime", waiting: 1 }],
				suggestions: [{ book: summary, genre: "crime", method: "vector", score: 0.1 }],
			},
		}),
	);
	return { id, title };
}

async function shown(page: Page, title: string) {
	const wide = (page.viewportSize()?.width ?? 0) >= 1024;
	return wide
		? page.getByRole("complementary", { name: `${title}, details` })
		: page.getByRole("dialog", { name: title });
}

async function looksInsideWithoutSaving(page: Page, title: string) {
	const saves: string[] = [];
	page.on("request", (r) => {
		if (r.method() === "PUT" && r.url().includes("/progress/")) saves.push(r.url());
	});
	const panel = await shown(page, title);
	await panel.getByRole("button", { name: "Look inside" }).click();
	await expect(page.getByText("Preview: your place is not saved")).toBeVisible();
	await bookFrame(page, "Chapter 1");
	await page.getByRole("button", { name: "Page right" }).click();
	await page.waitForTimeout(1500);
	expect(saves).toEqual([]);
	await page.keyboard.press("Escape");
	await expect(page.getByText("Preview: your place is not saved")).toHaveCount(0);
}

test("the review shows a book beside the list and looks inside it without saving a place", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name}${Date.now()}`;
	const { title } = await queued(page, stamp);
	await page.goto("/review");
	await page.getByRole("button", { name: title, exact: true }).click();
	await expect(page).toHaveURL(/book=/);
	await expect(await shown(page, title)).toBeVisible();
	await expect(
		(await shown(page, title)).getByRole("link", { name: "Open the book page" }),
	).toBeVisible();

	await looksInsideWithoutSaving(page, title);

	await page.goBack();
	await expect(page).not.toHaveURL(/book=/);
	await expect(await shown(page, title)).toHaveCount(0);
});

test("the genre review shows the book and its suggestion in the panel", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name}${Date.now()}`;
	const { title } = await queued(page, stamp);
	await page.goto("/genres");
	await page.getByRole("button", { name: title, exact: true }).click();
	await expect(page).toHaveURL(/book=/);
	const panel = await shown(page, title);
	await expect(panel.getByRole("button", { name: "Accept Crime & mystery" })).toBeVisible();

	await panel.getByRole("button", { name: "Close" }).click();
	await expect(page).not.toHaveURL(/book=/);
	await expect(page.getByRole("button", { name: title, exact: true })).toBeFocused();
});
