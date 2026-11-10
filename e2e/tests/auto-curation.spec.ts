import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test("a dry run of the automatic clean-up shows what it would do under Changes and changes nothing", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name.replace(/\W/g, "")}${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Auto ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const title = `Zugabe ${stamp} (German Edition)`;
	const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
		multipart: {
			file: {
				name: `${title}.epub`,
				mimeType: "application/epub+zip",
				buffer: makeEpub(title, 1, `tag ${stamp}`, false),
			},
		},
	});
	const book = (await uploaded.json()).bookId as string;
	const edited = await page.request.patch(`/api/v1/books/${book}`, {
		data: { title, locks: { title: false } },
	});
	expect(edited.status()).toBe(200);

	await page.goto("/cleanup?kind=titles");
	const panel = page.getByRole("region", { name: "Apply by itself" });
	await expect(panel).toContainText(/\d+ suggestions on \d+ books/);

	await panel.getByRole("button", { name: "Run: title additions", exact: true }).click();
	await expect(panel).toContainText("Run it on");
	await panel.getByRole("button", { name: "Cancel" }).click();
	await expect(panel.getByRole("button", { name: "Dry run: title additions" })).toBeVisible();

	await panel.getByRole("button", { name: "Dry run: title additions" }).click();
	await expect(panel.getByRole("status")).toContainText("The dry run is queued");
	await panel.getByRole("link", { name: "See it under Changes" }).click();
	await expect(page).toHaveURL(/\/curation\?run=/);

	const run = page.getByRole("listitem", { name: /^Clean-up / }).first();
	await expect(run).toContainText("Dry run");
	await expect(run).toContainText("Done", { timeout: 30_000 });
	await expect(run.getByRole("button", { name: /^Undo the run/ })).toHaveCount(0);
	await expect(run.getByRole("button", { name: /^Confirm the run/ })).toHaveCount(0);
	await expect(run.getByRole("list", { name: "Changes of the run" })).toContainText(
		"Would change",
	);

	const after = await (await page.request.get(`/api/v1/books/${book}`)).json();
	expect(after.title).toBe(title);
});
