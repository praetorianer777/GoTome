import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

// A PNG of one pixel.
const PIXEL = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
	"base64",
);

test("an edited book keeps what was typed, and says where each field came from", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Edit ${stamp}`, mode: "managed", visibility: "shared" },
	});
	const { id } = await created.json();
	const uploaded = await page.request.post(`/api/v1/libraries/${id}/uploads`, {
		multipart: {
			file: {
				name: `Draft ${stamp}.pdf`,
				mimeType: "application/pdf",
				buffer: Buffer.from(`not a pdf ${stamp}`),
			},
		},
	});
	const { bookId } = await uploaded.json();

	await page.goto(`/books/${bookId}`);
	await page.getByRole("link", { name: "Edit details" }).click();
	const title = page.getByRole("textbox", { name: "Title", exact: true });
	await expect(title).toHaveAccessibleDescription("Guessed from the file name");
	await title.fill(`Edited ${stamp}`);
	await expect(
		page.getByRole("checkbox", { name: "Lock Title" }),
	).toBeChecked();
	await page.getByRole("combobox", { name: "New tag" }).fill("Drafts");
	await page.getByRole("button", { name: "Add tag" }).click();
	await page
		.getByLabel("Choose an image")
		.setInputFiles({ name: "cover.png", mimeType: "image/png", buffer: PIXEL });
	await expect(
		page.getByRole("button", { name: "Remove the cover" }),
	).toBeVisible();
	await page.getByRole("button", { name: "Save" }).click();

	await expect(
		page.getByRole("heading", { name: `Edited ${stamp}`, level: 1 }),
	).toBeVisible();
	await expect(page.getByText("Drafts", { exact: true })).toBeVisible();

	// Reading the file again does not take the title back to the file name.
	await expect(
		page.getByRole("button", { name: `Read Draft ${stamp}.pdf again` }),
	).toBeVisible({ timeout: 15_000 });
	await page
		.getByRole("button", { name: `Read Draft ${stamp}.pdf again` })
		.click();
	await expect(page.getByText(/^Could not be read: /)).toBeVisible({
		timeout: 15_000,
	});
	await page.reload();
	await expect(
		page.getByRole("heading", { name: `Edited ${stamp}`, level: 1 }),
	).toBeVisible();

	await page.getByRole("link", { name: "Edit details" }).click();
	await expect(
		page.getByRole("textbox", { name: "Title", exact: true }),
	).toHaveAccessibleDescription("Set by hand");
	await expect(
		page.getByRole("checkbox", { name: "Lock Cover" }),
	).toBeChecked();

	expect((await page.request.delete(`/api/v1/libraries/${id}`)).status()).toBe(
		204,
	);
});
