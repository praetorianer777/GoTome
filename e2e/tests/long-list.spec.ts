import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

// The list of books is answered here, as a library of this many would: no
// stack holds that many for a test, and only the page's handling of them is
// under test.
const TOTAL = 500;

function title(i: number): string {
	return `Long Book ${String(i).padStart(3, "0")}`;
}

test("a long list keeps only the books near the window, and back returns to the same place", async ({
	page,
}, testInfo) => {
	test.setTimeout(45_000);
	await page.route(/\/api\/v1\/books\?/, async (route) => {
		const query = new URL(route.request().url()).searchParams;
		const limit = Number(query.get("limit") ?? 50);
		const from = Number(query.get("cursor") ?? 0);
		const books = Array.from(
			{ length: Math.min(limit, TOTAL - from) },
			(_, k) => ({
				id: `00000000-0000-7000-8000-${String(from + k).padStart(12, "0")}`,
				libraryId: "00000000-0000-7000-8000-999999999999",
				title: title(from + k),
				authors: ["A. Writer"],
				formats: ["epub"],
				addedAt: "2026-01-01T00:00:00Z",
				status: "unread",
				placeholder: false,
			}),
		);
		const next = from + books.length;
		await route.fulfill({
			json: { books, ...(next < TOTAL ? { nextCursor: String(next) } : {}) },
		});
	});
	await openSignedIn(page);
	// A library of its own, for the page to list from at all.
	const created = await page.request.post("/api/v1/libraries", {
		data: {
			name: `Long list ${testInfo.project.name} ${Date.now()}`,
			mode: "managed",
			visibility: "private",
		},
	});
	expect(created.status()).toBe(201);
	const library = (await created.json()).id as string;
	await page.reload();

	// Scrolling to the end loads every page.
	const region = page.getByRole("region", { name: "Books" });
	// While a page loads, the button says so instead.
	const more = region.getByRole("button", { name: /^(Show more|Loading…)$/ });
	await expect(
		region.getByText(title(0), { exact: true }).first(),
	).toBeVisible();
	const toEnd = () =>
		page.evaluate(() =>
			window.scrollTo(0, document.documentElement.scrollHeight),
		);
	await expect
		.poll(
			async () => {
				await toEnd();
				return more.count();
			},
			{ timeout: 30_000 },
		)
		.toBe(0);
	// Rows measured on the way down make the page taller than the estimate
	// did, so the end is scrolled to again until the last book is there.
	const last = page.getByText(title(TOTAL - 1), { exact: true }).first();
	await expect
		.poll(
			async () => {
				await toEnd();
				return last.isVisible();
			},
			{ timeout: 15_000 },
		)
		.toBe(true);
	await expect(last).toBeInViewport();
	const cards = region.getByRole("listitem");
	await expect(cards.first()).toHaveAttribute("aria-setsize", String(TOTAL));
	expect(await cards.count()).toBeLessThan(150);
	await expect(page.getByText(title(0), { exact: true })).toHaveCount(0);

	// Halfway down, a book is opened and left again.
	await page.evaluate(() =>
		window.scrollTo(0, document.documentElement.scrollHeight / 2),
	);
	// The card at the middle of the window, once the rows for where it was
	// scrolled to are in: a click does not scroll to it.
	let name = "";
	await expect
		.poll(async () => {
			name = await page.evaluate(() => {
				const middle = window.innerHeight / 2;
				const card = [...document.querySelectorAll("[role=listitem]")].find(
					(c) => {
						const box = c.getBoundingClientRect();
						return box.top <= middle && box.bottom >= middle;
					},
				);
				return card?.querySelector(".line-clamp-2")?.textContent ?? "";
			});
			return name;
		})
		.toMatch(/^Long Book \d+$/);
	await page.getByRole("link", { name: new RegExp(`^${name}`) }).click();
	await expect(page).toHaveURL(/\/books\//);
	await page.goBack();
	// Back draws the list from its start, then scrolls to the row: in WebKit
	// on CI's runners that has taken more than five seconds.
	await expect(page.getByText(name, { exact: true }).first()).toBeInViewport({
		timeout: 15_000,
	});
	expect(await cards.count()).toBeLessThan(150);
	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
