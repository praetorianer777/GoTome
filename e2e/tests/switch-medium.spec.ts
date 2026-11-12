import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { bookFrame, gotoUnderPolicy, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { deleteLibrary, uploadEpub, waitUntilRead } from "../fixtures/library";

function part(n: 1 | 2, stamp: string): Buffer {
	const audio = readFileSync(fileURLToPath(new URL(`../fixtures/audio/part${n}.mp3`, import.meta.url)));
	const tag = Buffer.alloc(128);
	tag.write("TAG", 0, "latin1");
	tag.write(stamp.slice(0, 30), 97, "latin1");
	return Buffer.concat([audio, tag]);
}

test("a book with a text and a recording offers to go on in the other medium at the matching chapter", async ({
	page,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Switch ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;

	// The recording's chapters (Arrival, The Keeper) are the text's, so
	// the chapters place the mapping and not the share of the whole.
	const text = await uploadEpub(
		page,
		library,
		`Lantern ${stamp}`,
		makeEpub(`Lantern ${stamp}`, 2, stamp, false, {}, ["Arrival", "The Keeper"]),
	);
	let sound = "";
	for (const n of [1, 2] as const) {
		const uploaded = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
			multipart: {
				file: { name: `${stamp} Harbour - Part ${n}.mp3`, mimeType: "audio/mpeg", buffer: part(n, stamp) },
			},
			timeout: 20_000,
		});
		const answer = await uploaded.json();
		expect(answer.outcome).toBe("added");
		sound ||= answer.bookId;
	}
	await waitUntilRead(page, text);
	await waitUntilRead(page, sound);
	const merged = await page.request.post(`/api/v1/books/${text}/merge`, { data: { from: sound, take: [] } });
	expect(merged.status()).toBe(200);
	await expect
		.poll(async () => (await (await page.request.get(`/api/v1/books/${text}/audio`)).json()).complete)
		.toBe(true);

	const save = async (medium: "ebook" | "audio", body: Record<string, unknown>) =>
		expect(
			(
				await page.request.put(`/api/v1/books/${text}/progress/${medium}`, {
					data: { clientId: "switch-spec", locator: "x", force: true, ...body },
				})
			).ok(),
		).toBe(true);

	// Three seconds into the recording is where its second chapter begins:
	// the text is offered at the same chapter, and opens there.
	await save("audio", { fraction: 0.25, positionMs: 3000 });
	await gotoUnderPolicy(page, `/books/${text}`);
	await page.getByRole("link", { name: "Continue reading at The Keeper" }).click();
	await bookFrame(page, "Chapter 2, paragraph 1.");

	// Having read on to near the end of the text, the recording is the
	// one behind, and the player opens at the place that matches.
	await save("ebook", { fraction: 0.9, locator: "fraction:0.9" });
	await gotoUnderPolicy(page, `/books/${text}`);
	await expect(page.getByRole("link", { name: /^Continue reading/ })).toHaveCount(0);
	await page.getByRole("link", { name: /^Continue in the audiobook at 0:(09|10|11)$/ }).click();
	await expect(page.getByTestId("position")).toHaveText(/^0:(09|10|11)$/);

	await deleteLibrary(page, library);
});
