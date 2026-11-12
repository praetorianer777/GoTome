import { expect, test } from "@playwright/test";

const BASE = "/GoTome";

test("the demo boots at its base path, signed in, with the banner and the sample books", async ({ page }) => {
	await page.goto(`${BASE}/`);
	await expect(page.getByRole("note").filter({ hasText: "This is a demo" })).toBeVisible();
	await expect(page.getByRole("link", { name: "Run your own" })).toHaveAttribute(
		"href",
		/github\.com\/praetorianer777\/GoTome/,
	);
	await expect(page.getByRole("link", { name: /Persuasion/ }).first()).toBeVisible();
	await expect(page).toHaveURL(`${BASE}/`);
});

test("a deep link opens and reloads, as Pages serves it through 404.html", async ({ page }) => {
	await page.goto(`${BASE}/books/b-persuasion`);
	await expect(page.getByRole("heading", { name: "Persuasion" })).toBeVisible();
	await page.reload();
	await expect(page.getByRole("heading", { name: "Persuasion" })).toBeVisible();
});

test("a book opens in the reader on the bundled EPUB", async ({ page }) => {
	await page.goto(`${BASE}/books/b-emma`);
	await page.getByRole("link", { name: /^Read/ }).first().click();
	await expect
		.poll(async () => (await Promise.all(page.frames().map((f) => f.content().catch(() => "")))).join(""), {
			timeout: 15_000,
		})
		.toContain("Emma Woodhouse, handsome");
});

test("the player loads the bundled audio parts", async ({ page }) => {
	await page.goto(`${BASE}/books/b-audio/listen`);
	await page.getByRole("button", { name: "Listen" }).click();
	await expect(page.getByText("The Keeper").first()).toBeVisible();
	await expect
		.poll(() => page.locator("audio").evaluate((el: HTMLAudioElement) => el.currentSrc))
		.toMatch(/\/GoTome\/demo\/files\/part1\.mp3$/);
});

test("pages that write say they are not in the demo", async ({ page }) => {
	for (const path of ["/upload", "/admin/settings", "/jobs", "/books/b-emma/edit"]) {
		await page.goto(`${BASE}${path}`);
		await expect(page.getByRole("heading", { name: "Not in the demo" })).toBeVisible();
	}
});

test("authors, series, collections, duplicates and statistics render", async ({ page }) => {
	await page.goto(`${BASE}/series/sherlock%20holmes`);
	await expect(page.getByRole("heading", { name: "Sherlock Holmes" })).toBeVisible();
	await page.goto(`${BASE}/duplicates`);
	await expect(page.getByText("Emma").first()).toBeVisible();
	await page.goto(`${BASE}/collections`);
	await expect(page.getByText("Start here")).toBeVisible();
	await page.goto(`${BASE}/stats`);
	await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});

test("nothing leaves the page's own origin", async ({ page, baseURL }) => {
	const outside: string[] = [];
	page.on("request", (request) => {
		const url = request.url();
		if (!url.startsWith("data:") && !url.startsWith("blob:") && new URL(url).origin !== new URL(baseURL!).origin) {
			outside.push(url);
		}
	});
	await page.goto(`${BASE}/`);
	await page.getByRole("link", { name: /Persuasion/ }).first().click();
	await expect(page.getByRole("heading", { name: "Persuasion" })).toBeVisible();
	expect(outside).toEqual([]);
});

test("the demo works at phone width", async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 800 });
	await page.goto(`${BASE}/`);
	await expect(page.getByRole("note").filter({ hasText: "This is a demo" })).toBeVisible();
	const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
	expect(overflow).toBe(false);
});
