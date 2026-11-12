import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { makeLibrary, uploadFile } from "../fixtures/accounts";
import { openSignedIn } from "../fixtures/app";
import { makePdf } from "../fixtures/pdf";

// Drawing pages keeps a loaded WebKit busy enough in CI that its controls
// take more than the usual five seconds to settle for a click.
test.use({ actionTimeout: 15_000 });

test("the PDF reader zooms, follows its bookmarks, finds words, and offers the place another device reached", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const library = await makeLibrary(
		page.request,
		`PDF controls ${stamp}`,
		"private",
	);
	const name = `Atlas ${stamp}.pdf`;
	const { bookId, fileId } = await uploadFile(
		page.request,
		library,
		name,
		"application/pdf",
		makePdf(30, 0, stamp, { 17: "The harbour lights were green." }, [
			{ title: "Coasts", page: 4 },
			{ title: "Islands", page: 12 },
		]),
	);

	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: `Read ${name}` }).click();
	const shown = page.getByRole("figure", { name: /^Page \d+ of / });
	await expect(shown).toHaveAttribute("data-page", "1", { timeout: 15_000 });

	// A page is 612 points wide, drawn a pixel to the point at 100%.
	const width = async () => Math.round((await shown.boundingBox())?.width ?? 0);
	await page.getByRole("button", { name: "Zoom out" }).click();
	await expect(page.getByRole("button", { name: "75%" })).toBeVisible();
	await expect.poll(width).toBe(459);
	await page.getByRole("button", { name: "Zoom in" }).click();
	await page.getByRole("button", { name: "Zoom in" }).click();
	await expect(page.getByRole("button", { name: "125%" })).toBeVisible();
	await expect.poll(width).toBe(765);
	await page.getByRole("button", { name: "125%" }).click();
	await expect(page.getByRole("button", { name: "Fit width" })).toHaveAttribute(
		"aria-pressed",
		"true",
	);

	await page.getByRole("button", { name: "Contents" }).click();
	const contents = page.getByRole("navigation", { name: "Contents" });
	await contents.getByRole("button", { name: "Islands" }).click();
	await expect(shown).toHaveAttribute("data-page", "12");
	await page.getByRole("button", { name: "Contents" }).click();
	await expect(contents).toBeHidden();

	await page.getByRole("button", { name: "Search", exact: true }).click();
	const search = page.getByRole("search");
	await search
		.getByLabel("Words to look for in the book")
		.fill("harbour lights");
	await search.getByRole("button", { name: "Find" }).click();
	await expect(page.getByText("Places found: 1")).toBeVisible({
		timeout: 15_000,
	});
	await page.getByRole("button", { name: /^Page 17:/ }).click();
	await expect(shown).toHaveAttribute("data-page", "17");
	await page.getByRole("button", { name: "Search", exact: true }).click();
	await expect(search).toBeHidden();

	// Another device reads on to page 25, after this browser last heard.
	await expect
		.poll(
			async () =>
				(
					await (
						await page.request.get(`/api/v1/books/${bookId}/progress`)
					).json()
				).ebook?.locator,
			{ timeout: 15_000 },
		)
		.toBe("page:17");
	const elsewhere = await page.request.put(
		`/api/v1/books/${bookId}/progress/ebook`,
		{
			data: {
				fileId,
				locator: "page:25",
				fraction: 25 / 30,
				clientId: `other-device-${stamp}`,
			},
		},
	);
	expect((await elsewhere.json()).saved).toBe(true);
	await page.getByRole("button", { name: "Previous page" }).click();
	await expect(shown).toHaveAttribute("data-page", "16");
	await expect(
		page.getByText("Another device is further on, at page 25."),
	).toBeVisible();
	await page.getByRole("button", { name: "Go there" }).click();
	await expect(shown).toHaveAttribute("data-page", "25");
	await expect(
		page.getByText("Another device is further on, at page 25."),
	).toBeHidden();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
