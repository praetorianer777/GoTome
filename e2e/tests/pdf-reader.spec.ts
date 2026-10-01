import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { makePdf } from "../fixtures/pdf";

test("a large PDF opens at its first page from a part of the file, and reopens where it was left", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	// Two hundred pages of 40 kB each: 8 MB.
	const pdf = makePdf(200, 40_000, stamp);
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `PDF ${stamp}`, mode: "managed", visibility: "private" },
	});
	const { id } = await created.json();
	const uploaded = await page.request.post(`/api/v1/libraries/${id}/uploads`, {
		multipart: {
			file: { name: `Long ${stamp}.pdf`, mimeType: "application/pdf", buffer: pdf },
		},
	});
	const { bookId, outcome } = await uploaded.json();
	expect(outcome).toBe("added");

	const ranges: number[] = [];
	page.on("response", async (response) => {
		if (response.url().includes("/download") && response.status() === 206) {
			ranges.push(Number(response.headers()["content-length"]));
		}
	});
	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: `Read Long ${stamp}.pdf` }).click();
	const shown = page.getByRole("figure", { name: /^Page \d+ of / });
	await expect(shown).toHaveAttribute("data-page", "1", { timeout: 15_000 });
	await expect(page.getByText("of 200")).toBeVisible();
	// The page came from range requests, which together are far from the
	// whole file.
	expect(ranges.length).toBeGreaterThan(0);
	expect(ranges.reduce((a, b) => a + b, 0)).toBeLessThan(pdf.length / 4);

	await page.getByRole("button", { name: "Next page" }).click();
	await expect(shown).toHaveAttribute("data-page", "2");
	const saved = page.waitForResponse(
		(r) =>
			r.url().includes("/progress/ebook") &&
			r.request().method() === "PUT" &&
			r.request().postDataJSON().locator === "page:3",
	);
	await page.keyboard.press("ArrowRight");
	await expect(shown).toHaveAttribute("data-page", "3");
	expect((await (await saved).json()).progress.locator).toBe("page:3");

	// Listening before the page turns: the save follows the page, and may
	// come before the page is drawn.
	const savedLast = page.waitForResponse(
		(r) =>
			r.url().includes("/progress/ebook") &&
			r.request().method() === "PUT" &&
			r.request().postDataJSON().locator === "page:150",
	);
	await page.getByRole("textbox", { name: "Page number" }).fill("150");
	await page.getByRole("textbox", { name: "Page number" }).press("Enter");
	await expect(shown).toHaveAttribute("data-page", "150");
	await expect(page.getByText("Page 150", { exact: true })).toBeAttached();
	await savedLast;

	await page.reload();
	await expect(shown).toHaveAttribute("data-page", "150", { timeout: 15_000 });
	await page.getByRole("link", { name: "Back to the book" }).click();
	await expect(page.getByText(/^Read 75%/)).toBeVisible();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(204);
});
