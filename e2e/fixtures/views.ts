import { expect, type Page } from "@playwright/test";
import { libraryOfBooks } from "./app";
import { makeEpub } from "./ebooks";
import { newLibrary, uploadEpub } from "./library";

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
	{ path: "/releases", heading: "New books" },
	{ path: "/stats", heading: "Your reading" },
	{ path: "/overview", heading: "Overview" },
	{ path: "/jobs", heading: "Background jobs" },
	{ path: "/trash", heading: "Trash" },
	{ path: "/profile", heading: "Your account" },
	{ path: "/admin/libraries", heading: "Libraries" },
	{ path: "/admin/users", heading: "Users" },
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
	return {
		views,
		cleanup: async () => {
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

