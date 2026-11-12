import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import { accountMenu, bookFrame, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { makePdf } from "../fixtures/pdf";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// The worker, its caches and the offline mode of the browser are Chromium's
// to test; the other engines run the same code, which the app tests hold.
test.skip(({ browserName }) => browserName !== "chromium", "offline mode is driven through Chromium");
test.use({ actionTimeout: 15_000 });

async function uploadBook(page: Page, stamp: string) {
	const library = (
		await (
			await page.request.post("/api/v1/libraries", {
				data: { name: `Offline ${stamp}`, mode: "managed", visibility: "private" },
			})
		).json()
	).id as string;
	const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
		multipart: {
			file: {
				name: `Offline ${stamp}.epub`,
				mimeType: "application/epub+zip",
				buffer: makeEpub(`Offline ${stamp}`, 3, stamp),
			},
		},
	});
	const { bookId, outcome } = await uploaded.json();
	expect(outcome).toBe("added");
	return { bookId: bookId as string, library };
}

/** How many entries the kept caches hold. */
const keptEntries = (page: Page) =>
	page.evaluate(async () => {
		let n = 0;
		for (const name of await caches.keys()) {
			if (name.startsWith("gotome-kept:")) n += (await (await caches.open(name)).keys()).length;
		}
		return n;
	});

test("a kept book opens and reads offline, its places are sent when back, and signing out clears it", async ({
	page,
	context,
}, testInfo) => {
	test.setTimeout(120_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const { bookId, library } = await uploadBook(page, stamp);

	await page.goto(`/books/${bookId}`);
	await page.evaluate(() => navigator.serviceWorker.ready);
	await expect
		.poll(() => page.evaluate(() => navigator.serviceWorker.controller !== null))
		.toBe(true);
	await page.getByRole("button", { name: `Keep Offline ${stamp}.epub offline` }).click();
	await expect(
		page.getByRole("button", { name: `Remove the offline copy of Offline ${stamp}.epub` }),
	).toBeVisible({ timeout: 60_000 });
	expect(await keptEntries(page)).toBeGreaterThan(0);

	const readerPath = await page
		.getByRole("link", { name: `Read Offline ${stamp}.epub` })
		.getAttribute("href");

	await context.setOffline(true);
	const sent: string[] = [];
	page.on("request", (r) => {
		if (r.method() === "PUT" && r.url().includes("/progress/ebook")) sent.push(r.url());
	});
	await page.goto(readerPath ?? "/");
	await bookFrame(page, "Chapter 1, paragraph 1.");

	await page.getByRole("button", { name: "Page right" }).click();
	await page.keyboard.press("ArrowRight");
	await page.keyboard.press("ArrowRight");
	await expect
		.poll(() =>
			page.evaluate(
				() =>
					new Promise<number>((resolve) => {
						const open = indexedDB.databases
							? indexedDB.databases().then((all) => all.find((d) => d.name?.startsWith("gotome:")))
							: Promise.resolve(undefined);
						void open.then((d) => {
							if (!d?.name) return resolve(0);
							const req = indexedDB.open(d.name);
							req.onsuccess = () => {
								const all = req.result.transaction("queue").objectStore("queue").getAll();
								all.onsuccess = () => resolve(all.result.length);
							};
						});
					}),
			),
		)
		.toBeGreaterThan(0);

	const resent = page.waitForResponse(
		(r) => r.url().includes("/progress/ebook") && r.request().method() === "PUT" && r.ok(),
	);
	await context.setOffline(false);
	const answer = await (await resent).json();
	expect(answer.saved).toBe(true);
	expect(answer.progress.fraction).toBeGreaterThan(0);

	await page.goto("/profile");
	const kept = page.getByRole("region", { name: "Offline" });
	await expect(kept.getByRole("link", { name: `Offline ${stamp}` })).toBeVisible();

	await (await accountMenu(page)).getByRole("button", { name: "Sign out" }).click();
	await expect(page).toHaveURL(/\/login/);
	expect(await keptEntries(page)).toBe(0);

	await openSignedIn(page);
	expect((await page.request.delete(`/api/v1/libraries/${library}`)).status()).toBe(204);
});

async function controlled(page: Page) {
	await page.evaluate(() => navigator.serviceWorker.ready);
	await expect
		.poll(() => page.evaluate(() => navigator.serviceWorker.controller !== null))
		.toBe(true);
}

test("a kept PDF opens offline from ranges of its copy", async ({ page, context }, testInfo) => {
	test.setTimeout(120_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const library = (
		await (
			await page.request.post("/api/v1/libraries", {
				data: { name: `Offline PDF ${stamp}`, mode: "managed", visibility: "private" },
			})
		).json()
	).id as string;
	const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
		multipart: {
			file: {
				name: `Offline ${stamp}.pdf`,
				mimeType: "application/pdf",
				buffer: makePdf(20, 20_000, stamp),
			},
		},
	});
	const { bookId } = await uploaded.json();

	await page.goto(`/books/${bookId}`);
	await controlled(page);
	await page.getByRole("button", { name: `Keep Offline ${stamp}.pdf offline` }).click();
	await expect(
		page.getByRole("button", { name: `Remove the offline copy of Offline ${stamp}.pdf` }),
	).toBeVisible({ timeout: 60_000 });
	const readerPath = await page
		.getByRole("link", { name: `Read Offline ${stamp}.pdf` })
		.getAttribute("href");

	await context.setOffline(true);
	await page.goto(readerPath ?? "/");
	const shown = page.getByRole("figure", { name: /^Page \d+ of / });
	await expect(shown).toHaveAttribute("data-page", "1", { timeout: 15_000 });
	await page.getByRole("button", { name: "Next page" }).click();
	await expect(shown).toHaveAttribute("data-page", "2");
	await context.setOffline(false);
	expect((await page.request.delete(`/api/v1/libraries/${library}`)).status()).toBe(204);
});

