import { expect, type Locator, type Page } from "@playwright/test";
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
