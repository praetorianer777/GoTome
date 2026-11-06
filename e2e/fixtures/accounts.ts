import {
	type APIRequestContext,
	type Browser,
	type BrowserContext,
	type BrowserContextOptions,
	expect,
	type Page,
	type TestInfo,
} from "@playwright/test";

export type Role = "reader" | "editor" | "admin";

export interface Account {
	id: string;
	username: string;
	password: string;
}

/**
 * Makes an account of the role through the API, as the administrator the
 * request is signed in as. The name carries the stamp, as every browser
 * project makes its own on the one stack; the password is a test value of
 * a throwaway stack.
 */
export async function makeAccount(
	admin: APIRequestContext,
	role: Role,
	stamp: string,
): Promise<Account> {
	const username = `${role}-${stamp}`.toLowerCase().replace(/[^a-z0-9-]/g, "-");
	const password = `pw-${username}`;
	const made = await admin.post("/api/v1/users", {
		data: { username, password, role },
	});
	expect(made.status()).toBe(201);
	return { id: (await made.json()).id as string, username, password };
}

/**
 * A browser of the person's own, signed in through the API. The page is
 * blank: the caller opens what it needs.
 */
export async function signedIn(
	browser: Browser,
	testInfo: TestInfo,
	account: Account,
	options: BrowserContextOptions = {},
): Promise<{ context: BrowserContext; page: Page }> {
	const context = await browser.newContext({
		...options,
		baseURL: testInfo.project.use.baseURL,
	});
	const page = await context.newPage();
	const login = await page.request.post("/api/v1/auth/login", {
		data: { username: account.username, password: account.password },
	});
	expect(login.ok()).toBe(true);
	return { context, page };
}

/** A new managed library, made through the API; its ID. */
export async function makeLibrary(
	admin: APIRequestContext,
	name: string,
	visibility: "shared" | "private",
): Promise<string> {
	const created = await admin.post("/api/v1/libraries", {
		data: { name, mode: "managed", visibility },
	});
	expect(created.status()).toBe(201);
	return (await created.json()).id as string;
}

/** Uploads a file into the library through the API; the book and file it became. */
export async function uploadFile(
	request: APIRequestContext,
	library: string,
	name: string,
	mimeType: string,
	buffer: Buffer,
): Promise<{ bookId: string; fileId: string }> {
	const uploaded = await request.post(`/api/v1/libraries/${library}/uploads`, {
		multipart: { file: { name, mimeType, buffer } },
	});
	const answer = await uploaded.json();
	expect(answer.outcome).toBe("added");
	return { bookId: answer.bookId as string, fileId: answer.fileId as string };
}
