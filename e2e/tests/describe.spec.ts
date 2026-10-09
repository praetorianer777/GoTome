import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

// The test stack runs without the embedding model, so a description is
// answered with why it cannot be searched; the integration tests rank books.
test("the search page searches by a description, and says when the model cannot run", async ({
	page,
}) => {
	await openSignedIn(page);
	await page.goto("/search");
	await page.getByRole("button", { name: "Description" }).click();
	await expect(page.getByRole("button", { name: "Description" })).toHaveAttribute("aria-pressed", "true");
	await page.getByRole("searchbox", { name: "What the books are about" }).fill("a detective story set in Venice");
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL(/mode=description/);
	await expect(page.getByRole("alert")).toContainText("Searching by description needs the embedding model");

	await page.getByRole("button", { name: "Words in the text" }).click();
	await expect(page.getByRole("searchbox", { name: "Words to look for" })).toHaveValue("a detective story set in Venice");
});
