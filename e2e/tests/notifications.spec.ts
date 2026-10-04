import { expect, test } from "@playwright/test";
import { openSignedIn, signIn } from "../fixtures/app";
import { makePdf } from "../fixtures/pdf";

test("a finished bulk change rings the bell without a reload, and stays unread until read", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(60_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	// Someone of their own, whose bell no other test rings.
	const username = `bell-${testInfo.project.name}-${Date.now()}`;
	const password = `pw-${Date.now()}-long-enough`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/users", {
		data: { username, password, role: "editor" },
	});
	expect(created.status()).toBe(201);
	const library = (
		await (
			await page.request.post("/api/v1/libraries", {
				data: { name: `Bell ${stamp}`, mode: "managed", visibility: "shared" },
			})
		).json()
	).id as string;
	const uploaded = await page.request.post(
		`/api/v1/libraries/${library}/uploads`,
		{
			multipart: {
				file: {
					name: `Bell ${stamp}.pdf`,
					mimeType: "application/pdf",
					buffer: makePdf(1, 0, stamp),
				},
			},
		},
	);
	const { bookId } = await uploaded.json();

	const theirs = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await theirs.newPage();
	await them.goto("/");
	await signIn(them, { username, password });
	const bell = them.getByRole("button", {
		name: /^Notifications, \d+ unread$/,
	});
	await expect(bell).toHaveAccessibleName("Notifications, 0 unread");

	// Started outside the page; the page hears of it through its stream.
	const started = await them.request.post("/api/v1/books/bulk", {
		data: { books: [bookId], action: "writeBack" },
	});
	expect(started.status()).toBe(202);
	await expect(bell).toHaveAccessibleName("Notifications, 1 unread", {
		timeout: 20_000,
	});

	await them.reload();
	await expect(bell).toHaveAccessibleName("Notifications, 1 unread");
	await bell.click();
	const panel = them.getByRole("region", { name: "Notifications" });
	await panel
		.getByRole("button", { name: /^Unread: Writing into many files is done/ })
		.click();
	await expect(them).toHaveURL(/\/bulk\//);
	await expect(bell).toHaveAccessibleName("Notifications, 0 unread");
	await them.reload();
	await expect(bell).toHaveAccessibleName("Notifications, 0 unread");

	await theirs.close();
	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
