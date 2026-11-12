import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { gotoSignedIn, openSignedIn } from "../fixtures/app";

test("a Calibre library is previewed, then imported, and its metadata arrives", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const folder = `/fixtures/calibre-${testInfo.project.name}`;
	await openSignedIn(page);

	// An earlier attempt may have added the folder already, and a folder
	// belongs to one library.
	const known = (await (await page.request.get("/api/v1/libraries")).json()).libraries as {
		id: string;
		rootPath?: string;
		name: string;
	}[];
	let library = known.find((l) => l.rootPath === folder);
	if (!library) {
		const created = await page.request.post("/api/v1/libraries", {
			data: {
				name: `Calibre ${testInfo.project.name} ${Date.now()}`,
				mode: "external",
				rootPath: folder,
				// Private: its books (one by "Unknown") would otherwise turn up
				// in what other tests' editors see, such as the clean-up's.
				visibility: "private",
			},
		});
		expect(created.status()).toBe(201);
		library = await created.json();
	}
	const name = library?.name ?? "";
	await expect
		.poll(
			async () => {
				const list = (await (await page.request.get("/api/v1/libraries")).json()).libraries as {
					id: string;
					filesPending: number;
					lastScan?: { state: string };
				}[];
				const mine = list.find((l) => l.id === library?.id);
				return mine?.lastScan?.state === "done" && mine.filesPending === 0;
			},
			{ timeout: 40_000 },
		)
		.toBe(true);

	await gotoSignedIn(page, "/admin/libraries");
	await page.getByRole("link", { name: "Import from Calibre" }).click();
	await expect(page.getByRole("heading", { name: "Import from Calibre", level: 1 })).toBeVisible();

	await page.getByRole("combobox", { name: "Calibre library" }).selectOption({ label: name });
	await page.getByRole("button", { name: "Preview" }).click();
	const preview = page.getByRole("region", { name: "Import from Calibre" });
	await expect(preview.getByText("4 books in Calibre")).toBeVisible();
	await expect(preview.getByText("3 books with a file in GOtome, to be described")).toBeVisible();
	await expect(preview.getByText("1 books left out, as none of their files is in GOtome")).toBeVisible();
	await expect(preview.getByText(/^Die Brücke am Fluss: /)).toBeVisible();
	await expect(preview.getByText("Files by format: epub 2, pdf 2")).toBeVisible();
	await expect(preview.getByText("1 covers")).toBeVisible();
	await expect(preview.getByText("1 series")).toBeVisible();
	await expect(preview.getByText("3 tags")).toBeVisible();
	await expect(preview.getByText("Read (bool, 2 books)")).toBeVisible();
	await expect(preview.getByText("Shelf (text, 1 books)")).toBeVisible();
	await expect(preview.getByText("Finished (datetime, 1 books)")).toBeVisible();

	await preview.getByLabel("Shelf (text, 1 books)").selectOption("Tags");
	await preview.getByLabel("Take Calibre's ratings as my own").check();
	await preview.getByRole("button", { name: "Import" }).click();
	await expect(preview.getByText("Done.")).toBeVisible({ timeout: 40_000 });
	await expect(preview.getByText("3 books changed")).toBeVisible();

	// The books are described as Calibre says, whatever their files hold.
	const found = await page.request.get(`/api/v1/books?library=${library?.id}&q=Lantern`);
	expect(found.status()).toBe(200);
	const book = (await found.json()).books.find((b: { title: string }) => b.title === "The Lantern at Dusk");
	expect(book).toBeTruthy();
	await page.goto(`/books/${book.id}`);
	await expect(page.getByRole("heading", { name: "The Lantern at Dusk", level: 1 })).toBeVisible();
	await expect(page.getByText("Henrik Halden").first()).toBeVisible();
	await expect(page.getByText("Brinewick Press").first()).toBeVisible();
	await expect(page.getByText("favourites").first()).toBeVisible();
});
