import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test("an author's two spellings are made one, and a title's addition is left as it is", async ({
	page,
}, testInfo) => {
	// The browser projects share one account: each has a name of its own.
	const stamp = `${testInfo.project.name.replace(/\W/g, "")}${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Clean ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const books: Record<string, string> = {};
	for (const [title, author] of [
		[`Erstes ${stamp}`, `Wortmann, Wanda ${stamp}`],
		[`Zweites ${stamp}`, `Wanda ${stamp} Wortmann`],
		[`Drittes ${stamp} (German Edition)`, `Wortmann, Wanda ${stamp}`],
	] as const) {
		const uploaded = await page.request.post(
			`/api/v1/libraries/${library}/uploads`,
			{
				multipart: {
					file: {
						name: `${title}.epub`,
						mimeType: "application/epub+zip",
						buffer: makeEpub(title, 1, `tag ${stamp}`, false),
					},
				},
			},
		);
		const book = (await uploaded.json()).bookId as string;
		// Unlocked, as a scan leaves what a file says: a fix leaves a field
		// someone locked as it is.
		const edited = await page.request.patch(`/api/v1/books/${book}`, {
			data: {
				title,
				contributors: [{ name: author, role: "author" }],
				locks: { title: false, contributors: false },
			},
		});
		expect(edited.status()).toBe(200);
		books[title] = book;
	}

	await page.goto("/cleanup");
	const spellings = page
		.getByRole("listitem")
		.filter({ hasText: `Wanda ${stamp} Wortmann` });
	await expect(
		spellings.getByRole("radio", {
			name: new RegExp(`^Wortmann, Wanda ${stamp} Books: 2`),
		}),
	).toBeChecked();
	await spellings
		.getByRole("radio", { name: new RegExp(`^Wanda ${stamp} Wortmann`) })
		.check();
	await spellings.getByRole("button", { name: "Use this spelling" }).click();
	await expect(page.getByRole("status")).toContainText(
		"Being applied to 2 books.",
		{ timeout: 15_000 },
	);
	await expect(spellings).toHaveCount(0);

	await page.getByRole("link", { name: "See how it goes" }).click();
	await expect(page.getByText("2 of 2 books done.")).toBeVisible({
		timeout: 15_000,
	});
	for (const title of [
		`Erstes ${stamp}`,
		`Drittes ${stamp} (German Edition)`,
	]) {
		const book = await (
			await page.request.get(`/api/v1/books/${books[title]}`)
		).json();
		expect(book.contributors.map((c: { name: string }) => c.name)).toEqual([
			`Wanda ${stamp} Wortmann`,
		]);
	}

	await page.goto("/cleanup?kind=titles");
	const title = page
		.getByRole("listitem")
		.filter({ hasText: `Drittes ${stamp} (German Edition)` });
	await expect(title).toContainText(`becomes Drittes ${stamp}`);
	await title.getByRole("button", { name: "Leave it" }).click();
	await expect(title).toHaveCount(0);
	await page.reload();
	await expect(
		page.getByRole("button", { name: /^Title additions:/ }),
	).toHaveAttribute("aria-pressed", "true");
	await expect(
		page.getByRole("listitem").filter({ hasText: `Drittes ${stamp}` }),
	).toHaveCount(0);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
