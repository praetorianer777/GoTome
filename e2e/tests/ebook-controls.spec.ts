import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import { makeLibrary, uploadFile } from "../fixtures/accounts";
import { bookFrame, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

// Laying a book out keeps a loaded WebKit busy enough in CI that its
// controls take more than the usual five seconds to settle for a click.
test.use({ actionTimeout: 15_000 });

/** A word of letters alone that no other test's books hold. */
function uniqueWord(project: string): string {
	return `zq${project}${Date.now().toString(36)}`
		.toLowerCase()
		.replace(/[^a-z0-9]/g, "")
		.replace(/\d/g, (d) => "abcdefghij"[Number(d)] ?? "");
}

/**
 * A computed style of the book's root element, in the frame that shows the
 * text. Laying the book out again replaces the frames, so each look finds
 * the frame afresh.
 */
async function bookStyle(
	page: Page,
	text: string,
	property: "backgroundColor" | "fontSize" | "columnWidth",
) {
	const frame = await bookFrame(page, text);
	return frame
		.evaluate((p) => getComputedStyle(document.documentElement)[p], property)
		.catch(() => "");
}

test("the ebook reader finds words in the book, and reaching its end marks it read", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const word = uniqueWord(testInfo.project.name);
	await openSignedIn(page);
	const library = await makeLibrary(
		page.request,
		`Ebook controls ${stamp}`,
		"private",
	);
	const name = `Seaside ${stamp}.epub`;
	const { bookId } = await uploadFile(
		page.request,
		library,
		name,
		"application/epub+zip",
		makeEpub(`Seaside ${stamp}`, 3, stamp, false, {
			3: `The ${word} stood by the sea.`,
		}),
	);

	await page.goto(`/books/${bookId}`);
	await expect(page.getByLabel("Your status")).toHaveValue("unread");
	// The place it opens at is saved first: the end is reached from before it.
	const opened = page.waitForResponse(
		(r) =>
			r.url().includes("/progress/ebook") && r.request().method() === "PUT",
	);
	await page.getByRole("link", { name: `Read ${name}` }).click();
	await bookFrame(page, "Chapter 1, paragraph 1.");
	await opened;

	await page.getByRole("button", { name: "Search", exact: true }).click();
	const panel = page.getByRole("search");
	await panel.getByLabel("Words to look for in the book").fill(word);
	await panel.getByRole("button", { name: "Find" }).click();
	await expect(page.getByText("Places found: 1")).toBeVisible({
		timeout: 15_000,
	});
	await page.getByRole("button", { name: new RegExp(word) }).click();
	await bookFrame(page, `The ${word} stood by the sea.`);
	await expect(
		page.getByRole("region", { name: `Pages of Seaside ${stamp}` }),
	).toHaveAttribute("data-chapter", "Chapter 3");

	await panel
		.getByLabel("Words to look for in the book")
		.fill("nowhere-to-be-found");
	await panel.getByRole("button", { name: "Find" }).click();
	await expect(
		page.getByText("The words are nowhere in the book."),
	).toBeVisible({ timeout: 15_000 });
	await page.getByRole("button", { name: "Search", exact: true }).click();
	await expect(panel).toBeHidden();

	const finished = page.waitForResponse(
		(r) =>
			r.url().includes("/progress/ebook") &&
			r.request().method() === "PUT" &&
			r.request().postDataJSON().fraction === 1,
	);
	await page.keyboard.press("End");
	await expect(page.getByText("100%", { exact: true })).toBeVisible();
	await finished;
	expect((await (await page.request.get(`/api/v1/books/${bookId}`)).json()).reading.status).toBe("completed");
	await page.getByRole("link", { name: "Back to the book" }).click();
	await expect(page.getByLabel("Your status")).toHaveValue("completed");

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("the ebook reader's colours, text size and layout are this browser's and kept over a reload", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const library = await makeLibrary(
		page.request,
		`Ebook look ${stamp}`,
		"private",
	);
	const name = `Lamplight ${stamp}.epub`;
	const { bookId } = await uploadFile(
		page.request,
		library,
		name,
		"application/epub+zip",
		makeEpub(`Lamplight ${stamp}`, 2, stamp),
	);
	const text = "Chapter 1, paragraph 1.";

	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: `Read ${name}` }).click();
	await bookFrame(page, text);
	await expect
		.poll(() => bookStyle(page, text, "backgroundColor"))
		.toBe("rgb(255, 255, 255)");
	const size = Number.parseFloat(await bookStyle(page, text, "fontSize"));
	expect(await bookStyle(page, text, "columnWidth")).not.toBe("auto");

	await page.getByRole("button", { name: "Display" }).click();
	await page.getByLabel("Page colours").selectOption({ label: "Dark" });
	await expect
		.poll(() => bookStyle(page, text, "backgroundColor"))
		.toBe("rgb(15, 23, 42)");
	await page.getByRole("button", { name: "Larger" }).click();
	await page.getByRole("button", { name: "Larger" }).click();
	await expect(page.getByText("Text size 120%")).toBeVisible();
	await expect
		.poll(async () =>
			Number.parseFloat(await bookStyle(page, text, "fontSize")),
		)
		.toBeCloseTo(size * 1.2, 0);
	await page.getByLabel("Layout").selectOption({ label: "Scrolling" });
	await expect.poll(() => bookStyle(page, text, "columnWidth")).toBe("auto");

	await page.reload();
	await bookFrame(page, text);
	await expect
		.poll(() => bookStyle(page, text, "backgroundColor"))
		.toBe("rgb(15, 23, 42)");
	await expect.poll(() => bookStyle(page, text, "columnWidth")).toBe("auto");
	await page.getByRole("button", { name: "Display" }).click();
	await expect(page.getByLabel("Page colours")).toHaveValue("dark");
	await expect(page.getByLabel("Layout")).toHaveValue("scrolled");
	await expect(page.getByText("Text size 120%")).toBeVisible();
	// The keys of the list do the same.
	await page.getByRole("button", { name: "Smaller" }).focus();
	await page.keyboard.press("-");
	await expect(page.getByText("Text size 110%")).toBeVisible();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
