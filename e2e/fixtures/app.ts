import { expect, type Page } from "@playwright/test";
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
