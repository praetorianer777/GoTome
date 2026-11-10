import { mkdirSync, readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { devices, expect, type Page, test } from "@playwright/test";
import { accountMenu, signIn } from "../fixtures/app";
import { ADMIN } from "../fixtures/stack";

const BOOKS = fileURLToPath(new URL("../../web/demo/files/", import.meta.url));
const AUDIO = fileURLToPath(new URL("../fixtures/audio/", import.meta.url));
const OUT = process.env.SHOTS_OUT ?? "/src/.cache/screenshots";

type Theme = "Light" | "Dark";

async function shoot(page: Page, name: string, theme: Theme): Promise<void> {
	await page.mouse.move(0, 0);
	await page.screenshot({ path: `${OUT}/${name}-${theme.toLowerCase()}.png` });
}

async function setTheme(page: Page, theme: Theme): Promise<void> {
	const menu = await accountMenu(page);
	await menu.getByLabel("Colours").selectOption({ label: theme });
	await expect(page.locator("html")).toHaveAttribute(
		"data-theme",
		theme.toLowerCase(),
	);
	await page.keyboard.press("Escape");
}

async function signInFresh(page: Page): Promise<void> {
	await page.goto("/");
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	await signIn(page);
	await expect(
		page.getByRole("heading", { name: "Library", level: 1 }),
	).toBeVisible();
}

async function settle(page: Page): Promise<void> {
	await page.waitForLoadState("networkidle").catch(() => {});
	await page.evaluate(() => document.fonts.ready);
	await page.waitForTimeout(800);
}

test("the screenshots of the README", async ({ browser, page }) => {
	mkdirSync(OUT, { recursive: true });

	const { needed } = (await (
		await page.request.get("/api/v1/setup")
	).json()) as { needed: boolean };
	if (needed) {
		await page.goto("/");
		await expect(page).toHaveURL(/\/setup$/);
		await page.getByLabel("User name").fill(ADMIN.username);
		await page.getByLabel("Password").fill(ADMIN.password);
		await page.getByRole("button", { name: "Create the account" }).click();
		await expect(
			page.getByRole("heading", { name: "No library yet" }),
		).toBeVisible();
	} else {
		await page.goto("/");
		await signIn(page);
		await expect(page.getByRole("heading", { name: "Library", level: 1 })).toBeVisible();
	}
	const before = (await (await page.request.get("/api/v1/libraries")).json()) as {
		libraries: { id: string }[];
	};
	for (const l of before.libraries) await page.request.delete(`/api/v1/libraries/${l.id}`);

	const created = await page.request.post("/api/v1/libraries", {
		data: { name: "Public domain", mode: "managed", visibility: "private" },
	});
	expect(created.status()).toBe(201);
	const library = (await created.json()).id as string;

	const ids: Record<string, string> = {};
	for (const file of readdirSync(BOOKS)
		.filter((f) => f.endsWith(".epub"))
		.sort()) {
		await expect(async () => {
			const r = await page.request.post(
				`/api/v1/libraries/${library}/uploads`,
				{
					multipart: {
						file: {
							name: file,
							mimeType: "application/epub+zip",
							buffer: readFileSync(BOOKS + file),
						},
					},
					timeout: 60_000,
				},
			);
			expect(r.status()).toBe(200);
			ids[file.replace(/\.epub$/, "")] = (await r.json()).bookId;
		}).toPass({ timeout: 120_000 });
	}
	let audioBook = "";
	for (const n of [1, 2]) {
		const r = await page.request.post(`/api/v1/libraries/${library}/uploads`, {
			multipart: {
				file: {
					name: `The Lighthouse - Part ${n}.mp3`,
					mimeType: "audio/mpeg",
					buffer: readFileSync(`${AUDIO}part${n}.mp3`),
				},
			},
			timeout: 60_000,
		});
		expect(r.status()).toBe(200);
		audioBook ||= (await r.json()).bookId;
	}

	await expect
		.poll(
			async () => (await (await page.request.get(`/api/v1/books/${audioBook}/audio`)).json()).complete,
			{ timeout: 60_000 },
		)
		.toBe(true);
	const all = [...Object.values(ids), audioBook];
	await expect
		.poll(
			async () => {
				for (const id of all) {
					const d = await (
						await page.request.get(`/api/v1/books/${id}`)
					).json();
					if (
						d.files.some(
							(f: { extractState: string }) => f.extractState === "pending",
						)
					)
						return false;
				}
				return true;
			},
			{ timeout: 300_000, intervals: [3_000] },
		)
		.toBe(true);

	const id = (key: string): string => ids[key] as string;
	const status = (book: string, body: object) =>
		page.request.put(`/api/v1/books/${book}/reading`, { data: body });
	const place = (book: string, fraction: number) =>
		page.request.put(`/api/v1/books/${book}/progress/ebook`, {
			data: {
				locator: `fraction:${fraction}`,
				fraction,
				clientId: "screenshots",
			},
		});
	const finished = [
		"b-pride",
		"b-emma",
		"b-dracula",
		"b-alice",
		"b-frankenstein",
		"b-hound",
	];
	for (const [i, key] of finished.entries()) {
		await place(id(key), 1);
		await status(id(key), { status: "completed", rating: 5 - (i % 2) });
	}
	for (const [key, f] of [
		["b-moby", 0.37],
		["b-persuasion", 0.62],
		["b-twist", 0.14],
		["b-sense", 0.81],
	] as const) {
		await place(id(key), f);
	}
	for (const key of ["b-war", "b-twenty"])
		await status(id(key), { status: "wishlist" });

	await page.context().clearCookies();
	await signInFresh(page);
	await page.setViewportSize({ width: 1440, height: 900 });

	const found = page.getByRole("region", { name: "Books found" });
	await expect(async () => {
		await page.goto("/search?q=whale");
		await expect(found.locator("mark").first()).toBeVisible({ timeout: 4_000 });
	}).toPass({ timeout: 240_000 });

	const detail = await (
		await page.request.get(`/api/v1/books/${id("b-moby")}`)
	).json();
	const fileId = detail.files[0].id as string;

	for (const theme of ["Light", "Dark"] as const) {
		await page.goto("/");
		await setTheme(page, theme);

		await page.goto("/?view=grid");
		await expect(
			page.getByRole("heading", { name: "Library", level: 1 }),
		).toBeVisible();
		await settle(page);
		await shoot(page, "library", theme);

		await page.goto(`/books/${id("b-pride")}`);
		await settle(page);
		await shoot(page, "book", theme);

		await page.goto("/search?q=whale");
		await expect(found.locator("mark").first()).toBeVisible();
		await settle(page);
		await shoot(page, "search", theme);

		await page.goto(`/books/${id("b-moby")}/read/${fileId}`);
		await expect(page.getByText(/^\d+%$/)).toBeVisible();
		await expect.poll(() => page.frames().length).toBeGreaterThan(1);
		await settle(page);
		await shoot(page, "reader", theme);

		await page.goto(`/books/${audioBook}/listen`);
		await page.getByRole("button", { name: "Listen", exact: true }).click();
		await expect(page.getByRole("button", { name: /^Arrival/ })).toBeVisible();
		const pause = page.getByRole("button", { name: "Pause", exact: true });
		if (await pause.isVisible()) await pause.click();
		await settle(page);
		await shoot(page, "audiobook", theme);
	}

	const phone = await browser.newContext({
		...devices["Pixel 7"],
		viewport: { width: 390, height: 844 },
		deviceScaleFactor: 2,
	});
	const p = await phone.newPage();
	await signInFresh(p);
	for (const theme of ["Light", "Dark"] as const) {
		await p.goto("/");
		await setTheme(p, theme);
		await p.goto("/?view=grid");
		await expect(
			p.getByRole("heading", { name: "Library", level: 1 }),
		).toBeVisible();
		await settle(p);
		await shoot(p, "phone-library", theme);
		await p.goto(`/books/${id("b-pride")}`);
		await settle(p);
		await shoot(p, "phone-book", theme);
	}
	await phone.close();

	await page.request.delete(`/api/v1/libraries/${library}`);
});
