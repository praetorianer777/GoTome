import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { mainNav, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { projectWord } from "../fixtures/library";

test("an author is followed from a book's page, listed on the new books page, and let go again", async ({
	page,
}, testInfo) => {
	// The browser projects share one account: each follows an author of its own.
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const author = `Writer, Wanda ${stamp}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Follow ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: {
					name: `Followed ${stamp}.epub`,
					mimeType: "application/epub+zip",
					buffer: makeEpub(`Followed ${stamp}`, 1, `tag ${stamp}`, false),
				},
			},
		},
	);
	const book = (await uploaded.json()).bookId as string;
	const edited = await page.request.patch(`/api/v1/books/${book}`, {
		data: { contributors: [{ name: author, role: "author" }] },
	});
	expect(edited.status()).toBe(200);

	await page.goto(`/books/${book}`);
	await page.getByRole("button", { name: author, exact: true }).click();
	await expect(
		page.getByRole("button", { name: `✓ ${author}` }),
	).toHaveAttribute("aria-pressed", "true");

	await (await mainNav(page)).getByRole("link", { name: "New books" }).click();
	const following = page.getByRole("region", { name: "You follow" });
	await expect(following.getByText(author, { exact: true })).toBeVisible();
	await following
		.getByRole("button", { name: `Stop following ${author}` })
		.click();
	await expect(following.getByText(author, { exact: true })).toBeHidden();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("an author followed by name has their announced book listed as coming", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	// fixtures/metadata.json lists a book of 2099 by this author, a name of
	// each browser project's own.
	const word = projectWord(testInfo);
	const author = `Wren Calloway ${word}`;
	await openSignedIn(page);
	await page.goto("/releases");
	const following = page.getByRole("region", { name: "You follow" });
	await following.getByRole("combobox", { name: "What" }).selectOption("author");
	await following.getByRole("textbox", { name: "Name" }).fill(author);
	await following.getByRole("button", { name: "Follow", exact: true }).click();
	await expect(following.getByText(author, { exact: true })).toBeVisible();

	const coming = page.getByRole("region", { name: "Coming" });
	const book = coming
		.getByRole("listitem")
		.filter({ hasText: `Comet Over Kestrel ${word}` });
	await expect(async () => {
		await page.reload();
		await expect(book).toBeVisible({ timeout: 1_000 });
	}).toPass({ timeout: 40_000 });
	await expect(book).toContainText(`Through ${author}`);
	await expect(book).toContainText("Listed by canned");
	// Of this author: the browser projects share the account, and with it
	// what is followed.
	await expect(
		following
			.getByRole("listitem")
			.filter({ hasText: author })
			.getByText(/^Author · Looked up /),
	).toBeVisible();

	await following
		.getByRole("button", { name: `Stop following ${author}` })
		.click();
	await expect(following.getByText(author, { exact: true })).toBeHidden();
	await expect(book).toBeHidden();
});

// The stack is offline, and feeds are fetched only from public addresses,
// so no feed is read here: reading one, its failures and trying again are
// the integration tests' (test/feeds_test.go).
test("a feed is followed with its filters and let go again", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const url = `https://feeds.example.org/${stamp}.xml`;
	await openSignedIn(page);
	await page.goto("/releases");
	const feeds = page.getByRole("region", { name: "Feeds" });
	await feeds.getByRole("textbox", { name: "Feed address" }).fill(url);
	await feeds.getByRole("textbox", { name: "Words" }).fill("lighthouse, harbour");
	await feeds.getByRole("textbox", { name: "Authors" }).fill("Nora Vale");
	await feeds.getByRole("button", { name: "Follow the feed" }).click();

	const feed = feeds.getByRole("listitem").filter({ hasText: url });
	await expect(feed).toContainText("Naming lighthouse, harbour · By Nora Vale");
	await expect(feed).toContainText("Not read yet");
	await expect(feeds.getByRole("textbox", { name: "Feed address" })).toHaveValue("");

	await feed.getByRole("button", { name: `Stop following ${url}` }).click();
	await expect(feed).toBeHidden({ timeout: 15_000 });
});
