import { expect, test } from "@playwright/test";
import { openSignedIn, signIn } from "../fixtures/app";
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
	if ((page.viewportSize()?.width ?? 0) >= 768) {
		const row = await title.boundingBox();
		const buttons = await title
			.getByRole("button")
			.filter({ hasNotText: `Drittes ${stamp}` })
			.evaluateAll((all) =>
			all.map((b) => b.getBoundingClientRect()),
		);
		expect(buttons).toHaveLength(3);
		for (const b of buttons) {
			expect(b.top).toBeGreaterThanOrEqual(row?.y ?? 0);
			expect(b.bottom).toBeLessThanOrEqual((row?.y ?? 0) + (row?.height ?? 0));
		}
		expect(buttons[0]?.top).toBe(buttons[1]?.top);
		expect(buttons[1]?.top).toBe(buttons[2]?.top);
		expect(row?.height).toBeLessThan(90);
	}
	await title.getByRole("button", { name: "Show the books" }).click();
	await expect(title.locator("del")).toHaveText(
		`Drittes ${stamp} (German Edition)`,
	);
	await expect(title.locator("ins")).toHaveText(`Drittes ${stamp}`);
	await expect(title.getByText("Books this changes: 1")).toBeVisible();
	await expect(title.getByText(/No provider answer is kept/)).toBeVisible();
	await title.getByRole("button", { name: "Hide the books" }).click();
	await expect(title.locator("del")).toHaveCount(0);
	const dismissed = page.waitForResponse("**/api/v1/cleanup/dismiss");
	await title.getByRole("button", { name: "Leave it" }).click();
	await expect(title).toHaveCount(0);
	expect((await dismissed).status()).toBe(204);
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

test("every suggestion of a kind is applied at once", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const username = `cleaner-${stamp}`;
	const password = `pw-${stamp}`;
	await openSignedIn(page);
	// "Apply all" applies to every book its person sees, and every project
	// leaves books by "Unknown": an editor who sees only this library keeps
	// it to this test's.
	const made = await page.request.post("/api/v1/users", {
		data: { username, password, role: "editor" },
	});
	expect(made.status()).toBe(201);
	const editor = (await made.json()).id as string;
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Unknown ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	expect(
		(
			await page.request.put(`/api/v1/libraries/${library}/members/${editor}`)
		).status(),
	).toBe(204);
	const books: string[] = [];
	for (const title of [`Nameless ${stamp}`, `Authorless ${stamp}`]) {
		const uploaded = await page.request.post(
			`/api/v1/libraries/${library}/uploads`,
			{
				multipart: {
					file: {
						name: `${title}.epub`,
						mimeType: "application/epub+zip",
						buffer: makeEpub(title, 1, `unknown ${title}`, false),
					},
				},
			},
		);
		const book = (await uploaded.json()).bookId as string;
		const edited = await page.request.patch(`/api/v1/books/${book}`, {
			data: {
				title,
				contributors: [{ name: "Unknown", role: "author" }],
				locks: { title: false, contributors: false },
			},
		});
		expect(edited.status()).toBe(200);
		books.push(book);
	}

	const theirs = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await theirs.newPage();
	await them.goto("/cleanup?kind=placeholders");
	await signIn(them, { username, password });
	await expect(
		them.getByRole("listitem").filter({ hasText: "“Unknown” is not a name." }),
	).toContainText("Books: 2", { timeout: 15_000 });
	await them.getByRole("button", { name: "Apply all 1" }).click();
	await expect(them.getByText("Apply all 1 as suggested?")).toBeVisible();
	await them.getByRole("button", { name: "Apply all", exact: true }).click();
	await expect(them.getByText("2 of 2 books done.")).toBeVisible({
		timeout: 20_000,
	});
	await theirs.close();

	for (const book of books) {
		const detail = await (await page.request.get(`/api/v1/books/${book}`)).json();
		expect(detail.contributors).toEqual([]);
	}
	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("a name that is both author and series is asked about, never cleared by default", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name.replace(/\W/g, "")}${Date.now()}`;
	const name = `Hausname ${stamp}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Clash ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const books: Record<string, string> = {};
	for (const [title, series] of [
		[`Der Erste ${stamp}`, 1],
		[`Der Zweite ${stamp}`, 2],
		[`Der Dritte ${stamp}`, undefined],
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
		const edited = await page.request.patch(`/api/v1/books/${book}`, {
			data: {
				title,
				contributors: [{ name, role: "author" }],
				...(series ? { series: { name, index: series } } : {}),
				locks: { title: false, contributors: false, series: false },
			},
		});
		expect(edited.status()).toBe(200);
		books[title] = book;
	}

	await page.goto("/cleanup?kind=clashes");
	const clash = page.getByRole("listitem").filter({ hasText: name });
	await expect(clash.getByRole("radio")).toHaveCount(2);
	await expect(clash.getByRole("radio", { checked: true })).toHaveCount(0);
	await expect(
		clash.getByRole("button", { name: "Choose which it is" }),
	).toBeDisabled();

	await clash.getByRole("button", { name: "Show the books" }).last().click();
	await expect(clash.locator("del").first()).toContainText(name);
	await expect(clash.getByText("Books this changes: 2")).toBeVisible();
	await clash.getByRole("button", { name: "Show the books" }).first().click();
	await expect(clash.getByText("Books this changes: 3")).toBeVisible();

	await clash.getByRole("radio").first().check();
	await clash.getByRole("button", { name: "Make it the series" }).click();
	await expect(page.getByRole("status")).toContainText(
		"Being applied to 3 books.",
		{ timeout: 15_000 },
	);
	await expect(async () => {
		const book = await (
			await page.request.get(`/api/v1/books/${books[`Der Dritte ${stamp}`]}`)
		).json();
		expect(book.series).toBe(name);
		expect(book.contributors ?? []).toEqual([]);
	}).toPass({ timeout: 20_000 });
	const first = await (
		await page.request.get(`/api/v1/books/${books[`Der Erste ${stamp}`]}`)
	).json();
	expect(first.series).toBe(name);
	expect(first.seriesIndex).toBe(1);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
