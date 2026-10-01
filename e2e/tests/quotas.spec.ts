import { expect, test } from "@playwright/test";
import { openSignedIn, signIn } from "../fixtures/app";

test("an upload past the quota is refused and says why", async ({
	page,
	browser,
}, testInfo) => {
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const username = `quota-${stamp}`;
	const password = `pw-${stamp}`;
	await openSignedIn(page);
	// An account of its own: the browser projects share the administrator,
	// whose uploads a quota would get in the way of.
	const made = await page.request.post("/api/v1/users", {
		data: { username, password, role: "editor" },
	});
	expect(made.status()).toBe(201);
	const { id } = await made.json();
	expect(
		(
			await page.request.put(`/api/v1/users/${id}/quota`, {
				data: { quotaBytes: 1000 },
			})
		).status(),
	).toBe(200);
	const created = await page.request.post("/api/v1/libraries", {
		data: { name: `Quota ${stamp}`, mode: "managed", visibility: "shared" },
	});
	const library = (await created.json()).id;

	const theirs = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await theirs.newPage();
	await them.goto("/upload");
	await signIn(them, { username, password });
	await expect(
		them.getByText("Your uploads take up 0 B of the 1 KB you may use."),
	).toBeVisible();
	const choice = them.getByRole("combobox", { name: "Into the library" });
	if (await choice.isVisible()) {
		await choice.selectOption({ label: `Quota ${stamp}` });
	}
	await them.getByLabel("Choose files").setInputFiles({
		name: `Too big ${stamp}.pdf`,
		mimeType: "application/pdf",
		buffer: Buffer.alloc(5000, `${stamp}`),
	});
	const row = them.getByRole("listitem", { name: `Too big ${stamp}.pdf` });
	await expect(row.getByText(/does not fit into your storage/)).toBeVisible();
	await theirs.close();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
