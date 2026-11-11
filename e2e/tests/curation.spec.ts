import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test("a merge shows under Changes with its before and after, and undoing the run brings both books back", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Changes ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const ids: string[] = [];
	for (const title of [`Kept ${stamp}`, `Merged ${stamp}`]) {
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
	const [kept, merged] = ids as [string, string];

	const said = `Kept ${stamp} and Merged ${stamp} have the same content, with other details.`;
	const pairs = page.getByRole("region", { name: "Pairs" });
	await expect(async () => {
		await page.goto(`/duplicates?library=${library}`);
		await expect(pairs.getByRole("listitem", { name: said })).toBeVisible({ timeout: 2_000 });
	}).toPass({ timeout: 30_000 });
	const pair = pairs.getByRole("listitem", { name: said });
	await pair.getByRole("button", { name: "Merge…" }).click();
	const form = pair.getByRole("form", { name: "Merge the two books" });
	await form.getByRole("radio", { name: `Kept ${stamp}, files: 1` }).check();
	await form.getByRole("radiogroup", { name: "Title" }).getByRole("radio", { name: `Merged ${stamp}` }).check();
	await form.getByRole("button", { name: `Merge into Kept ${stamp}` }).click();
	await expect(pairs.getByRole("listitem", { name: said })).toBeHidden();

	// Other specs make runs on this stack at the same time, so the page
	// is opened at the one this merge made.
	const made = await (await page.request.get("/api/v1/curation/runs")).json();
	const mine = made.runs.find((r: { kind: string; dryRun: boolean }) => r.kind === "duplicates" && !r.dryRun);
	await page.goto(`/curation?run=${mine.id}`);
	await expect(page.getByRole("heading", { name: "Changes", level: 1 })).toBeVisible();
	const run = page
		.getByRole("list", { name: "Runs" })
		.getByRole("listitem")
		.filter({ has: page.getByRole("button", { name: "Hide changes" }) });
	await expect(run).toContainText("Duplicates, by");
	await expect(run).toContainText("Done");
	const action = run.getByRole("list", { name: "Changes of the run" }).getByRole("listitem").first();
	await expect(action).toContainText("Merged");
	const title = action.getByRole("row", { name: /^Title/ });
	await expect(title).toContainText(`Kept ${stamp}`);
	await expect(title).toContainText(`Merged ${stamp}`);
	await expect(action.getByRole("row", { name: "Files Before: 1 After: 2" })).toBeVisible();

	await run.getByRole("button", { name: /^Undo the run of/ }).click();
	// A job undoes the run; the page follows it.
	await expect(run).toContainText("Undone", { timeout: 30_000 });

	for (const [id, title] of [[kept, `Kept ${stamp}`], [merged, `Merged ${stamp}`]] as const) {
		const book = await (await page.request.get(`/api/v1/books/${id}`)).json();
		expect(book.id).toBe(id);
		expect(book.title).toBe(title);
		expect(book.files).toHaveLength(1);
	}

	expect((await page.request.delete(`/api/v1/libraries/${library}`)).status()).toBe(204);
});
