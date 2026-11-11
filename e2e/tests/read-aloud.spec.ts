import { type Page, expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { bookFrame, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test.use({ actionTimeout: 15_000 });

async function openBook(page: Page, stamp: string) {
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Aloud ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: {
					name: `Aloud ${stamp}.epub`,
					mimeType: "application/epub+zip",
					buffer: makeEpub(`Aloud ${stamp}`, 2, stamp),
				},
			},
		},
	);
	const { bookId } = await uploaded.json();
	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: `Read Aloud ${stamp}.epub` }).click();
	await bookFrame(page, "Chapter 1, paragraph 1.");
	return library as string;
}

test("the read aloud control is left out where the browser has no voices", async ({
	page,
}, testInfo) => {
	await page.addInitScript(() => {
		Object.defineProperty(window, "speechSynthesis", {
			value: {
				getVoices: () => [],
				addEventListener: () => {},
				removeEventListener: () => {},
				cancel: () => {},
				speak: () => {},
			},
		});
	});
	const library = await openBook(page, `none ${testInfo.project.name} ${Date.now()}`);
	await expect(page.getByRole("button", { name: "Keys" })).toBeEnabled();
	await expect(page.getByRole("button", { name: "Read aloud" })).toHaveCount(0);
	await page.request.delete(`/api/v1/libraries/${library}`);
});

test("the read aloud control starts and stops where the browser has voices", async ({
	page,
}, testInfo) => {
	const library = await openBook(page, `voices ${testInfo.project.name} ${Date.now()}`);
	await expect(page.getByRole("button", { name: "Keys" })).toBeEnabled();
	const voices = await page.evaluate(
		() =>
			new Promise<number>((resolve) => {
				const have = () => window.speechSynthesis?.getVoices().length ?? 0;
				if (have() > 0) return resolve(have());
				window.speechSynthesis?.addEventListener("voiceschanged", () => resolve(have()));
				setTimeout(() => resolve(have()), 2000);
			}),
	);
	test.skip(voices === 0, "This browser has no voices in the test container");
	const button = page.getByRole("button", { name: "Read aloud" });
	await expect(button).toBeVisible();
	await button.click();
	await expect(button).toHaveAttribute("aria-pressed", "true");
	await button.click();
	await expect(button).toHaveAttribute("aria-pressed", "false");
	await page.request.delete(`/api/v1/libraries/${library}`);
});

test("read aloud speaks the book's sentences in order and stops on the second press", async ({
	page,
}, testInfo) => {
	await page.addInitScript(() => {
		const said: string[] = [];
		(window as unknown as { said: string[] }).said = said;
		class Utterance {
			voice: unknown = null;
			rate = 1;
			lang = "";
			onend: (() => void) | null = null;
			onerror: (() => void) | null = null;
			constructor(public text: string) {}
		}
		Object.defineProperty(window, "SpeechSynthesisUtterance", { value: Utterance });
		Object.defineProperty(window, "speechSynthesis", {
			value: {
				getVoices: () => [
					{ name: "Test", voiceURI: "test", lang: "en-US", default: true, localService: true },
				],
				addEventListener: () => {},
				removeEventListener: () => {},
				cancel: () => {},
				speak: (u: Utterance) => {
					said.push(u.text);
					setTimeout(() => u.onend?.(), 100);
				},
			},
		});
	});
	const library = await openBook(page, `fake ${testInfo.project.name} ${Date.now()}`);
	const button = page.getByRole("button", { name: "Read aloud" });
	await expect(button).toBeEnabled();
	await button.click();
	await expect(button).toHaveAttribute("aria-pressed", "true");
	await expect
		.poll(() => page.evaluate(() => (window as unknown as { said: string[] }).said.length))
		.toBeGreaterThan(1);
	const said = await page.evaluate(() => (window as unknown as { said: string[] }).said);
	expect(said[0]).toContain("Chapter 1");
	// A page that settles after the press starts the reading again from its
	// top, so a sentence may come twice; none may be skipped.
	const numbers = said
		.map((text) => /^Chapter \d+, paragraph (\d+)\./.exec(text)?.[1])
		.filter((n): n is string => n !== undefined)
		.map(Number);
	expect(numbers.length).toBeGreaterThan(0);
	numbers.forEach((n, i) => {
		if (i > 0) expect(n).toBeLessThanOrEqual((numbers[i - 1] as number) + 1);
	});
	await button.click();
	await expect(button).toHaveAttribute("aria-pressed", "false");
	const count = await page.evaluate(() => (window as unknown as { said: string[] }).said.length);
	await page.waitForTimeout(500);
	expect(
		await page.evaluate(() => (window as unknown as { said: string[] }).said.length),
	).toBeLessThanOrEqual(count + 1);
	await page.request.delete(`/api/v1/libraries/${library}`);
});
