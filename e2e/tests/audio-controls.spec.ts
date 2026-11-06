import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, type Page, test } from "@playwright/test";
import { makeLibrary, uploadFile } from "../fixtures/accounts";
import { openSignedIn } from "../fixtures/app";

function part(n: 1 | 2, stamp: string): Buffer {
	const audio = readFileSync(
		fileURLToPath(new URL(`../fixtures/audio/part${n}.mp3`, import.meta.url)),
	);
	// An ID3v1 tag at the end, its comment the stamp: every test's parts are
	// files of their own to the upload's duplicate check, and play the same.
	const tag = Buffer.alloc(128);
	tag.write("TAG", 0, "latin1");
	tag.write(stamp.slice(0, 30), 97, "latin1");
	return Buffer.concat([audio, tag]);
}

/** The seconds the player's clock shows. */
async function clock(page: Page): Promise<number> {
	const [m, s] = ((await page.getByTestId("position").textContent()) ?? "0:00")
		.split(":")
		.map(Number);
	return (m ?? 0) * 60 + (s ?? 0);
}

const rate = (page: Page) =>
	page.locator("audio").evaluate((el: HTMLAudioElement) => el.playbackRate);

test("the audiobook player's speed and keys", async ({
	page,
	browserName,
}, testInfo) => {
	// Firefox in the test container finds no audio output and stops with
	// OnMediaSinkAudioError, which the player shows as it should; there is
	// nothing to hear, so nothing to test.
	test.skip(
		browserName === "firefox",
		"Firefox has no audio output in the test container",
	);
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const library = await makeLibrary(
		page.request,
		`Audio keys ${stamp}`,
		"private",
	);
	let bookId = "";
	// The stamp leads the name: one that ends the title in a number would
	// read as a title with a number in it, not as parts of one.
	for (const n of [1, 2] as const) {
		const added = await uploadFile(
			page.request,
			library,
			`${stamp} Foghorn - Part ${n}.mp3`,
			"audio/mpeg",
			part(n, stamp),
		);
		bookId ||= added.bookId;
		expect(added.bookId).toBe(bookId);
	}
	await expect
		.poll(
			async () =>
				(await (await page.request.get(`/api/v1/books/${bookId}/audio`)).json())
					.complete,
			{ timeout: 20_000 },
		)
		.toBe(true);

	await page.goto(`/books/${bookId}/listen`);
	await page.getByRole("button", { name: "Listen" }).click();
	const speed = page.getByLabel("Speed");
	await expect(speed).toHaveValue("1");
	await speed.selectOption({ label: "1.5×" });
	await expect.poll(() => rate(page)).toBe(1.5);

	// The keys answer once the focus is off the list of speeds.
	await page.getByRole("heading", { level: 1 }).click();
	await page.keyboard.press("+");
	await expect(speed).toHaveValue("1.75");
	await expect.poll(() => rate(page)).toBe(1.75);
	await page.keyboard.press("-");
	await page.keyboard.press("-");
	await expect(speed).toHaveValue("1.25");

	// Six seconds into twelve: the second part, from its start.
	await page.keyboard.press("5");
	await expect.poll(() => clock(page)).toBe(6);
	await expect(page.locator("audio")).toHaveAttribute("data-part", "1");
	await page.keyboard.press("Home");
	await expect.poll(() => clock(page)).toBe(0);
	await expect(page.locator("audio")).toHaveAttribute("data-part", "0");

	await page.keyboard.press("k");
	await expect(
		page.getByRole("button", { name: "Pause", exact: true }),
	).toBeVisible();
	await expect
		.poll(() => clock(page), { timeout: 10_000 })
		.toBeGreaterThanOrEqual(1);
	await page.keyboard.press("k");
	await expect(
		page.getByRole("button", { name: "Play", exact: true }),
	).toBeVisible();
	// The speed holds for what is played.
	expect(await rate(page)).toBe(1.25);

	await page.keyboard.press("?");
	const help = page.getByRole("dialog", { name: "Keyboard shortcuts" });
	await expect(help).toBeVisible();
	await expect(help.getByText("Faster", { exact: true })).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(help).toBeHidden();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
