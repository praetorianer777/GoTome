import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, type Page, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

function part(n: 1 | 2, stamp: string): Buffer {
	const audio = readFileSync(fileURLToPath(new URL(`../fixtures/audio/part${n}.mp3`, import.meta.url)));
	// An ID3v1 tag at the end, its comment the stamp: every test's parts are
	// files of their own to the upload's duplicate check, and play the same.
	const tag = Buffer.alloc(128);
	tag.write("TAG", 0, "latin1");
	tag.write(stamp.slice(0, 30), 97, "latin1");
	return Buffer.concat([audio, tag]);
}

/** Waits for the player's clock to show at least the seconds. */
async function heard(page: Page, seconds: number) {
	await expect
		.poll(
			async () => {
				const [m, s] = ((await page.getByTestId("position").textContent()) ?? "0:00").split(":").map(Number);
				return (m ?? 0) * 60 + (s ?? 0);
			},
			{ timeout: 15_000 },
		)
		.toBeGreaterThanOrEqual(seconds);
}

test("an audiobook in two parts plays as one, seeks across the parts, and resumes on another device", async ({
	page,
	browser,
	browserName,
}, testInfo) => {
	// Firefox in the test container finds no audio output and stops with
	// OnMediaSinkAudioError, which the player shows as it should; there is
	// nothing to hear, so nothing to test.
	test.skip(browserName === "firefox", "Firefox has no audio output in the test container");
	// Uploading, reading the parts and listening to eight seconds of them.
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Audio ${stamp}`, mode: "managed", visibility: "private" },
	});
	const { id } = await created.json();
	let bookId = "";
	// The stamp leads the name: one that ends the title in a number would
	// read as a title with a number in it, not as parts of one.
	for (const n of [1, 2] as const) {
		const uploaded = await page.request.post(`/api/v1/libraries/${id}/uploads`, {
			multipart: {
				file: { name: `${stamp} Lighthouse - Part ${n}.mp3`, mimeType: "audio/mpeg", buffer: part(n, stamp) },
			},
		});
		const answer = await uploaded.json();
		expect(answer.outcome).toBe("added");
		bookId ||= answer.bookId;
		expect(answer.bookId).toBe(bookId);
	}
	// The parts' lengths are known once they have been read.
	await expect
		.poll(async () => (await (await page.request.get(`/api/v1/books/${bookId}/audio`)).json()).complete && "read", {
			timeout: 20_000,
		})
		.toBe("read");

	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: "Listen" }).click();
	await expect(page.getByRole("button", { name: /^Arrival/ })).toBeVisible();
	await expect(page.getByRole("button", { name: /^The Keeper/ })).toBeVisible();
	await page.getByRole("button", { name: "Play", exact: true }).click();
	await expect(page.getByRole("button", { name: "Pause", exact: true })).toBeVisible();
	await heard(page, 1);

	// Into the second part, which is another file.
	await page.getByRole("button", { name: /Lighthouse - Part 2/ }).click();
	await expect(page.locator("audio")).toHaveAttribute("data-part", "1");
	await heard(page, 8);
	const saved = page.waitForResponse((r) => r.url().includes("/progress/audio") && r.request().method() === "PUT");
	await page.getByRole("button", { name: "Pause", exact: true }).click();
	const place = (await (await saved).json()).progress;
	expect(place.positionMs).toBeGreaterThanOrEqual(8000);

	// Elsewhere in the app, the bar along the bottom carries the book.
	await page.getByRole("link", { name: "Back to the book" }).click();
	await expect(page.getByRole("region", { name: "Now playing" })).toBeVisible();

	// Another device, another browser, picks up there.
	const other = await browser.newPage();
	await openSignedIn(other);
	await other.goto(`/books/${bookId}/listen`);
	await other.getByRole("button", { name: "Listen" }).click();
	await heard(other, Math.floor(place.positionMs / 1000));
	await expect(other.locator("audio")).toHaveAttribute("data-part", "1");
	await other.close();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(204);
});
