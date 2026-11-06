import { expect, type Response, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { VIEWS } from "../fixtures/views";

// Most pages load on their first visit rather than with the app (#353): each
// is opened here by its address, as a bookmark or a reload opens it.
test("every main page loads on its own", async ({ page }) => {
	await openSignedIn(page);
	for (const view of VIEWS) {
		await page.goto(view.path);
		await expect(
			page.getByRole("heading", { name: view.heading, level: 1 }),
		).toBeVisible();
	}
});

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
