import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import { libraryOfBooks, openSignedIn, signIn } from "../fixtures/app";
import { ADMIN } from "../fixtures/stack";

// A book without a cover stands as a card with its title, inside the same
// link, so a link's text is the title twice.
const endingIn = (...titles: string[]) =>
	titles.map((title) => new RegExp(`${title}$`));

/**
 * Clicks a button that moves a book and waits for the order to be saved:
 * the page shows the new order before, and a reload then would cut the
 * request off.
 */
async function reordered(page: Page, button: string): Promise<void> {
	const saved = page.waitForResponse(
		(r) => r.request().method() === "PUT" && r.url().endsWith("/order"),
	);
	await page.getByRole("button", { name: button }).click();
	expect((await saved).status()).toBe(200);
}

/** Makes a collection on the collections page, which then opens it. */
async function newCollection(page: Page, name: string): Promise<string> {
	await page.goto("/collections");
	const form = page.getByRole("form", { name: "New collection" });
	await form.getByRole("textbox", { name: "Name" }).fill(name);
	await form.getByRole("button", { name: "Create" }).click();
	await expect(page.getByRole("heading", { name, level: 1 })).toBeVisible();
	return page.url().split("/").pop() ?? "";
}

test(
	"a book added to a collection from its page is in it after a reload",
	{
		tag: "@smoke",
	},
	async ({ page }, testInfo) => {
		test.setTimeout(60_000);
		const stamp = `${testInfo.project.name} ${Date.now()}`;
		const title = `Shelved ${stamp}`;
		const shelf = `Favourites ${stamp}`;
		await openSignedIn(page);
		const { id, books } = await libraryOfBooks(page, `Collect ${stamp}`, [
			title,
		]);
		await newCollection(page, shelf);
		await expect(
			page.getByText(
				"No books yet. Add them from a book's page or from a selection in the library.",
			),
		).toBeVisible();

		await page.goto(`/books/${books[title]}`);
		await page.getByText(/^Your collections holding it: /).click();
		// Ticked once the server has it, so not at once.
		await page.getByRole("checkbox", { name: shelf }).click();
		await expect(page.getByRole("checkbox", { name: shelf })).toBeChecked();
		await expect(
			page.getByText("Your collections holding it: 1"),
		).toBeVisible();

		await page.reload();
		await expect(
			page.getByText("Your collections holding it: 1"),
		).toBeVisible();
		await page.getByText("Your collections holding it: 1").click();
		await expect(page.getByRole("checkbox", { name: shelf })).toBeChecked();
		await page.getByRole("link", { name: "Manage collections" }).click();
		await page.getByRole("link", { name: new RegExp(`^${shelf}`) }).click();
		await expect(
			page
				.getByRole("list", { name: "Books in this collection" })
				.getByRole("link", { name: title }),
		).toBeVisible();

		expect(
			(await page.request.delete(`/api/v1/libraries/${id}`)).status(),
		).toBe(204);
	},
);

test("a collection is reordered, pruned, renamed, shared with others and deleted", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const [first, second, third] = ["One", "Two", "Three"].map(
		(n) => `${n} ${stamp}`,
	) as [string, string, string];
	const name = `Draft list ${stamp}`;
	const renamed = `Reading list ${stamp}`;
	await openSignedIn(page);
	const { id: library, books } = await libraryOfBooks(
		page,
		`Arrange ${stamp}`,
		[first, second, third],
	);
	const collection = await newCollection(page, name);
	expect(
		(
			await page.request.post(`/api/v1/collections/${collection}/books`, {
				data: { books: [books[first], books[second], books[third]] },
			})
		).ok(),
	).toBe(true);

	await page.reload();
	const list = page.getByRole("list", { name: "Books in this collection" });
	const titles = list.getByRole("listitem").getByRole("link");
	await expect(titles).toHaveText(endingIn(first, second, third));

	await reordered(page, `Move ${third} up`);
	await expect(titles).toHaveText(endingIn(first, third, second));
	await reordered(page, `Move ${first} down`);
	await expect(titles).toHaveText(endingIn(third, first, second));
	await page.reload();
	await expect(titles).toHaveText(endingIn(third, first, second));

	await page
		.getByRole("button", { name: `Take ${first} out of the collection` })
		.click();
	await expect(titles).toHaveText(endingIn(third, second));
	await page.reload();
	await expect(titles).toHaveText(endingIn(third, second));

	await page.getByRole("button", { name: "Edit collection" }).click();
	const form = page.getByRole("form", { name: "Edit collection" });
	await form.getByRole("textbox", { name: "Name" }).fill(renamed);
	await form
		.getByRole("textbox", { name: "Description" })
		.fill("What to read next.");
	await form
		.getByRole("combobox", { name: "Who sees it" })
		.selectOption({ label: "Everyone signed in" });
	await form.getByRole("button", { name: "Save" }).click();
	await expect(form).toBeHidden();
	await page.reload();
	await expect(
		page.getByRole("heading", { name: renamed, level: 1 }),
	).toBeVisible();
	await expect(page.getByText("Everyone signed in")).toBeVisible();
	await expect(page.getByText("What to read next.")).toBeVisible();

	// Someone else sees it among those others share, and may not change it.
	const username = `reader-${testInfo.project.name}-${Date.now()}`;
	const password = `pw-${stamp}`;
	expect(
		(
			await page.request.post("/api/v1/users", {
				data: { username, password, role: "reader" },
			})
		).status(),
	).toBe(201);
	const theirs = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await theirs.newPage();
	await them.goto("/collections");
	await signIn(them, { username, password });
	const shared = them.getByRole("region", { name: "Shared by others" });
	await shared.getByRole("link", { name: new RegExp(`^${renamed}`) }).click();
	await expect(
		them.getByRole("heading", { name: renamed, level: 1 }),
	).toBeVisible();
	await expect(them.getByText(`Shared by ${ADMIN.username}`)).toBeVisible();
	await expect(
		them
			.getByRole("list", { name: "Books in this collection" })
			.getByRole("listitem")
			.getByRole("link"),
	).toHaveText(endingIn(third, second));
	await expect(
		them.getByRole("button", { name: "Edit collection" }),
	).toHaveCount(0);
	await expect(them.getByRole("button", { name: /^Move / })).toHaveCount(0);
	await theirs.close();

	await page.getByRole("button", { name: "Edit collection" }).click();
	await form.getByRole("button", { name: "Delete collection" }).click();
	await form.getByRole("button", { name: "Yes, delete" }).click();
	await expect(
		page.getByRole("heading", { name: "Collections", level: 1 }),
	).toBeVisible();
	await page.reload();
	await expect(
		page.getByRole("heading", { name: "Collections", level: 1 }),
	).toBeVisible();
	await expect(
		page.getByRole("link", { name: new RegExp(`^${renamed}`) }),
	).toHaveCount(0);
	await page.goto(`/collections/${collection}`);
	await expect(page.getByText("There is no such collection.")).toBeVisible();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
