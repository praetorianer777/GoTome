import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import { chooseLibrary, openSignedIn } from "../fixtures/app";
import { makeEpub, makeShortEpub } from "../fixtures/ebooks";
import {
	deleteLibrary,
	newLibrary,
	uploadEpub,
	waitUntilRead,
} from "../fixtures/library";

/** A library of the books given, read and listed on the library page. */
async function libraryOf(
	page: Page,
	name: string,
	books: Record<string, Buffer>,
): Promise<{ library: string; ids: Record<string, string> }> {
	const library = await newLibrary(page, name);
	const ids: Record<string, string> = {};
	for (const [title, buffer] of Object.entries(books)) {
		ids[title] = await uploadEpub(page, library, title, buffer);
	}
	for (const id of Object.values(ids)) {
		await waitUntilRead(page, id);
	}
	await page.goto("/");
	await chooseLibrary(page, name);
	for (const title of Object.keys(books)) {
		await expect(page.getByRole("link", { name: title })).toBeVisible();
	}
	await page.getByRole("button", { name: "Select books" }).click();
	return { library, ids };
}

const bar = (page: Page) =>
	page.getByRole("region", { name: "Change many books" });

test("many books are edited at once, and the change's page counts them", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const titles = [`Bulk one ${stamp}`, `Bulk two ${stamp}`];
	await openSignedIn(page);
	const { library, ids } = await libraryOf(
		page,
		`Bulk ${stamp}`,
		Object.fromEntries(titles.map((t) => [t, makeEpub(t, 1, `bulk ${t}`)])),
	);

	await bar(page)
		.getByRole("button", { name: "Select every book that matches" })
		.click();
	await expect(bar(page)).toContainText("Every book that matches is selected");
	await bar(page).getByRole("button", { name: "Edit" }).click();
	await bar(page)
		.getByRole("combobox", { name: "What to do with Publisher" })
		.selectOption({ label: "Set to" });
	await bar(page)
		.getByRole("textbox", { name: "Value for Publisher" })
		.fill(`Press ${stamp}`);
	await bar(page)
		.getByRole("combobox", { name: "What to do with Tags" })
		.selectOption({ label: "Add" });
	await bar(page)
		.getByRole("textbox", { name: "Value for Tags" })
		.fill("Bulk edited");
	await bar(page).getByRole("button", { name: "Change the books" }).click();

	await expect(
		page.getByRole("heading", { name: "Editing many books" }),
	).toBeVisible();
	await expect(page.getByText("2 of 2 books done.")).toBeVisible({
		timeout: 20_000,
	});
	await expect(page.getByText("Finished.")).toBeVisible();
	await page.getByRole("button", { name: "Changed: 2" }).click();
	await expect(
		page.getByRole("button", { name: "Changed: 2" }),
	).toHaveAttribute("aria-pressed", "true");
	for (const title of titles) {
		await expect(page.getByRole("link", { name: title })).toBeVisible();
		const book = await (
			await page.request.get(`/api/v1/books/${ids[title]}`)
		).json();
		expect(book.publisher).toBe(`Press ${stamp}`);
		expect(book.tags).toContain("Bulk edited");
	}

	await deleteLibrary(page, library);
});

