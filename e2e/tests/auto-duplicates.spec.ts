import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test("the duplicates page offers a dry run per class, and the dry run shows under Changes without having changed a book", async ({
	page,
}, testInfo) => {
	test.setTimeout(120_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Rules ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const ids: string[] = [];
	for (const title of [`First ${stamp}`, `Second ${stamp}`]) {
		const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
			multipart: {
				file: {
					name: `${title}.epub`,
					mimeType: "application/epub+zip",
					buffer: makeEpub(title, 2, `${title} tag`, false, { 1: `Only in ${stamp}.` }),
				},
			},
		});
		ids.push((await uploaded.json()).bookId);
	}

	const said = `First ${stamp} and Second ${stamp} have the same content, with other details.`;
	const pairs = page.getByRole("region", { name: "Pairs" });
	await expect(async () => {
		await page.goto(`/duplicates?library=${library}`);
		await expect(pairs.getByRole("listitem", { name: said })).toBeVisible({ timeout: 2_000 });
	}).toPass({ timeout: 30_000 });

	const panel = page.getByRole("region", { name: "Resolve certain duplicates automatically" });
	const same = panel.getByRole("listitem", { name: "Same file or same text" });
	await expect(same).toContainText("Made one book");
	await expect(same).toContainText(/\d+ open pairs/);
	await expect(panel.getByRole("button", { name: "Run: Same file or same text", exact: true })).toBeEnabled();
	await panel.getByRole("button", { name: "Run: Same file or same text", exact: true }).click();
	const dialog = page.getByRole("alertdialog");
	await expect(dialog).toContainText(/Resolve \d+ pairs: Same file or same text\?/);
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toBeHidden();

	await panel.getByRole("button", { name: "Dry run: Same file or same text", exact: true }).click();
	await expect(panel.getByRole("status")).toContainText("The dry run has started.");
	await panel.getByRole("link", { name: "See it under Changes" }).click();

	await expect(page.getByRole("heading", { name: "Changes", level: 1 })).toBeVisible();
	const run = page
		.getByRole("list", { name: "Runs" })
		.getByRole("listitem")
		.filter({ has: page.getByRole("button", { name: "Hide changes" }) });
	await expect(run).toContainText("Duplicates, resolved by the rules");
	await expect(run).toContainText("Dry run");
	await expect(run).toContainText("Planned, not done", { timeout: 60_000 });
	await expect(run.getByRole("button", { name: /^Undo the run of/ })).toHaveCount(0);
	const actions = run.getByRole("list", { name: "Changes of the run" }).getByRole("listitem");
	await expect(actions.first()).toBeVisible();
	await expect(run.getByRole("button", { name: /^Undo the change to/ })).toHaveCount(0);

	for (const id of ids) {
		const book = await (await page.request.get(`/api/v1/books/${id}`)).json();
		expect(book.id).toBe(id);
		expect(book.files).toHaveLength(1);
	}
	expect((await page.request.delete(`/api/v1/libraries/${library}`)).status()).toBe(204);
});
