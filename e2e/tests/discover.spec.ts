import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn } from "../fixtures/app";

// The stack runs offline, so Discover has no provider to ask; the page says
// so. What it lists is answered to the browser as the server would, and the
// ranking and the filtering are held by the integration tests.

async function setDiscover(page: import("@playwright/test").Page, value: "on" | "off") {
	const set = await page.request.patch("/api/v1/settings", {
		data: { values: { "recommendations.discover": value } },
	});
	expect(set.status()).toBe(200);
}

test("Discover says why there is nothing: off until an administrator turns it on, then offline", async ({
	page,
}) => {
	await openSignedIn(page);
	await setDiscover(page, "off");
	await page.goto("/discover");
	await expect(page.getByRole("heading", { name: "Discover", level: 1 })).toBeVisible();
	await expect(page.getByText(/An administrator has not switched Discover on/)).toBeVisible();
	await expect(page.getByRole("button", { name: "Find books for me" })).toHaveCount(0);

	await setDiscover(page, "on");
	try {
		await page.goto("/discover");
		await expect(page.getByText(/This server runs offline and asks no metadata source/)).toBeVisible();
		await expect(page.getByRole("button", { name: "Find books for me" })).toHaveCount(0);
	} finally {
		await setDiscover(page, "off");
	}
});

test("Discover lists books by reason; Add to wishlist and Not interested take a book off the page", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const wished = `Wished ${stamp}`;
	const dismissed = `Dismissed ${stamp}`;
	const long = `Wolfeschlegelsteinhausenbergerdorff-${stamp.replaceAll(" ", "")}`;
	await openSignedIn(page);
	const item = (id: string, title: string, reason: Record<string, unknown>) => ({
		id,
		title,
		authors: [long],
		year: 2021,
		provider: "openlibrary",
		identifiers: [],
		tags: [],
		people: [],
		reason,
		wish: { title, provider: "openlibrary" },
	});
	let items = [
		item("00000000-0000-0000-0000-0000000000c1", wished, {
			kind: "series_next",
			name: `The Unabridged Chronicles of ${long}`,
			stars: 0,
			because: ["Liked One", "Liked Two"],
		}),
		item("00000000-0000-0000-0000-0000000000c2", dismissed, {
			kind: "author",
			name: long,
			stars: 5,
			because: [],
		}),
		item("00000000-0000-0000-0000-0000000000c3", `Kept ${stamp}`, {
			kind: "genre",
			name: "fantasy",
			stars: 0,
			because: [],
		}),
	];
	const dismissals: string[] = [];
	const wishes: string[] = [];
	await page.route("**/api/v1/me/discover", (route) =>
		route.fulfill({ json: { state: "ready", finishedAt: "2026-11-10T08:00:00Z", items } }),
	);
	await page.route("**/api/v1/me/discover/*/dismiss", (route) => {
		const id = route.request().url().split("/").at(-2) ?? "";
		dismissals.push(id);
		items = items.filter((i) => i.id !== id);
		return route.fulfill({ status: 204 });
	});
	await page.route("**/api/v1/books/wishes", (route) => {
		const title = (route.request().postDataJSON() as { title: string }).title;
		wishes.push(title);
		items = items.filter((i) => i.title !== title);
		return route.fulfill({ status: 201, json: {} });
	});

	await page.goto("/discover");
	await expect(page.getByRole("heading", { name: "Discover", level: 1 })).toBeVisible();
	const next = page.getByRole("region", { name: "Next in your series" });
	await expect(next.getByText(wished)).toBeVisible();
	await expect(next.getByText("Like Liked One, Liked Two")).toBeVisible();
	await expect(page.getByRole("region", { name: "By authors you rated" })).toContainText("whom you rated 5 stars");

	await page.getByRole("button", { name: `Not interested in ${dismissed}` }).click();
	await expect(page.getByText(dismissed)).toHaveCount(0);
	expect(dismissals).toEqual(["00000000-0000-0000-0000-0000000000c2"]);

	await page.getByRole("button", { name: `Add ${wished} to wishlist` }).click();
	await expect(page.getByText(wished)).toHaveCount(0);
	expect(wishes).toEqual([wished]);
	await expect(page.getByText(`Kept ${stamp}`)).toBeVisible();

	// A name no break can cut keeps to the column, on a phone too.
	const widest = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
	expect(widest).toBeLessThanOrEqual(0);
});

