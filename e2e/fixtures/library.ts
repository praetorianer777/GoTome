import { expect, type Page, type TestInfo } from "@playwright/test";

/**
 * A word that names the browser project, as the records of
 * fixtures/metadata.json do: the projects share one stack and one
 * account, and each looks up books of its own.
 */
export function projectWord(testInfo: TestInfo): string {
	const name = testInfo.project.name;
	return name.charAt(0).toUpperCase() + name.slice(1);
}

/** Makes a managed library and returns its ID. */
export async function newLibrary(
	page: Page,
	name: string,
	visibility: "private" | "shared" = "private",
): Promise<string> {
	const created = await page.request.post("/api/v1/libraries", {
		data: { name, mode: "managed", visibility },
	});
	expect(created.status()).toBe(201);
	return (await created.json()).id as string;
}

/** Uploads an EPUB into a library and returns its book's ID. */
export async function uploadEpub(
	page: Page,
	library: string,
	title: string,
	buffer: Buffer,
): Promise<string> {
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: {
					name: `${title}.epub`,
					mimeType: "application/epub+zip",
					buffer,
				},
			},
			// An upload is read and hashed while the stack is busy with the
			// other projects' books.
			timeout: 20_000,
		},
	);
	expect(uploaded.status()).toBe(200);
	return (await uploaded.json()).bookId as string;
}

/** Changes a book as a person would through its edit page. */
export async function editBook(
	page: Page,
	book: string,
	data: Record<string, unknown>,
): Promise<void> {
	const edited = await page.request.patch(`/api/v1/books/${book}`, { data });
	expect(edited.status()).toBe(200);
}

/** Waits until every file of the book has been read. */
export async function waitUntilRead(page: Page, book: string): Promise<void> {
	await expect
		.poll(
			async () => {
				const detail = await (
					await page.request.get(`/api/v1/books/${book}`)
				).json();
				return detail.files.every(
					(f: { extractState: string }) => f.extractState !== "pending",
				);
			},
			{ timeout: 30_000 },
		)
		.toBe(true);
}

export async function deleteLibrary(page: Page, id: string): Promise<void> {
	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
}
