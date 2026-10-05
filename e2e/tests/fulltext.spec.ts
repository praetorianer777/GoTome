import { type Page, expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { makePdf } from "../fixtures/pdf";

// Laying a book out keeps a loaded WebKit busy enough in CI that its
// controls take more than the usual five seconds to settle for a click.
test.use({ actionTimeout: 15_000 });

/** A word of letters alone that no other test's books hold. */
function uniqueWord(project: string): string {
	return `zq${project}${Date.now().toString(36)}`
		.toLowerCase()
		.replace(/[^a-z0-9]/g, "")
		.replace(/\d/g, (d) => "abcdefghij"[Number(d)] ?? "");
}

async function upload(
	page: Page,
	library: string,
	name: string,
	buffer: Buffer,
) {
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: { name, mimeType: "application/octet-stream", buffer },
			},
		},
	);
	expect((await uploaded.json()).outcome).toBe("added");
}

/** Ticks or unticks a value in the filters, behind their button on a phone. */
async function filter(page: Page, name: RegExp, on: boolean) {
	const toggle = page.getByRole("button", { name: /^Filters/ });
	const panel = page.getByRole("complementary", { name: "Filters" });
	if ((await toggle.isVisible()) && !(await panel.isVisible())) {
		await toggle.click();
	}
	await panel.getByRole("checkbox", { name }).setChecked(on);
}

test("the text is searched from the header, narrowed by a filter, and a passage opens in its reader", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const word = uniqueWord(testInfo.project.name);
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Text ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	await upload(
		page,
		library,
		`Harbour ${stamp}.epub`,
		makeEpub(`Harbour ${stamp}`, 3, stamp, false, {
			2: `The ${word} sang beneath the harbour lights.`,
		}),
	);
	await upload(
		page,
		library,
		`Ledger ${stamp}.pdf`,
		// Every page needs text enough not to be taken for a scan.
		makePdf(
			12,
			0,
			stamp,
			Object.fromEntries(
				Array.from({ length: 12 }, (_, i) => [
					i + 1,
					i === 6
						? `The ${word} counted the coins twice before the ledger was closed for the night.`
						: "The clerk counted the coins twice before the ledger was closed for the night.",
				]),
			),
		),
	);

	const box = page.getByRole("combobox", { name: "Search books" });
	await box.fill(word);
	await page
		.getByRole("option", { name: `Search the text of the books for “${word}”` })
		.click();
	await expect(
		page.getByRole("heading", { name: "Search the text", level: 1 }),
	).toBeVisible();
	const results = page.getByRole("region", { name: "Books found" });
	const found = results.getByRole("heading", { level: 2 });
	// The books' text is read in the background after the upload.
	await expect(async () => {
		await page.reload();
		await expect(found).toHaveCount(2, { timeout: 2_000 });
	}).toPass({ timeout: 30_000 });
	await expect(results.locator("mark").first()).toHaveText(word);

	await filter(page, /^PDF \(/, true);
	await expect(found).toHaveCount(1);
	await expect(found).toHaveText(new RegExp(`^Ledger ${stamp}`));
	await expect(page).toHaveURL(new RegExp(`[?&]q=${word}`));
	await expect(page).toHaveURL(/[?&]format=[^&]*pdf/);
	await page.reload();
	await expect(found).toHaveCount(1);
	await expect(
		page.getByRole("searchbox", { name: "Words to look for" }),
	).toHaveValue(word);

	await results.getByRole("link", { name: "Read from here" }).first().click();
	await expect(
		page.getByRole("textbox", { name: "Page number" }),
	).toHaveValue("7");

	await page.goBack();
	await filter(page, /^PDF \(/, false);
	await filter(page, /^EPUB \(/, true);
	await expect(found).toHaveText(new RegExp(`^Harbour ${stamp}`));
	await results.getByRole("link", { name: "Read from here" }).first().click();
	await expect(
		page.getByRole("region", { name: `Pages of Harbour ${stamp}` }),
	).toHaveAttribute("data-chapter", "Chapter 2", { timeout: 20_000 });

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
