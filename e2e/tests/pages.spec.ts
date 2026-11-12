import { expect, type Response } from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn } from "../fixtures/app";
import { VIEWS } from "../fixtures/views";

// Most pages load on their first visit rather than with the app (#353): each
// is opened here by its address, as a bookmark or a reload opens it.
for (const view of VIEWS) {
	test(`${view.path} loads on its own`, async ({ page }, testInfo) => {
		const webkit = testInfo.project.name === "webkit";
		await openSignedIn(page);
		try {
			await page.goto(view.path);
		} catch (error) {
			// WebKit in CI now and then fails a navigation with "internal
			// error" before any request reaches the app; a second load is
			// the same page, and anything else still fails.
			if (!webkit || !String(error).includes("internal error")) throw error;
			await page.goto(view.path);
		}
		// A first visit loads the page's chunk, which WebKit in CI is slowest at.
		await expect(
			page.getByRole("heading", { name: view.heading, level: 1 }),
		).toBeVisible({ timeout: webkit ? 10_000 : undefined });
	});
}

test("the app's files are sent compressed", async ({ page }) => {
	const scripts: Response[] = [];
	page.on("response", (response) => {
		if (/\/assets\/.*\.js$/.test(new URL(response.url()).pathname)) {
			scripts.push(response);
		}
	});
	await page.goto("/login");
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	let large = 0;
	for (const script of scripts) {
		// The build leaves files under 1 KiB as they are.
		if ((await script.body()).length < 1024) continue;
		large++;
		expect(["br", "gzip"], script.url()).toContain(
			await script.headerValue("content-encoding"),
		);
	}
	expect(large).toBeGreaterThan(0);
});
