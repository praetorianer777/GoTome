import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn } from "../fixtures/app";
import { makeShortEpub } from "../fixtures/ebooks";
import {
	deleteLibrary,
	newLibrary,
	uploadEpub,
} from "../fixtures/library";

// The stack embeds with vectors made from words (GOTOME_TEST_EMBEDDINGS),
// so a description finds the books that share its words; how well a model
// ranks them is the integration tests' and similar-check's.
test("the search page searches by a description and keeps the words when switched back", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name}${Date.now()}`;
	const title = `Lagoon ${stamp}`;
	const about = `a detective story set in Venice ${stamp}`;
	await openSignedIn(page);
	const library = await newLibrary(page, `Describe ${stamp}`);
	await uploadEpub(
		page,
		library,
		title,
		makeShortEpub(title, `describe ${stamp}`, `This is ${about}, among canals.`),
	);

	await page.goto("/search");
	await page.getByRole("button", { name: "Description" }).click();
	await expect(page.getByRole("button", { name: "Description" })).toHaveAttribute("aria-pressed", "true");
	const box = page.getByRole("searchbox", { name: "What the books are about" });
	await box.fill(about);
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL(/mode=description/);
	await expect(async () => {
		await page.reload();
		await expect(page.getByRole("link", { name: title }).first()).toBeVisible({ timeout: 2_000 });
	}).toPass({ timeout: 40_000 });

	await page.getByRole("button", { name: "Words in the text" }).click();
	await expect(page.getByRole("searchbox", { name: "Words to look for" })).toHaveValue(about);

	await deleteLibrary(page, library);
});
