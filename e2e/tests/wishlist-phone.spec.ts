import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn } from "../fixtures/app";

test.skip(({ isMobile }) => !isMobile, "the phone's layout");

test("lookup results on the wishlist keep their text wide on a phone", async ({ page }) => {
	await openSignedIn(page);
	await page.setViewportSize({ width: 390, height: 844 });
	const title = "The Extraordinarily Long Chronicles of the Wandering Lighthouse Keepers";
	await page.route(/\/api\/v1\/metadata\/search\?/, (route) =>
		route.fulfill({
			json: {
				failures: [],
				candidates: [
					{
						provider: "openlibrary",
						id: "OL1",
						score: 0.5,
						title,
						publisher: "Elsewhere Press",
						published: "2020",
						contributors: [
							{ name: "Ida Brandt" },
							{ name: "Bartholomew Featherstonehaugh" },
							{ name: "Ximena Oyelaran-Smith" },
							{ name: "Jo Li" },
						],
						tags: [],
						identifiers: [],
					},
				],
			},
		}),
	);
	await page.goto("/wishlist");
	const find = page.getByRole("region", { name: "Wish for a book" });
	await find.getByRole("textbox", { name: "Title" }).fill("lighthouse");
	await find.getByRole("button", { name: "Look it up" }).click();
	const row = find.getByRole("listitem").first();
	const heading = row.getByText(title);
	await expect(heading).toBeVisible();
	const rowBox = await row.boundingBox();
	const textBox = await heading.boundingBox();
	const lineHeight = await heading.evaluate((el) => parseFloat(getComputedStyle(el).lineHeight));
	expect(textBox && rowBox && textBox.width / rowBox.width).toBeGreaterThan(0.6);
	expect(textBox && textBox.height / lineHeight).toBeLessThanOrEqual(4);
	const buttons = await row.getByRole("button").evaluateAll((els) =>
		els.map((el) => el.getBoundingClientRect().right),
	);
	for (const right of buttons) {
		expect(right).toBeLessThanOrEqual(390);
	}
});
