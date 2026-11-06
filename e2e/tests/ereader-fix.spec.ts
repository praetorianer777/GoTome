import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeShortEpub } from "../fixtures/ebooks";
import {
	deleteLibrary,
	newLibrary,
	uploadEpub,
	waitUntilRead,
} from "../fixtures/library";

test("an EPUB that lists a file it lacks is repaired from the book's page", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const title = `Unplated ${stamp}`;
	await openSignedIn(page);
	const library = await newLibrary(page, `Fix ${stamp}`);
	const book = await uploadEpub(
		page,
		library,
		title,
		makeShortEpub(title, `fix ${stamp}`, "The plate it names is not there.", {
			missingFile: true,
		}),
	);
	await waitUntilRead(page, book);

	await page.goto(`/books/${book}`);
	await expect(
		page.getByText("Files the EPUB lists but lacks: 1"),
	).toBeVisible();
	await page
		.getByRole("button", {
			name: `Repair the list of files in ${title}.epub`,
		})
		.click();
	await expect(page.getByRole("status").filter({ hasText: "Fixed." })).toHaveText(
		"Fixed. The file as it was is in the trash, from where it can be restored beside this one.",
		{ timeout: 15_000 },
	);
	await expect(page.getByText("Files the EPUB lists but lacks: 1")).toBeHidden();

	await page.goto("/trash");
	await expect(
		page.getByRole("listitem", { name: `${title} (original).epub` }),
	).toBeVisible();

	await deleteLibrary(page, library);
});
