import { expect, test } from "@playwright/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";

test("a smart shelf is built from rules with a live count, edited and deleted", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	// One word: a tag is compared by its key, and every browser project
	// shares the administrator, whose shelves see every project's books.
	const tag = `shelf${testInfo.project.name}${Date.now()}`;
	const [done, waiting, other] = ["Done", "Waiting", "Other"].map(
		(n) => `${n} ${stamp}`,
	) as [string, string, string];
	const name = `Finished ones ${stamp}`;
	await openSignedIn(page);
	const { id, books } = await libraryOfBooks(page, `Smart ${stamp}`, [
		done,
		waiting,
		other,
	]);
	for (const title of [done, waiting]) {
		expect(
			(
				await page.request.patch(`/api/v1/books/${books[title]}`, {
					data: { tags: [tag] },
				})
			).status(),
		).toBe(200);
	}
	expect(
		(
			await page.request.put(`/api/v1/books/${books[done]}/reading`, {
				data: { status: "completed" },
			})
		).status(),
	).toBe(200);

	await page.goto("/collections");
	await page.getByRole("link", { name: "New smart shelf" }).click();
	const form = page.getByRole("form", { name: "New smart shelf" });
	await form.getByRole("textbox", { name: "Name" }).fill(name);
	const first = form.getByRole("group", { name: "Rule 1 of Rules" });
	await first.getByRole("combobox", { name: "Field" }).selectOption({
		label: "Tag",
	});
	await first.getByRole("textbox", { name: "Values" }).fill(tag);
	const count = form.getByRole("status");
	await expect(count).toHaveText("Books matching now: 2");

	await form.getByRole("button", { name: "Add a rule" }).click();
	const second = form.getByRole("group", { name: "Rule 2 of Rules" });
	await second.getByRole("combobox", { name: "Field" }).selectOption({
		label: "My status",
	});
	await second.getByRole("checkbox", { name: "Completed" }).check();
	await expect(count).toHaveText("Books matching now: 1");
	await form.getByRole("button", { name: "Create" }).click();

	await expect(page.getByRole("heading", { name, level: 1 })).toBeVisible();
	const shelf = page.url().split("/").pop() ?? "";
	const shown = page.getByRole("list", { name: "Books on this shelf" });
	await expect(shown.getByRole("link")).toHaveCount(1);
	await expect(shown.getByRole("link", { name: done })).toBeVisible();
	await page.reload();
	await expect(shown.getByRole("link")).toHaveCount(1);
	await expect(page.getByText("Books: 1 · Only you")).toBeVisible();

	// The shelf follows the books: another one finished is on it at once.
	expect(
		(
			await page.request.put(`/api/v1/books/${books[waiting]}/reading`, {
				data: { status: "completed" },
			})
		).status(),
	).toBe(200);
	await page.reload();
	await expect(shown.getByRole("link")).toHaveCount(2);

	await page.getByRole("button", { name: "Edit smart shelf" }).click();
	const edit = page.getByRole("form", { name: "Edit smart shelf" });
	await expect(edit.getByRole("status")).toHaveText("Books matching now: 2");
	await edit
		.getByRole("group", { name: "Rule 2 of Rules" })
		.getByRole("checkbox", { name: "Completed" })
		.uncheck();
	await edit
		.getByRole("group", { name: "Rule 2 of Rules" })
		.getByRole("checkbox", { name: "Unread" })
		.check();
	await expect(edit.getByRole("status")).toHaveText("Books matching now: 0");
	await edit
		.getByRole("group", { name: "Rule 2 of Rules" })
		.getByRole("button", { name: "Remove this rule" })
		.click();
	await expect(edit.getByRole("status")).toHaveText("Books matching now: 2");
	await edit.getByRole("textbox", { name: "Name" }).fill(`Tagged ${stamp}`);
	await edit.getByRole("button", { name: "Save" }).click();
	await expect(edit).toBeHidden();
	await page.reload();
	await expect(
		page.getByRole("heading", { name: `Tagged ${stamp}`, level: 1 }),
	).toBeVisible();
	await expect(shown.getByRole("link")).toHaveCount(2);
	await page.getByRole("button", { name: "Edit smart shelf" }).click();
	await expect(
		edit.getByRole("group", { name: "Rule 2 of Rules" }),
	).toHaveCount(0);
	await expect(
		edit
			.getByRole("group", { name: "Rule 1 of Rules" })
			.getByRole("textbox", { name: "Values" }),
	).toHaveValue(tag);

	await edit.getByRole("button", { name: "Delete smart shelf" }).click();
	await edit.getByRole("button", { name: "Yes, delete" }).click();
	await expect(
		page.getByRole("heading", { name: "Collections", level: 1 }),
	).toBeVisible();
	await page.reload();
	await expect(
		page.getByRole("link", { name: new RegExp(`^Tagged ${stamp}`) }),
	).toHaveCount(0);
	await page.goto(`/smart-shelves/${shelf}`);
	await expect(page.getByText("There is no such smart shelf.")).toBeVisible();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
