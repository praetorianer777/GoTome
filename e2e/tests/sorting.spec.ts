import { expect, test } from "@playwright/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";

// A card without a cover holds the title twice, on the card and under it.
const endingIn = (...titles: string[]) =>
	titles.map((title) => new RegExp(`${title}$`));

test("the library is sorted and shown as covers or a list as chosen, and the address keeps both", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const [apple, banana, cherry] = ["Apple", "Banana", "Cherry"].map(
		(n) => `${n} ${stamp}`,
	) as [string, string, string];
	await openSignedIn(page);
	// Added in another order than the titles', each by an author whose name
	// sorts in a third, so that each sort puts them differently.
	const { id, books } = await libraryOfBooks(page, `Sort ${stamp}`, [
		banana,
		cherry,
		apple,
	]);
	const authors: Record<string, string> = {
		[apple]: "Zoe Zimmer",
		[banana]: "Mia Moser",
		[cherry]: "Ada Adler",
	};
	for (const [title, author] of Object.entries(authors)) {
		expect(
			(
				await page.request.patch(`/api/v1/books/${books[title]}`, {
					data: { contributors: [{ name: author, role: "author" }] },
				})
			).status(),
		).toBe(200);
	}

	await page.goto(`/?library=${id}`);
	const list = page.getByRole("region", { name: "Books" });
	const titles = list.getByRole("link", { name: new RegExp(stamp) });
	const sort = page.getByRole("combobox", { name: "Sort by" });
	await expect(sort).toHaveValue("title");
	await expect(titles).toHaveText(endingIn(apple, banana, cherry));

	await sort.selectOption({ label: "Author" });
	await expect(titles).toHaveText(endingIn(cherry, banana, apple));
	await sort.selectOption({ label: "Recently added" });
	await expect(titles).toHaveText(endingIn(apple, cherry, banana));

	await expect(list.getByRole("table")).toHaveCount(0);
	await page.getByRole("button", { name: "List", exact: true }).click();
	await expect(
		page.getByRole("button", { name: "List", exact: true }),
	).toHaveAttribute("aria-pressed", "true");
	await expect(list.getByRole("table")).toBeVisible();
	await expect(titles).toHaveText(endingIn(apple, cherry, banana));

	await page.reload();
	await expect(sort).toHaveValue("added");
	await expect(list.getByRole("table")).toBeVisible();
	await expect(titles).toHaveText(endingIn(apple, cherry, banana));

	await page.getByRole("button", { name: "Covers", exact: true }).click();
	await expect(list.getByRole("table")).toHaveCount(0);
	await sort.selectOption({ label: "Title" });
	await expect(titles).toHaveText(endingIn(apple, banana, cherry));
	await page.reload();
	await expect(list.getByRole("table")).toHaveCount(0);
	await expect(titles).toHaveText(endingIn(apple, banana, cherry));

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
