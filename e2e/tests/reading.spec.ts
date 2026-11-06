import { expect, type Page, test } from "@playwright/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";

/** Opens the library's filters, which a phone keeps behind a button. */
async function filters(page: Page) {
	const toggle = page.getByRole("button", { name: /^Filters/ });
	if (
		(await toggle.isVisible()) &&
		(await toggle.getAttribute("aria-expanded")) === "false"
	) {
		await toggle.click();
	}
	return page.getByRole("complementary", { name: "Filters" });
}

test(
	"a status and a rating set on a book's page stay, and the library filters by them",
	{
		tag: "@smoke",
	},
	async ({ page }, testInfo) => {
		test.setTimeout(60_000);
		const stamp = `${testInfo.project.name} ${Date.now()}`;
		const read = `Finished ${stamp}`;
		const unread = `Untouched ${stamp}`;
		await openSignedIn(page);
		const { id, books } = await libraryOfBooks(page, `Reading ${stamp}`, [
			read,
			unread,
		]);

		await page.goto(`/books/${books[read]}`);
		const mine = page.getByRole("region", { name: "Your reading" });
		await mine.getByRole("combobox", { name: "Your status" }).selectOption({
			label: "Completed",
		});
		await mine.getByRole("button", { name: "4 of 5 stars" }).click();
		await expect(
			mine.getByRole("button", { name: "4 of 5 stars" }),
		).toHaveAttribute("aria-pressed", "true");

		await page.reload();
		await expect(
			mine.getByRole("combobox", { name: "Your status" }),
		).toHaveValue("completed");
		await expect(
			mine.getByRole("button", { name: "4 of 5 stars" }),
		).toHaveAttribute("aria-pressed", "true");

		await page.goto(`/?library=${id}`);
		const list = page.getByRole("region", { name: "Books" });
		await expect(list.getByRole("link")).toHaveCount(2);
		await (await filters(page))
			.getByRole("checkbox", { name: "Completed (1)" })
			.check();
		await expect(list.getByRole("link")).toHaveCount(1);
		await expect(list.getByRole("link", { name: read })).toBeVisible();

		await page.reload();
		await expect(list.getByRole("link")).toHaveCount(1);
		await (await filters(page))
			.getByRole("checkbox", { name: "Completed (1)" })
			.uncheck();
		await expect(list.getByRole("link")).toHaveCount(2);
		await (await filters(page))
			.getByRole("checkbox", { name: "4 of 5 stars (1)" })
			.check();
		await expect(list.getByRole("link")).toHaveCount(1);
		await expect(list.getByRole("link", { name: read })).toBeVisible();

		// The same star again takes the rating away.
		await page.goto(`/books/${books[read]}`);
		await mine.getByRole("button", { name: "4 of 5 stars" }).click();
		await expect(
			mine.getByRole("button", { name: "4 of 5 stars" }),
		).toHaveAttribute("aria-pressed", "false");
		await page.reload();
		await expect(
			mine.getByRole("button", { name: "4 of 5 stars" }),
		).toHaveAttribute("aria-pressed", "false");
		await expect(
			mine.getByRole("combobox", { name: "Your status" }),
		).toHaveValue("completed");

		expect(
			(await page.request.delete(`/api/v1/libraries/${id}`)).status(),
		).toBe(204);
	},
);