test("Discover shows how far a run is, then that it waits, then the books", async ({ page }) => {
	await openSignedIn(page);
	const long = "Wolfeschlegelsteinhausenbergerdorff";
	const counts = (asked: number, planned: number) => ({ asked, planned });
	let body: Record<string, unknown> = {
		state: "running",
		items: [],
		progress: {
			startedAt: "2026-11-13T08:00:00Z",
			planned: true,
			authors: counts(5, 10),
			series: counts(4, 8),
			genres: counts(3, 3),
			activity: "working",
			backingOff: [],
		},
	};
	await page.route("**/api/v1/me/discover", (route) => route.fulfill({ json: body }));

	await page.goto("/discover");
	const bar = page.getByRole("progressbar", { name: "How far the search for books is" });
	await expect(bar).toHaveAttribute("aria-valuenow", "12");
	await expect(bar).toHaveAttribute("aria-valuemax", "21");
	await expect(page.getByText("Asked about 12 of 21 authors, series and genres")).toBeVisible();
	await expect(page.getByText(/Asking the sources now/)).toBeVisible();

	body = {
		state: "running",
		items: [],
		progress: {
			...(body.progress as object),
			activity: "waiting",
			nextPassAt: new Date(Date.now() + 120_000).toISOString(),
			backingOff: [{ provider: long, until: "2026-11-13T09:00:00Z" }],
		},
	};
	await page.reload();
	await expect(page.getByText(/Waiting for the next pass, in about \d+ min/)).toBeVisible();
	await expect(page.getByText(/asked to be left alone until/)).toBeVisible();
	const widest = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
	expect(widest).toBeLessThanOrEqual(0);

	body = { state: "ready", finishedAt: "2026-11-13T09:00:00Z", items: [] };
	await page.reload();
	await expect(page.getByText(/Last looked on/)).toBeVisible();
	await expect(page.getByRole("progressbar")).toHaveCount(0);
});

test("Discover opens a book's details at the side or, on a phone, from the bottom, and the address keeps the book", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const long = `Wolfeschlegelsteinhausenbergerdorff-${stamp.replaceAll(" ", "")}`;
	const title = `Details ${stamp}`;
	await openSignedIn(page);
	const id = "00000000-0000-0000-0000-0000000000d1";
	const items = [
		{
			id,
			title,
			subtitle: `A subtitle that goes on and on ${long}`,
			authors: [long, "Pia Quill"],
			people: [{ name: "Pia Quill", key: "pia-quill" }],
			year: 2021,
			published: "2021-05-04",
			publisher: `Quay Press ${long}`,
			pageCount: 312,
			language: "en",
			description: `${"A very long description. ".repeat(60)}${long}`,
			identifiers: [{ type: "isbn", value: "9780306406157" }],
			tags: [`Sea stories ${long}`, "Adventure"],
			provider: "openlibrary",
			reason: { kind: "author", name: long, stars: 5, because: [`Liked ${long}`] },
			wish: { title, provider: "openlibrary" },
		},
	];
	await page.route("**/api/v1/me/discover", (route) =>
		route.fulfill({ json: { state: "ready", finishedAt: "2026-11-10T08:00:00Z", items } }),
	);
	await page.goto("/discover");
	const opener = page.getByRole("button", { name: title, exact: true });
	await opener.click();
	const panel = page.getByRole("dialog", { name: title });
	await expect(panel).toBeVisible();
	await expect(page).toHaveURL(new RegExp(`book=${id}`));
	await expect(panel.getByRole("link", { name: "Pia Quill" })).toHaveAttribute("href", "/authors/pia-quill");
	await expect(panel.getByText("9780306406157")).toBeVisible();
	await expect(panel.getByRole("button", { name: "Add to wishlist" })).toBeVisible();
	await expect(panel.getByRole("button", { name: "Not interested" })).toBeVisible();

	const view = page.viewportSize();
	const box = await panel.boundingBox();
	expect(box).not.toBeNull();
	if (view && box) {
		expect(box.x).toBeGreaterThanOrEqual(0);
		expect(box.x + box.width).toBeLessThanOrEqual(view.width + 1);
		expect(box.y + box.height).toBeLessThanOrEqual(view.height + 1);
		// The panel sits against the right edge, the sheet against the bottom.
		if (view.width >= 640) expect(box.x + box.width).toBeGreaterThanOrEqual(view.width - 20);
		else expect(box.y + box.height).toBeGreaterThanOrEqual(view.height - 1);
	}
	const sideways = await panel.evaluate((el) => el.scrollWidth - el.clientWidth);
	expect(sideways).toBeLessThanOrEqual(0);

	await page.keyboard.press("Escape");
	await expect(panel).toHaveCount(0);
	await expect(page).not.toHaveURL(/book=/);
	await expect(opener).toBeFocused();

	await opener.press("Enter");
	await expect(panel).toBeVisible();
	await page.goBack();
	await expect(panel).toHaveCount(0);

	await page.goto(`/discover?book=${id}`);
	await expect(panel).toBeVisible();
	await panel.getByRole("button", { name: "Close" }).click();
	await expect(panel).toHaveCount(0);

	await opener.click();
	await expect(panel).toBeVisible();
	await page.mouse.click(5, 5);
	await expect(panel).toHaveCount(0);
});

