import { expect, type Page, test } from "@playwright/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import {
	clippedTextProblems,
	focusProblems,
	overflowProblems,
	primaryButtons,
	smallTargets,
	withTextSpacing,
} from "../fixtures/quality";
import { newLibrary, uploadEpub } from "../fixtures/library";
import { VIEWS } from "../fixtures/views";

// The layout rules of docs/design.md that a browser can measure (#378), on
// every main view in every project: nothing wider than the window at 320 px
// and at 200 % zoom (WCAG 1.4.10, 1.4.4), no text cut off or over other text
// with the text spacing of 1.4.12, one filled primary button, targets of at
// least 24 pixels (2.5.8) and a visible focus.
// Contrast is axe's (accessibility.spec.ts).

const MINIMUM = 24;

interface Seen {
	label: string;
	path: string;
	heading: string | RegExp;
}

/** The views that need a book or an author, found in a library of their own. */
async function seeded(page: Page, stamp: string) {
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

async function open(page: Page, view: { path: string; heading: string | RegExp }) {
	await page.goto(view.path);
	await expect(
		page.getByRole("heading", { name: view.heading, level: 1 }).first(),
	).toBeVisible();
	await page.waitForLoadState("networkidle");
}

test.describe("layout rules", () => {
	test.describe.configure({ timeout: 120_000 });

	test("every main view keeps to them", async ({ page }, testInfo) => {
		const stamp = `${testInfo.project.name} ${Date.now()}`;
		const minimum = MINIMUM;
		await openSignedIn(page);
		const extra = await seeded(page, stamp);
		const problems: string[] = [];
		const views = [
			...VIEWS.map((v) => ({ label: v.path, ...v })),
			...extra.views,
		];
		try {
			for (const view of views) {
				const reader = view.label === "the ebook reader";
				const note = (rule: string, found: string[]) =>
					found.forEach((f) => problems.push(`${view.label}: ${rule}: ${f}`));
				const size = page.viewportSize() ?? { width: 1280, height: 800 };

				await open(page, view);
				const primaries = await primaryButtons(page);
				if (primaries.length > 1) note("more than one primary button", primaries);
				note(`target under ${minimum} px`, await smallTargets(page, minimum));
				note("focus", await focusProblems(page, reader ? 15 : 40));

				await page.setViewportSize({ width: 320, height: 568 });
				await open(page, view);
				note("overflow at 320 px", await overflowProblems(page));
				note(
					"text spacing at 320 px",
					await withTextSpacing(page, () => clippedTextProblems(page)),
				);

				// 200 % zoom on a 1280 px window is a window of 640 CSS pixels.
				await page.setViewportSize({ width: 640, height: 400 });
				await open(page, view);
				note("overflow at 200 % zoom", await overflowProblems(page));

				await page.setViewportSize(size);
				await open(page, view);
				note(
					"text spacing",
					await withTextSpacing(page, () => clippedTextProblems(page)),
				);
			}
		} finally {
			await extra.cleanup();
		}
		expect(problems).toEqual([]);
	});
});
