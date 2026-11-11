import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";

// The other tests start with the strip folded (playwright.config.ts).
test.use({ storageState: { cookies: [], origins: [] } });

test("the strip over the library lists a book in progress and the new ones, folds, and opens the reader", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const going = `Midway ${stamp}`;
	const other = `Fresh ${stamp}`;
	await openSignedIn(page);
	const { id, books } = await libraryOfBooks(page, `Strip ${stamp}`, [going, other]);

	const detail = await page.request.get(`/api/v1/books/${books[going]}`);
	const fileId = ((await detail.json()) as { files: { id: string }[] }).files[0]?.id;
	expect(
		(
			await page.request.put(`/api/v1/books/${books[going]}/progress/ebook`, {
				data: { fileId, locator: "page:3", fraction: 0.3, clientId: "strip-test" },
			})
		).ok(),
	).toBe(true);

	await page.goto(`/?library=${id}`);
	const toggle = page.getByRole("button", { name: "Reading and new arrivals" });
	// A phone starts with it folded, to keep the grid in the first screen.
	if ((await toggle.getAttribute("aria-expanded")) === "false") {
		await toggle.click();
	}
	const strip = page.getByRole("region", { name: "Reading and new arrivals" });
	const continuing = strip.getByRole("region", { name: "Continue reading" });
	await expect(continuing.getByRole("link", { name: new RegExp(going) })).toContainText(
		"30 % read",
	);
	await expect(
		strip
			.getByRole("region", { name: "New in the library" })
			.getByRole("link", { name: new RegExp(other) }),
	).toBeVisible();

	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await expect(continuing).toBeHidden();
	await page.reload();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await toggle.click();

	// The link leads to the place in the reader; the reader itself is the
	// subject of its own tests (the file here is not a real PDF).
	await expect(
		continuing.getByRole("link", { name: new RegExp(going) }),
	).toHaveAttribute("href", `/books/${books[going]}/read/${fileId}`);
});
