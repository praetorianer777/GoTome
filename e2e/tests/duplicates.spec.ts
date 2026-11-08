import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";

test("a book uploaded twice with other details shows as a pair of cards, and is kept as two", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Twins ${stamp}`, mode: "managed", visibility: "private" },
	});
	const library = (await created.json()).id as string;
	// The same chapters under two titles: the same content, other details.
	for (const title of [`First ${stamp}`, `Second ${stamp}`]) {
		const uploaded = await page.request.post(
			`/api/v1/libraries/${library}/uploads`,
			{
				multipart: {
					file: {
						name: `${title}.epub`,
						mimeType: "application/epub+zip",
						buffer: makeEpub(title, 2, `${title} tag`, false, {
							1: `Only in ${stamp}.`,
						}),
					},
				},
			},
		);
		expect((await uploaded.json()).outcome).toBe("added");
	}

	const said = `First ${stamp} and Second ${stamp} have the same content, with other details.`;
	const pairs = page.getByRole("region", { name: "Pairs" });
	// The books are read and checked in the background after the upload.
	await expect(async () => {
		await page.goto(`/duplicates?library=${library}`);
		await expect(pairs.getByRole("listitem", { name: said })).toBeVisible({
			timeout: 2_000,
		});
	}).toPass({ timeout: 30_000 });
	const pair = pairs.getByRole("listitem", { name: said });
	// A library's long name does not widen the page past a phone's screen.
	const overflow = await page.evaluate(
		() => document.documentElement.scrollWidth - document.documentElement.clientWidth,
	);
	expect(overflow).toBeLessThanOrEqual(0);
	await expect(pair.getByText("Score 99%")).toBeVisible();

	// The two side by side, as cards; stacked on a phone.
	const first = pair.getByRole("article", { name: `First ${stamp} (EPUB)` });
	const second = pair.getByRole("article", { name: `Second ${stamp} (EPUB)` });
	await expect(first).toContainText(`First ${stamp}.epub`);
	await expect(second).toContainText(`Second ${stamp}.epub`);
	const [a, b] = [await first.boundingBox(), await second.boundingBox()];
	if (testInfo.project.name === "phone") {
		expect(b?.y ?? 0).toBeGreaterThanOrEqual((a?.y ?? 0) + (a?.height ?? 0));
	} else {
		expect(b?.y).toBe(a?.y);
	}

	await pair.getByRole("button", { name: "Keep both" }).click();
	await expect(pairs.getByRole("listitem", { name: said })).toBeHidden();
	await page.getByRole("button", { name: "Kept both" }).click();
	await expect(pairs.getByRole("listitem", { name: said })).toBeVisible();
	await expect(page).toHaveURL(/state=kept_both/);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