test("the selected books get a status, go into a collection and onto an e-reader", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const titles = [`Shelved one ${stamp}`, `Shelved two ${stamp}`];
	await openSignedIn(page);
	const collection = await (async () => {
		const made = await page.request.post("/api/v1/collections", {
			data: { name: `Shelf ${stamp}`, visibility: "private" },
		});
		expect(made.status()).toBe(201);
		return (await made.json()).id as string;
	})();
	const device = await page.request.post("/api/v1/auth/devices", {
		data: { name: `Reader ${stamp}` },
	});
	expect(device.status()).toBe(201);
	const deviceId = (await device.json()).device.id as string;
	const { library, ids } = await libraryOf(
		page,
		`Shelved ${stamp}`,
		Object.fromEntries(titles.map((t) => [t, makeEpub(t, 1, `shelf ${t}`)])),
	);

	for (const title of titles) {
		await page.getByRole("checkbox", { name: `Select ${title}` }).check();
	}
	await expect(bar(page)).toContainText("2 selected");
	await bar(page)
		.getByRole("combobox", { name: "Set my status" })
		.selectOption({ label: "Reading" });
	await expect(bar(page).getByRole("status")).toHaveText(
		"Status set on 2 books.",
		{ timeout: 15_000 },
	);
	await bar(page)
		.getByRole("combobox", { name: "Add to collection" })
		.selectOption({ label: `Shelf ${stamp}` });
	await expect(
		bar(page).getByText(`Added to Shelf ${stamp}: 2 not in it before.`),
	).toBeVisible({ timeout: 15_000 });
	await bar(page)
		.getByRole("combobox", { name: "Send to an e-reader" })
		.selectOption({ label: `Reader ${stamp}` });
	await expect(
		bar(page).getByText(`2 books are on their way to Reader ${stamp}.`),
	).toBeVisible({ timeout: 15_000 });

	for (const title of titles) {
		const book = await (
			await page.request.get(`/api/v1/books/${ids[title]}`)
		).json();
		expect(book.reading.status).toBe("reading");
	}
	await page.goto(`/collections/${collection}`);
	for (const title of titles) {
		await expect(page.getByRole("link", { name: title })).toBeVisible();
	}
	const queued = await (
		await page.request.get(`/api/v1/devices/${deviceId}/books`)
	).json();
	expect(JSON.stringify(queued)).toContain(ids[titles[0] as string]);

	expect(
		(await page.request.delete(`/api/v1/collections/${collection}`)).status(),
	).toBe(204);
	expect(
		(await page.request.delete(`/api/v1/auth/devices/${deviceId}`)).status(),
	).toBe(204);
	await deleteLibrary(page, library);
});

test("details are written into the files, and EPUBs are fixed for e-readers", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const broken = `Lacking ${stamp}`;
	const sound = `Sound ${stamp}`;
	await openSignedIn(page);
	const { library, ids } = await libraryOf(page, `Files ${stamp}`, {
		[broken]: makeShortEpub(broken, `fix ${stamp}`, "A plate is missing.", {
			missingFile: true,
		}),
		[sound]: makeShortEpub(sound, `sound ${stamp}`, "Nothing is missing."),
	});

	// Fixed first: writing into a file changes its hash, which clears what
	// was measured of it until it is measured again.
	await page.getByRole("checkbox", { name: `Select ${broken}` }).check();
	await page.getByRole("checkbox", { name: `Select ${sound}` }).check();
	await bar(page)
		.getByRole("combobox", { name: "Fix for e-readers" })
		.selectOption({ label: "Everything found" });
	await expect(
		page.getByRole("heading", { name: "Fixing EPUBs for e-readers" }),
	).toBeVisible();
	await expect(page.getByText("2 of 2 books done.")).toBeVisible({
		timeout: 30_000,
	});
	await expect(page.getByRole("button", { name: "Changed: 1" })).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Nothing to change: 1" }),
	).toBeVisible();

	await page.goto("/");
	await chooseLibrary(page, `Files ${stamp}`);
	await page.getByRole("button", { name: "Select books" }).click();
	await bar(page)
		.getByRole("button", { name: "Select every book that matches" })
		.click();
	await bar(page).getByRole("button", { name: "Write into the files" }).click();
	await expect(
		page.getByRole("heading", { name: "Writing details into the files" }),
	).toBeVisible();
	await expect(page.getByText("2 of 2 books done.")).toBeVisible({
		timeout: 20_000,
	});

	const fixed = await (
		await page.request.get(`/api/v1/books/${ids[broken]}`)
	).json();
	const live = fixed.files.filter((f: { missing: boolean }) => !f.missing);
	expect(live).toHaveLength(1);
	expect(live[0].ereader?.findings ?? []).toEqual([]);

	await deleteLibrary(page, library);
});
