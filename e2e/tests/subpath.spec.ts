import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { accountMenu, signIn } from "../fixtures/app";
import { ADMIN } from "../fixtures/stack";

// Run by make test-e2e-subpath against a stack started with
// GOTOME_BASE_PATH set to E2E_BASE_PATH. The project's baseURL ends in the
// prefix, so every address here is relative to it: a leading slash would
// leave it.
const prefix = process.env.E2E_BASE_PATH ?? "/";

test.beforeAll(async ({ playwright }, testInfo) => {
	const request = await playwright.request.newContext({ baseURL: testInfo.project.use.baseURL });
	const status = await request.get("api/v1/setup");
	expect(status.ok(), "the app answers below its prefix").toBe(true);
	if (((await status.json()) as { needed: boolean }).needed) {
		const made = await request.post("api/v1/setup", { data: ADMIN });
		expect(made.ok()).toBe(true);
	}
	await request.dispose();
});

test("the app signs in, reloads and signs out below its prefix", async ({ page }) => {
	const failed: string[] = [];
	page.on("response", (r) => {
		if (r.status() >= 400 && r.status() !== 401) failed.push(`${r.status()} ${r.url()}`);
	});

	await page.goto("");
	await expect(page).toHaveURL(new RegExp(`${prefix}login$`));
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	await expect(page.locator("base")).toHaveAttribute("href", prefix);

	await signIn(page);
	await expect(page).toHaveURL(new RegExp(`${prefix}$`));
	await expect(page.getByRole("heading", { name: "Library", level: 1 })).toBeVisible();

	// The session cookie is scoped to the prefix, and a reload of a deep
	// address loads the app from it.
	const cookies = await page.context().cookies();
	expect(cookies.find((c) => c.name === "gotome_session")?.path).toBe(prefix);
	// The service worker's scope is the prefix, so it serves nothing else.
	const scope = await page.evaluate(async () => (await navigator.serviceWorker.ready).scope);
	expect(new URL(scope).pathname).toBe(prefix);
	await page.goto("profile");
	await expect(page).toHaveURL(new RegExp(`${prefix}profile$`));
	await page.reload();
	await expect(page.getByRole("button", { name: `Account: ${ADMIN.username}` })).toBeVisible();

	await (await accountMenu(page)).getByRole("button", { name: "Sign out" }).click();
	await expect(page).toHaveURL(new RegExp(`${prefix}login$`));
	expect(failed).toEqual([]);
});

test("nothing answers outside the prefix, and the bare prefix leads in", async ({ request, baseURL }) => {
	const origin = new URL(baseURL ?? "").origin;
	expect((await request.get(`${origin}/api/v1/version`, { maxRedirects: 0 })).status()).toBe(404);
	expect((await request.get("api/v1/version")).ok()).toBe(true);
	if (prefix !== "/") {
		const bare = await request.get(origin + prefix.slice(0, -1), { maxRedirects: 0 });
		expect(bare.status()).toBe(308);
		expect(bare.headers().location).toBe(prefix);
	}
});

test("the pages tell the browser where they are", async ({ page }) => {
	await page.goto("login");
	await expect(page.locator('meta[name="gotome-base"]')).toHaveAttribute("content", prefix);
	const manifest = await page.request.get("manifest.webmanifest");
	expect(manifest.ok()).toBe(true);
	expect((await manifest.json()).start_url).toBe(".");
	await signIn(page);
	await expect(page.getByRole("heading", { name: "Library", level: 1 })).toBeVisible();
	await page.goto("profile");
	await expect(page.getByRole("link", { name: /plugin/i }).first()).toHaveAttribute(
		"href",
		`${prefix}api/v1/koreader/plugin.zip`,
	);
});

test("after signing in, the person lands where they were going", async ({ page }) => {
	await page.goto("profile?tab=x");
	await expect(page).toHaveURL(/\/login\?redirect=%2Fprofile/);
	await signIn(page);
	await expect(page).toHaveURL(new RegExp(`${prefix}profile\\?tab=x$`));
});
