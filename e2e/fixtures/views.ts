import { expect, type Page } from "@playwright/test";
import { libraryOfBooks } from "./app";
import { makeEpub } from "./ebooks";
import { editBook, newLibrary, uploadEpub } from "./library";

/** The main views of the app, by address and the heading each opens with. */
export const VIEWS: { path: string; heading: string }[] = [
	{ path: "/", heading: "Library" },
	{ path: "/search", heading: "Search the text" },
	{ path: "/duplicates", heading: "Duplicates" },
	{ path: "/cleanup", heading: "Clean up" },
	{ path: "/curation", heading: "Changes" },
	{ path: "/collections", heading: "Collections" },
	{ path: "/wishlist", heading: "Wishlist" },
	{ path: "/recommendations", heading: "For you" },
	{ path: "/discover", heading: "Discover" },
	{ path: "/releases", heading: "New books" },
	{ path: "/stats", heading: "Your reading" },
	{ path: "/highlights", heading: "Highlights" },
	{ path: "/overview", heading: "Overview" },
	{ path: "/jobs", heading: "Background jobs" },
	{ path: "/trash", heading: "Trash" },
	{ path: "/profile", heading: "Your account" },
	{ path: "/admin/libraries", heading: "Libraries" },
	{ path: "/admin/calibre", heading: "Import from Calibre" },
	{ path: "/admin/users", heading: "Users" },
	{ path: "/admin/webhooks", heading: "Webhooks" },
	{ path: "/admin/settings", heading: "Settings" },
];

export interface Seen {
	label: string;
	path: string;
	heading: string | RegExp;
}

/** The views that need a book or an author, found in a library of their own. */
export async function seeded(page: Page, stamp: string) {
	const { id, books } = await libraryOfBooks(page, `Layout views ${stamp}`, [
		`Layout One ${stamp}`,
		`Layout Two ${stamp}`,
		`Layout Three ${stamp}`,
	]);
	// The longest card the library shows: an author and a series too long for
	// the column, one word with no break to cut at, and the viewer's
	// standing with the book.
	const long = `Bartholomew-Featherstonehaugh ${stamp.replaceAll(" ", "")} Wolfeschlegelsteinhausenbergerdorff`;
	await editBook(page, books[`Layout Three ${stamp}`] as string, {
		contributors: [{ name: long, role: "author" }],
		series: { name: `The Unabridged Chronicles of ${long}`, index: 12 },
	});
	const reading = await page.request.put(
		`/api/v1/books/${books[`Layout Three ${stamp}`]}/reading`,
		{ data: { status: "completed", rating: 5 } },
	);
	expect(reading.status()).toBe(200);
	// A review too long for the column, shared, so that the book's page and a
	// person's page list it.
	const me = (await (await page.request.get("/api/v1/auth/me")).json()) as {
		username: string;
	};
	expect(
		(await page.request.put("/api/v1/me/sharing", { data: { shareReviews: true } })).ok(),
	).toBe(true);
	expect(
		(
			await page.request.put(`/api/v1/books/${books[`Layout Three ${stamp}`]}/review`, {
				data: { body: `${long} ${long}\n\nA second paragraph of the review.`, private: false },
			})
		).status(),
	).toBe(204);
	// A highlight whose passage and note are one long word each, on the book
	// page and in the list of every highlight.
	const unbroken = `Floccinaucinihilipilification${stamp.replaceAll(" ", "")}`.repeat(4);
	const highlighted = await page.request.post(
		`/api/v1/books/${books[`Layout One ${stamp}`]}/annotations`,
		{
			data: {
				kind: "cfi",
				locator: "epubcfi(/6/4!/4/2/1:0)",
				passage: unbroken,
				note: unbroken,
				chapter: unbroken,
			},
		},
	);
	expect(highlighted.status()).toBe(201);
	const epubLibrary = await newLibrary(page, `Layout reader ${stamp}`);
	const epub = await uploadEpub(
		page,
		epubLibrary,
		`Layout Reader ${stamp}`,
		makeEpub(`Layout Reader ${stamp}`, 3, stamp),
	);
	const detail = (await (await page.request.get(`/api/v1/books/${epub}`)).json()) as {
		files: { id: string }[];
	};
	const views: Seen[] = [
		{
			label: "a book",
			path: `/books/${books[`Layout One ${stamp}`]}`,
			heading: `Layout One ${stamp}`,
		},
		{ label: "authors", path: "/authors", heading: /Authors/ },
		{ label: "series", path: "/series", heading: /Series/ },
		{
			label: "the ebook reader",
			path: `/books/${epub}/read/${detail.files[0]?.id}`,
			heading: `Layout Reader ${stamp}`,
		},
	];
	views.push({
		label: "a person's reviews",
		path: `/people/${me.username}`,
		heading: `Reviews by ${me.username}`,
	});
	return {
		views,
		cleanup: async () => {
			await page.request.put("/api/v1/me/sharing", { data: { shareReviews: false } });
			for (const library of [id, epubLibrary]) {
				await page.request.delete(`/api/v1/libraries/${library}`, { timeout: 30_000 });
			}
		},
	};
}

export async function open(page: Page, view: { path: string; heading: string | RegExp }) {
	await page.goto(view.path);
	await expect(
		page.getByRole("heading", { name: view.heading, level: 1 }).first(),
	).toBeVisible();
	await page.waitForLoadState("networkidle");
}

