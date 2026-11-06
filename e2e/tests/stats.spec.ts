import { expect, type Page, test } from "@playwright/test";
import {
	makeAccount,
	makeLibrary,
	signedIn,
	uploadFile,
} from "../fixtures/accounts";
import { openSignedIn } from "../fixtures/app";
import { makePdf, proseOnEveryPage } from "../fixtures/pdf";

/** The value the overview shows under a label. */
const figure = (page: Page, label: string) =>
	page.locator(`dt:text-is("${label}") + dd`);

test("a person's statistics show what they started and finished, and nobody else's", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(45_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const title = `Logbook ${stamp}`;
	await openSignedIn(page);
	// An account of its own: statistics are only ever the reader's.
	const reader = await makeAccount(page.request, "reader", stamp);
	const library = await makeLibrary(page.request, `Stats ${stamp}`, "shared");
	const { bookId, fileId } = await uploadFile(
		page.request,
		library,
		`${title}.pdf`,
		"application/pdf",
		makePdf(10, 0, stamp),
	);
	const { context, page: them } = await signedIn(browser, testInfo, reader);

	await them.goto("/stats");
	await expect(
		them.getByRole("heading", { name: "Your reading", level: 1 }),
	).toBeVisible();
	await expect(them.getByText("Nothing started yet.")).toBeVisible();
	await expect(them.getByText("No book read to its end yet.")).toBeVisible();

	for (const [n, fraction] of [
		[5, 0.5],
		[10, 1],
	] as const) {
		const put = await them.request.put(
			`/api/v1/books/${bookId}/progress/ebook`,
			{
				data: {
					fileId,
					locator: `page:${n}`,
					fraction,
					clientId: `stats-${stamp}`,
				},
			},
		);
		expect((await put.json()).saved).toBe(true);
	}

	await them.reload();
	const history = them.getByRole("region", { name: "Started and finished" });
	await expect(
		history
			.getByRole("listitem")
			.filter({ hasText: "Started" })
			.getByRole("link", { name: title }),
	).toBeVisible();
	await expect(
		history
			.getByRole("listitem")
			.filter({ hasText: "Finished" })
			.getByRole("link", { name: title }),
	).toBeVisible();
	const year = String(new Date().getFullYear());
	await expect(
		them
			.getByRole("region", { name: "Books finished per year" })
			.getByRole("listitem"),
	).toHaveText(`${year}: 1 books`);
	await them.getByRole("button", { name: "7 days" }).click();
	await expect(them.getByRole("button", { name: "7 days" })).toHaveAttribute(
		"aria-pressed",
		"true",
	);
	await expect(them.getByRole("region", { name: "Day by day" })).toBeVisible();
	await context.close();

	// The administrator's own page does not show the reader's book.
	await page.goto("/stats");
	await expect(
		page.getByRole("heading", { name: "Your reading", level: 1 }),
	).toBeVisible();
	await expect(page.getByRole("link", { name: title })).toHaveCount(0);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("the overview counts what a library holds, and shows the background work only to whoever may watch it", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(45_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const name = `Overview ${stamp}`;
	await openSignedIn(page);
	const library = await makeLibrary(page.request, name, "shared");
	await uploadFile(
		page.request,
		library,
		`Short ${stamp}.pdf`,
		"application/pdf",
		makePdf(2, 0, `${stamp} a`, proseOnEveryPage(2)),
	);
	await uploadFile(
		page.request,
		library,
		`Shorter ${stamp}.pdf`,
		"application/pdf",
		makePdf(3, 0, `${stamp} b`, proseOnEveryPage(3)),
	);
	// Pages are counted once the files are read.
	await expect
		.poll(
			async () =>
				(
					await (
						await page.request.get(`/api/v1/statistics?library=${library}`)
					).json()
				).pages,
			{ timeout: 20_000 },
		)
		.toBe(5);

	await page.goto("/overview");
	await expect(
		page.getByRole("heading", { name: "Overview", level: 1 }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Background work" }),
	).toBeVisible();
	await page
		.getByRole("combobox", { name: "Library", exact: true })
		.selectOption({ label: name });
	await expect(figure(page, "Books")).toHaveText("2");
	await expect(figure(page, "Files")).toHaveText("2");
	await expect(figure(page, "Pages")).toHaveText("5");
	await expect(figure(page, "Audiobooks")).toHaveText("0");
	await expect(
		page
			.getByRole("region", { name: "Files by format" })
			.getByRole("listitem")
			.filter({ hasText: "PDF" }),
	).toContainText("2");

	const reader = await makeAccount(page.request, "reader", stamp);
	const { context, page: them } = await signedIn(browser, testInfo, reader);
	await them.goto("/overview");
	await expect(
		them.getByRole("heading", { name: "What the library holds" }),
	).toBeVisible();
	await expect(
		them.getByRole("heading", { name: "Background work" }),
	).toHaveCount(0);
	await context.close();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