test("Discover: Already read, with stars, from the list and from the details, and a way back", async ({ page }, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const long = `Wolfeschlegelsteinhausenbergerdorff-${stamp.replaceAll(" ", "")}`;
	await openSignedIn(page);
	const item = (id: string, title: string) => ({
		id,
		title,
		authors: [long],
		year: 2021,
		provider: "openlibrary",
		identifiers: [],
		tags: [],
		people: [],
		reason: { kind: "author", name: long, stars: 5, because: [] },
		wish: { title, provider: "openlibrary" },
	});
	const first = `Read A ${stamp}`;
	const second = `Read B ${stamp}`;
	let items = [item("00000000-0000-0000-0000-0000000000e1", first), item("00000000-0000-0000-0000-0000000000e2", second)];
	let read: { id: string; title: string; authors: string[]; stars?: number; createdAt: string }[] = [];
	const sent: { id: string; stars?: number }[] = [];
	await page.route("**/api/v1/me/discover", (route) =>
		route.fulfill({ json: { state: "ready", finishedAt: "2026-11-10T08:00:00Z", items } }),
	);
	await page.route("**/api/v1/me/discover/read", (route) => route.fulfill({ json: { items: read } }));
	await page.route("**/api/v1/me/discover/read/*", (route) => {
		read = [];
		return route.fulfill({ status: 204 });
	});
	await page.route("**/api/v1/me/discover/*/read", (route) => {
		const id = route.request().url().split("/").at(-2) ?? "";
		const body = route.request().postDataJSON() as { stars?: number };
		sent.push({ id, stars: body.stars });
		const was = items.find((i) => i.id === id);
		items = items.filter((i) => i.id !== id);
		if (was) read = [{ id: `r-${id}`, title: was.title, authors: was.authors, stars: body.stars, createdAt: "2026-11-13T08:00:00Z" }, ...read];
		return route.fulfill({ status: 204 });
	});

	await page.goto("/discover");
	await page.getByRole("button", { name: `I already read ${first}` }).click();
	await page.getByRole("button", { name: "4 of 5 stars" }).click();
	await expect(page.getByRole("button", { name: first, exact: true })).toHaveCount(0);
	expect(sent).toEqual([{ id: "00000000-0000-0000-0000-0000000000e1", stars: 4 }]);

	await page.getByRole("button", { name: second, exact: true }).click();
	const panel = page.getByRole("dialog", { name: second });
	await panel.getByRole("button", { name: `I already read ${second}` }).click();
	await panel.getByRole("button", { name: "No rating" }).click();
	await expect(panel).toHaveCount(0);
	expect(sent.at(-1)).toEqual({ id: "00000000-0000-0000-0000-0000000000e2", stars: undefined });

	await page.getByText("Read elsewhere (2)").click();
	await expect(page.getByText(first, { exact: false }).first()).toBeVisible();
	const widest = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
	expect(widest).toBeLessThanOrEqual(0);
	await page.getByRole("button", { name: `Offer ${first} again` }).click();
	await expect(page.getByText("Read elsewhere (2)")).toHaveCount(0);
});
