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

/**
 * Goes to a page of a test that is signed in. WebKit under load now and
 * then loses the context's session cookie without the server ending the
 * session (#511: a poll of the duplicates page found the sign-in page for
 * its last 35 seconds); the page then signs in again and goes on.
 */
export async function gotoSignedIn(page: Page, path: string): Promise<void> {
	await page.goto(path);
	// The app routes after the first render: wait for a page before asking which.
	await page.getByRole("heading", { level: 1 }).first().waitFor();
	if (await page.getByRole("heading", { name: "Sign in", level: 1 }).isVisible()) {
		await signIn(page);
		await expect(page.getByRole("heading", { name: "Sign in", level: 1 })).toBeHidden();
		await page.goto(path);
	}
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
	await chooseLibraryIn(
		page,
		page.getByRole("combobox", { name: "Library", exact: true }),
		name,
		page.getByRole("heading", { name, level: 2 }),
	);
}

/**
 * Chooses a library in a choice the app offers only while there are
 * several libraries, and only once it has them: looked for at once, the
 * choice is missed, or lacks the library, and the test goes on in another
 * one. It is tried again until the choice offers the library, or until no
 * choice is needed: where chosen is given, once it is visible (the page
 * names the library it works in); otherwise once the person sees at most
 * one library.
 */
export async function chooseLibraryIn(
	page: Page,
	choice: Locator,
	name: string,
	chosen?: Locator,
): Promise<void> {
	await expect(async () => {
		if (await choice.isVisible()) {
			await choice.selectOption({ label: name }, { timeout: 1_000 });
		} else if (!chosen) {
			const seen = await page.request.get("/api/v1/libraries");
			const { libraries } = (await seen.json()) as { libraries: unknown[] };
			expect(libraries.length, "several libraries, and no choice offered yet").toBeLessThanOrEqual(1);
		}
		if (chosen) {
			await expect(chosen).toBeVisible({ timeout: 1_000 });
		}
	}).toPass({ timeout: 15_000 });
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
	// On a desktop the sections are tabs in the top bar, on a phone the first
	// group of the drawer's "Main".
	return page
		.getByRole("navigation", { name: "Main" })
		.or(page.getByRole("navigation", { name: "Areas" }));
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

/**
 * Chooses the library the upload page adds files to. The choice is there
 * only once the page knows the libraries, and only when there are several
 * to choose from: looked for before that, it would be missed and the files
 * would go into whichever library comes first.
 */
export async function chooseUploadLibrary(page: Page, name: string): Promise<void> {
	await chooseLibraryIn(
		page,
		page.getByRole("combobox", { name: "Into the library" }),
		name,
		page.getByRole("region", { name: "Files to add" }).getByText(`Drop files here to add them to ${name},`),
	);
}
