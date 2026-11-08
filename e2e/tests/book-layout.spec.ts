import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

// Names as long as some books carry, and words no line can hold.
const WORD =
	"Donaudampfschifffahrtsgesellschaftskapitänswitwenrentenversicherung";
const LONG = {
	title: `Rain Wild Chronicles 01 - Drachenhüter: ${WORD} und die lange Reise flussaufwärts`,
	subtitle: `Die Hüter der Drachen ${WORD}`,
	series: `Die Regenwildnis-Chroniken der Elderlinge ${WORD}`,
	seriesIndex: 1,
	publisher: `Penhaligon-Verlag-in-der-Verlagsgruppe-Random-House-GmbH-München-${WORD}`,
	published: "2012-03-19",
	language: "de",
	pageCount: 544,
	description: `${WORD} `.repeat(6),
	tags: [WORD, "Fantasy", `Drachen-und-Seeschlangen-${WORD}`],
	contributors: [
		...Array.from({ length: 8 }, (_, i) => ({
			name: `Margaret Astrid Lindholm Ogden-${WORD}-${i}`,
			role: "author",
		})),
		{ name: `Uta Helene Theodora ${WORD}`, role: "translator" },
	],
	identifiers: [
		{ type: "isbn", value: "9783764530605", fromFile: true },
		{
			type: "uuid",
			value: `urn:uuid:3f2a9c1e-77b4-4e0b-9d1c-${WORD}`,
			fromFile: true,
		},
		{
			type: "amazon",
			value: "B00ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
			fromFile: true,
		},
	],
};

test("a book's page keeps every long value inside its box, on a phone and on a wide screen", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Layout ${stamp}`, mode: "managed", visibility: "private" },
	});
	expect(created.status()).toBe(201);
	const { id } = await created.json();
	const fileName = `Robin_Hobb_-_Rain_Wild_Chronicles_01_-_${WORD}_${stamp.replace(/\W/g, "_")}.pdf`;
	const uploaded = await page.request.post(`/api/v1/libraries/${id}/uploads`, {
		multipart: {
			file: {
				name: fileName,
				mimeType: "application/pdf",
				buffer: Buffer.from(`%PDF-1.4\n% not a pdf ${stamp}\n`),
			},
		},
	});
	const { bookId } = await uploaded.json();

	// The book as the server has it, with the long values over it: the API
	// checks identifiers and names that are beside the point here.
	await page.route(new RegExp(`/api/v1/books/${bookId}$`), async (route) => {
		const real = await (await route.fetch()).json();
		await route.fulfill({ json: { ...real, ...LONG } });
	});

	for (const width of [360, 768, 1024, 1440]) {
		await page.setViewportSize({ width, height: 900 });
		await page.goto(`/books/${bookId}`);
		await expect(
			page.getByRole("heading", { name: LONG.title, level: 1 }),
		).toBeVisible();
		await expect(page.getByRole("region", { name: "Details" })).toContainText(
			"9783764530605",
		);
		await expect(page.getByRole("region", { name: "Files" })).toContainText(
			fileName,
		);

		const overflowing = await page.evaluate(() => {
			const describe = (el: Element) =>
				`${el.tagName.toLowerCase()}.${el.className} "${el.textContent?.slice(0, 40)}"`;
			const found: string[] = [];
			const article = document.querySelector("article");
			if (!article) return ["no article"];
			const box = article.getBoundingClientRect();
			for (const el of article.querySelectorAll("*")) {
				const style = getComputedStyle(el);
				if (style.display === "inline" || style.display === "contents")
					continue;
				if (style.overflowX === "auto" || style.overflowX === "scroll")
					continue;
				// Text for screen readers only is a box of one pixel.
				if (el.clientWidth <= 1) continue;
				// A clamped text ends in an ellipsis, and what it cuts off may
				// measure wider than the line it ends.
				const clamp = style.getPropertyValue("-webkit-line-clamp");
				if (clamp && clamp !== "none") continue;
				if (el.scrollWidth > el.clientWidth + 1) {
					found.push(
						`${describe(el)} is ${el.scrollWidth - el.clientWidth}px too wide`,
					);
				}
				const rect = el.getBoundingClientRect();
				if (rect.left < box.left - 1 || rect.right > box.right + 1) {
					found.push(`${describe(el)} leaves the page`);
				}
			}
			const terms = [...document.querySelectorAll("article dt, article dd")];
			for (const [i, a] of terms.entries()) {
				const ra = a.getBoundingClientRect();
				for (const b of terms.slice(i + 1)) {
					const rb = b.getBoundingClientRect();
					if (
						ra.left < rb.right - 1 &&
						rb.left < ra.right - 1 &&
						ra.top < rb.bottom - 1 &&
						rb.top < ra.bottom - 1
					) {
						found.push(`${describe(a)} overlaps ${describe(b)}`);
					}
				}
			}
			const page =
				document.documentElement.scrollWidth -
				document.documentElement.clientWidth;
			if (page > 0) found.push(`the page is ${page}px wider than the window`);
			return found;
		});
		expect(overflowing, `at ${width}px`).toEqual([]);
	}

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
