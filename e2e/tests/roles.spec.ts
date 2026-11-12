import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import {
	makeAccount,
	makeLibrary,
	type Role,
	signedIn,
	uploadFile,
} from "../fixtures/accounts";
import { mainNav, openSignedIn } from "../fixtures/app";
import { makePdf } from "../fixtures/pdf";

/** The entries of the main navigation, by who may use the page behind them. */
const EDITOR_ENTRIES = [
	"Add books",
	"Review",
	"Clean up",
	"Genres",
	"Trash",
	"Jobs",
];
const ADMIN_ENTRIES = ["Libraries", "Users", "Settings"];

/** The pages each role is turned away from, to the library. */
const EDITOR_PAGES = [
	"/upload",
	"/review",
	"/cleanup",
	"/genres",
	"/trash",
	"/jobs",
];
const ADMIN_PAGES = ["/admin/libraries", "/admin/users", "/admin/webhooks", "/admin/settings"];

async function navEntries(page: Page, shown: string[], hidden: string[]) {
	const nav = await mainNav(page);
	// The menu is there once the entries every role has are.
	await expect(
		nav.getByRole("link", { name: "Library", exact: true }),
	).toBeVisible();
	await expect(nav.getByRole("link", { name: "Statistics" })).toBeVisible();
	for (const name of shown) {
		await expect(nav.getByRole("link", { name, exact: true })).toBeVisible();
	}
	for (const name of hidden) {
		await expect(nav.getByRole("link", { name, exact: true })).toHaveCount(0);
	}
}

async function turnedAway(page: Page, paths: string[]) {
	for (const path of paths) {
		await page.goto(path);
		await expect(page).toHaveURL(/\/$/);
		await expect(
			page.getByRole("heading", { name: "Library", level: 1 }),
		).toBeVisible();
	}
}

async function setUp(page: Page, role: Role, stamp: string) {
	await openSignedIn(page);
	const account = await makeAccount(page.request, role, stamp);
	const library = await makeLibrary(page.request, `Roles ${stamp}`, "shared");
	const name = `Roles ${stamp}.pdf`;
	const { bookId } = await uploadFile(
		page.request,
		library,
		name,
		"application/pdf",
		makePdf(2, 0, stamp),
	);
	return { account, library, name, bookId };
}

test("a reader finds no control to change the library, and its pages and routes refuse them", async ({
	page,
	browser,
}, testInfo) => {
	// A dozen pages, each loaded afresh.
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const { account, library, name, bookId } = await setUp(page, "reader", stamp);
	const { context, page: them } = await signedIn(browser, testInfo, account);

	await them.goto("/");
	await navEntries(
		them,
		["Duplicates", "Wishlist"],
		[...EDITOR_ENTRIES, ...ADMIN_ENTRIES],
	);

	await them.goto(`/books/${bookId}`);
	const files = them.getByRole("region", { name: "Files" });
	await expect(files.getByRole("link", { name: `Read ${name}` })).toBeVisible();
	await expect(
		files.getByRole("link", { name: `Download ${name}` }),
	).toBeVisible();
	await expect(them.getByLabel("Your status")).toBeVisible();
	await expect(them.getByRole("link", { name: "Edit details" })).toHaveCount(0);
	await expect(
		them.getByRole("link", { name: "Find details online" }),
	).toHaveCount(0);
	await expect(
		files.getByRole("button", { name: `Move ${name} to the trash` }),
	).toHaveCount(0);
	await expect(
		them.getByRole("button", { name: "Read the text again for search" }),
	).toHaveCount(0);

	await turnedAway(them, [
		...EDITOR_PAGES,
		...ADMIN_PAGES,
		`/books/${bookId}/edit`,
		`/books/${bookId}/find`,
	]);

	// The server refuses what the app does not offer.
	expect(
		(
			await them.request.patch(`/api/v1/books/${bookId}`, {
				data: { title: "Taken over" },
			})
		).status(),
	).toBe(403);
	expect((await them.request.get("/api/v1/trash")).status()).toBe(403);
	expect((await them.request.get("/api/v1/jobs")).status()).toBe(403);
	expect((await them.request.get("/api/v1/settings")).status()).toBe(403);
	expect((await them.request.get("/api/v1/users")).status()).toBe(403);
	await context.close();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("an editor changes books but finds no administration, and its pages and routes refuse them", async ({
	page,
	browser,
}, testInfo) => {
	// A dozen pages, each loaded afresh.
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const { account, library, name, bookId } = await setUp(page, "editor", stamp);
	const { context, page: them } = await signedIn(browser, testInfo, account);

	await them.goto("/");
	await navEntries(them, EDITOR_ENTRIES, ADMIN_ENTRIES);

	await them.goto(`/books/${bookId}`);
	const files = them.getByRole("region", { name: "Files" });
	await expect(files.getByRole("link", { name: `Read ${name}` })).toBeVisible();
	await expect(them.getByRole("link", { name: "Edit details" })).toBeVisible();
	await expect(
		them.getByRole("link", { name: "Find details online" }),
	).toBeVisible();
	await expect(
		files.getByRole("button", { name: `Move ${name} to the trash` }),
	).toBeVisible();

	// Their pages open for them, without what only an administrator does.
	await them.goto("/jobs");
	await expect(
		them.getByRole("heading", { name: "Background jobs", level: 1 }),
	).toBeVisible();
	await expect(
		them.getByRole("heading", { name: "Working out what books are about" }),
	).toBeVisible();
	await expect(
		them.getByRole("button", { name: /^(Pause|Start)$/ }),
	).toHaveCount(0);
	await them.goto("/trash");
	await expect(
		them.getByRole("heading", { name: "Trash", level: 1 }),
	).toBeVisible();

	await turnedAway(them, ADMIN_PAGES);

	expect((await them.request.get("/api/v1/settings")).status()).toBe(403);
	expect((await them.request.get("/api/v1/users")).status()).toBe(403);
	expect(
		(
			await them.request.post("/api/v1/libraries", {
				data: {
					name: `Not theirs ${stamp}`,
					mode: "managed",
					visibility: "shared",
				},
			})
		).status(),
	).toBe(403);
	expect(
		(await them.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(403);
	await context.close();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
