import { type Frame, type Page, expect } from "@playwright/test";
import { bookFrame, gotoUnderPolicy, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { test } from "../fixtures/test";

test.use({ actionTimeout: 15_000 });

async function upload(page: Page, name: string, buffer: Buffer, stamp: string) {
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Highlights ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
		multipart: { file: { name, mimeType: "application/epub+zip", buffer } },
	});
	const { bookId } = await uploaded.json();
	return { bookId: bookId as string, library };
}

/** Selects the text of the paragraph that holds the words, as dragging over it would. */
async function select(frame: Frame, words: string) {
	await frame.evaluate((text) => {
		const paragraph = [...document.querySelectorAll("p")].find((p) => p.textContent?.includes(text));
		if (!paragraph) throw new Error("no such paragraph");
		const range = document.createRange();
		range.selectNodeContents(paragraph);
		const chosen = document.getSelection();
		chosen?.removeAllRanges();
		chosen?.addRange(range);
	}, words);
}

test("text selected in the reader becomes a highlight with a note, kept over a reload and listed", async ({
	page,
}, testInfo) => {
	test.setTimeout(120_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const title = `Marked ${stamp}`;
	const { bookId, library } = await upload(page, `${title}.epub`, makeEpub(title, 3, stamp), stamp);

	await gotoUnderPolicy(page, `/books/${bookId}`);
	await page.getByRole("link", { name: `Read ${title}.epub` }).click();
	const frame = await bookFrame(page, "Chapter 1, paragraph 1.");
	await expect(page.getByText(/^\d+%$/)).toBeVisible();

	await select(frame, "Chapter 1, paragraph 1.");
	const bar = page.getByRole("form", { name: "Highlight the selected text" });
	await expect(bar).toBeVisible();
	await expect(bar).toContainText("Chapter 1, paragraph 1.");
	await bar.getByRole("radio", { name: "Green", exact: true }).check({ force: true });
	await bar.getByRole("textbox", { name: "Note" }).fill("My note on it");
	const kept = page.waitForResponse(
		(r) => r.url().includes(`/books/${bookId}/annotations`) && r.request().method() === "POST",
	);
	await bar.getByRole("button", { name: "Highlight", exact: true }).click();
	expect((await kept).status()).toBe(201);
	await expect(bar).toBeHidden();

	const listed = await (await page.request.get(`/api/v1/annotations?book=${bookId}`)).json();
	expect(listed.annotations).toHaveLength(1);
	expect(listed.annotations[0]).toMatchObject({
		kind: "cfi",
		color: "green",
		note: "My note on it",
		clientId: "web",
	});
	expect(listed.annotations[0].passage).toContain("Chapter 1, paragraph 1.");
	expect(listed.annotations[0].locator).toMatch(/^epubcfi\(/);

	// Shown again when the book is opened: the reader lists it, and it can be
	// gone to from the list.
	await page.reload();
	await bookFrame(page, "Chapter 1, paragraph 1.");
	await page.getByRole("button", { name: "Highlights", exact: true }).click();
	const panel = page.getByRole("list", { name: "Highlights" });
	await expect(panel.getByRole("listitem")).toHaveCount(1);
	await expect(panel).toContainText("My note on it");
	await panel.getByRole("button", { name: "Show in the book" }).click();

	// And on the book's page and in the list of every highlight.
	await gotoUnderPolicy(page, `/books/${bookId}`);
	await expect(page.getByRole("heading", { name: "Highlights", level: 2 })).toBeVisible();
	await page.goto("/highlights");
	await expect(page.getByRole("listitem").filter({ hasText: "My note on it" })).toBeVisible();
	await page.getByRole("searchbox", { name: "Search your highlights and notes" }).fill("no such words");
	await page.getByRole("button", { name: "Search", exact: true }).click();
	await expect(page.getByText("No highlight matches.")).toBeVisible();

	const markdown = await page.request.get(`/api/v1/annotations/export.md?book=${bookId}`);
	expect(markdown.status()).toBe(200);
	expect(await markdown.text()).toContain("My note on it");

	expect((await page.request.delete(`/api/v1/libraries/${library}`)).status()).toBe(204);
});