test("a kept audiobook plays offline", async ({ page, context }, testInfo) => {
	test.setTimeout(120_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const library = (
		await (
			await page.request.post("/api/v1/libraries", {
				data: { name: `Offline audio ${stamp}`, mode: "managed", visibility: "private" },
			})
		).json()
	).id as string;
	const audio = readFileSync(fileURLToPath(new URL("../fixtures/audio/part1.mp3", import.meta.url)));
	const tag = Buffer.alloc(128);
	tag.write("TAG", 0, "latin1");
	tag.write(stamp.slice(0, 30), 97, "latin1");
	const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
		multipart: {
			file: {
				name: `${stamp} Offline Listen.mp3`,
				mimeType: "audio/mpeg",
				buffer: Buffer.concat([audio, tag]),
			},
		},
	});
	const { bookId } = await uploaded.json();
	await expect
		.poll(async () => (await (await page.request.get(`/api/v1/books/${bookId}/audio`)).json()).complete, {
			timeout: 20_000,
		})
		.toBe(true);

	await page.goto(`/books/${bookId}`);
	await controlled(page);
	await page.getByRole("button", { name: /^Keep .* offline$/ }).click();
	await expect(page.getByRole("button", { name: /^Remove the offline copy of/ })).toBeVisible({
		timeout: 60_000,
	});

	await context.setOffline(true);
	await page.goto(`/books/${bookId}/listen`);
	await page.getByRole("button", { name: "Listen", exact: true }).click();
	await page.getByRole("button", { name: "Play", exact: true }).click();
	await expect
		.poll(
			() => page.locator("audio").evaluate((el: HTMLAudioElement) => el.readyState),
			{ timeout: 15_000 },
		)
		.toBeGreaterThanOrEqual(1);
	await context.setOffline(false);
	expect((await page.request.delete(`/api/v1/libraries/${library}`)).status()).toBe(204);
});
