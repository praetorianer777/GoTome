import { type Frame, type Page, expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub, makeMobi } from "../fixtures/ebooks";

// Laying a book out keeps a loaded WebKit busy enough in CI that its
// controls take more than the usual five seconds to settle for a click.
test.use({ actionTimeout: 15_000 });

/** Uploads the file into a new private library and returns the book and the library. */
async function upload(
	page: Page,
	name: string,
	type: string,
	buffer: Buffer,
	stamp: string,
) {
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Reader ${name} ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: { file: { name, mimeType: type, buffer } },
		},
	);
	const { bookId, outcome } = await uploaded.json();
	expect(outcome).toBe("added");
	return { bookId: bookId as string, library };
}

/**
 * The frame the book's pages are in. foliate-js keeps them in a closed
 * shadow root, which locators do not reach; the frames of the page do.
 */
async function bookFrame(page: Page, text: string): Promise<Frame> {
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

const percent = (page: Page) => page.getByText(/^\d+%$/);

/** The next save of a place, in the chapter when one is named. */
const savedPlace = (page: Page, chapter?: string) =>
	page.waitForResponse((r) => {
		if (!r.url().includes("/progress/ebook") || r.request().method() !== "PUT") return false;
		const sent = r.request().postDataJSON();
		return String(sent.locator).startsWith("epubcfi(") && (!chapter || sent.chapter === chapter);
	});

test("an EPUB opens, turns its pages, keeps its place over a reload, and runs none of its scripts", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const offsite: string[] = [];
	// A request the policy refuses still shows as a request, and then as
	// failed; one that reached the network finishes.
	page.on("requestfinished", (r) => {
		if (r.url().includes("example.com")) offsite.push(r.url());
	});
	await openSignedIn(page);
	const { bookId, library } = await upload(
		page,
		`Hostile ${stamp}.epub`,
		"application/epub+zip",
		makeEpub(`Hostile ${stamp}`, 3, stamp, true),
		stamp,
	);

	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: `Read Hostile ${stamp}.epub` }).click();
	const book = await bookFrame(page, "Chapter 1, paragraph 1.");
	await expect(percent(page)).toBeVisible();

	// Nothing the book carries ran, and it fetched nothing from elsewhere.
	expect(
		await page.evaluate(() => document.body.dataset.bookScript),
	).toBeUndefined();
	expect(
		await page.evaluate(() => document.body.dataset.bookHandler),
	).toBeUndefined();
	expect(
		await book.evaluate(
			() => (window as unknown as { bookScript?: string }).bookScript,
		),
	).toBeUndefined();
	expect(offsite).toEqual([]);

	const firstSave = savedPlace(page);
	await page.getByRole("button", { name: "Page right" }).click();
	await firstSave;
	await page.keyboard.press("ArrowRight");
	await page.keyboard.press("ArrowRight");

	await page.getByRole("button", { name: "Contents" }).click();
	const toChapterTwo = savedPlace(page, "Chapter 2");
	await page
		.getByRole("navigation", { name: "Contents" })
		.getByRole("button", { name: "Chapter 2" })
		.click();
	await bookFrame(page, "Chapter 2, paragraph 1.");
	const saved = await (await toChapterTwo).json();
	expect(saved.progress.chapter).toBe("Chapter 2");
	const at = await percent(page).textContent();

	await page.reload();
	await bookFrame(page, "Chapter 2, paragraph 1.");
	await expect(percent(page)).toHaveText(at ?? "");
	await expect(
		page.getByRole("region", { name: `Pages of Hostile ${stamp}` }),
	).toHaveAttribute("data-chapter", "Chapter 2");

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("a MOBI opens, turns its pages and reopens where it was left", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const { bookId, library } = await upload(
		page,
		`Kindle ${stamp}.mobi`,
		"application/x-mobipocket-ebook",
		makeMobi(`Kindle ${stamp}`, 3, stamp),
		stamp,
	);

	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: `Read Kindle ${stamp}.mobi` }).click();
	await bookFrame(page, "Chapter 1, paragraph 1.");
	const start = await percent(page).textContent();

	for (let i = 0; i < 4; i++) {
		await page.getByRole("button", { name: "Page right" }).click();
	}
	await expect(percent(page)).not.toHaveText(start ?? "");
	// The reload must come back to the place saved last, once saving has
	// settled.
	const savedLocator = async () =>
		(await (await page.request.get(`/api/v1/books/${bookId}/progress`)).json())
			.ebook?.locator ?? "";
	let previous = "";
	await expect
		.poll(
			async () => {
				const now = await savedLocator();
				const settled = now !== "" && now === previous;
				previous = now;
				return settled;
			},
			{ intervals: [1000], timeout: 20_000 },
		)
		.toBe(true);
	const at = await percent(page).textContent();

	await page.reload();
	await bookFrame(page, "Chapter");
	await expect(percent(page)).toHaveText(at ?? "");
	expect(await savedLocator()).toBe(previous);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
