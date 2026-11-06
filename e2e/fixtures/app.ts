import { expect, type Frame, type Locator, type Page } from "@playwright/test";
import { ADMIN } from "./stack";

/** Fills the sign-in form. The page must already be the sign-in page. */
export async function signIn(page: Page, credentials: { username: string; password: string } = ADMIN): Promise<void> {
	await page.getByLabel("User name").fill(credentials.username);
	await page.getByLabel("Password").fill(credentials.password);
	// Exactly: an identity provider set up by another test adds "Sign in
	// with …".
	await page.getByRole("button", { name: "Sign in", exact: true }).click();
}

/** Opens the app signed in, ending on the library. */
export async function openSignedIn(page: Page): Promise<void> {
	await page.goto("/");
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	await signIn(page);
	await expect(
		page.getByRole("heading", { name: "Library", level: 1 }),
	).toBeVisible();
}

/**
 * Narrows the library page to one library. Every browser project adds its
 * own to the one stack, so the page usually offers a choice.
 */
export async function chooseLibrary(page: Page, name: string): Promise<void> {
	// The list of books is there once the libraries are known.
	await expect(page.getByRole("region", { name: "Books" })).toBeVisible();
	const choice = page.getByRole("combobox", { name: "Library", exact: true });
	if (await choice.isVisible()) {
		await choice.selectOption({ label: name });
	}
}

/**
 * Opens a page of the app and checks that it came with the server's
 * Content Security Policy, so that whatever the test then does in a reader
 * or the player is done under it.
 */
export async function gotoUnderPolicy(page: Page, path: string): Promise<void> {
	const response = await page.goto(path);
	const policy = response?.headers()["content-security-policy"] ?? "";
	expect(policy).toContain("script-src 'self' 'wasm-unsafe-eval';");
	expect(policy).toContain("connect-src 'self';");
	expect(policy).toContain("object-src 'none';");
	expect(response?.headers()["x-content-type-options"]).toBe("nosniff");
}

/**
 * The main navigation. On a narrow screen it is in the drawer the menu
 * button opens, which this opens first.
 */
export async function mainNav(page: Page): Promise<Locator> {
	await expect(page.getByRole("banner")).toBeVisible();
	const menu = page.getByRole("button", { name: "Menu" });
	if (await menu.isVisible()) {
		await menu.click();
	}
	return page.getByRole("navigation", { name: "Main" });
}

/** Opens the account menu at the top right of the signed-in person. */
export async function accountMenu(page: Page, username: string = ADMIN.username): Promise<Locator> {
	await page.getByRole("button", { name: `Account: ${username}` }).click();
	return page.getByRole("group", { name: "Account" });
}

/**
 * Makes a shared managed library holding one book per title, each from a
 * PDF of that name, uploaded one after the other in the order given. It
 * answers the library's ID and the books' IDs by title.
 */
export async function libraryOfBooks(
	page: Page,
	name: string,
	titles: string[],
): Promise<{ id: string; books: Record<string, string> }> {
	const created = await page.request.post("/api/v1/libraries", {
		data: { name, mode: "managed", visibility: "shared" },
	});
	expect(created.status()).toBe(201);
	const { id } = (await created.json()) as { id: string };
	const books: Record<string, string> = {};
	for (const title of titles) {
		const uploaded = await page.request.post(`/api/v1/libraries/${id}/uploads`, {
			multipart: {
				file: {
					name: `${title}.pdf`,
					mimeType: "application/pdf",
					buffer: Buffer.from(`%PDF-1.4\n% ${title}\n`),
				},
			},
		});
		expect(uploaded.status()).toBe(200);
		books[title] = ((await uploaded.json()) as { bookId: string }).bookId;
	}
	return { id, books };
}

/**
 * The frame the book's pages are in. foliate-js keeps them in a closed
 * shadow root, which locators do not reach; the frames of the page do.
 */
export async function bookFrame(page: Page, text: string): Promise<Frame> {
	let found: Frame | undefined;
	await expect(async () => {
		for (const frame of page.frames()) {
			if (frame === page.mainFrame()) continue;
			if (
				await frame
					.getByText(text, { exact: false })
					.count()
					.catch(() => 0)
			) {
				found = frame;
				return;
			}
		}
		throw new Error(`no frame shows ${text}`);
	}).toPass({ timeout: 20_000 });
	return found as Frame;
}
