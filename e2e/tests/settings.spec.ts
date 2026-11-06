import { expect, test } from "@playwright/test";
import { mainNav, openSignedIn } from "../fixtures/app";

test("a token is saved encrypted and never shown again", async ({
	page,
}, testInfo) => {
	// Settings are global: the browser projects, which share one stack and
	// run at once, would change them under each other's feet.
	test.skip(
		testInfo.project.name !== "chromium",
		"settings are global to the stack",
	);
	const token = `hc-${Date.now()}`;
	await openSignedIn(page);

	await (await mainNav(page)).getByRole("link", { name: "Settings" }).click();
	await expect(
		page.getByRole("heading", { name: "Settings", level: 1 }),
	).toBeVisible();
	await page.getByLabel("Hardcover API token").fill(token);
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByRole("status")).toHaveText("Saved.");

	await page.reload();
	await expect(page.getByLabel("Hardcover API token")).toHaveValue("");
	// Of this token: other secrets may be set beside it.
	await expect(page.getByLabel("Hardcover API token")).toHaveAccessibleDescription(/Saved on/);
	const answer = await page.request.get("/api/v1/settings");
	expect(await answer.text()).not.toContain(token);

	await page
		.getByRole("button", { name: "Remove the Hardcover API token" })
		.click();
	await expect(page.getByLabel("Hardcover API token")).toHaveAccessibleDescription(/Not set\./);
});

test("a setting of a few values is chosen, not typed", async ({
	page,
}, testInfo) => {
	test.skip(
		testInfo.project.name !== "chromium",
		"settings are global to the stack",
	);
	// The development stack looks books up in its test records whichever
	// sources this names, so changing it disturbs no other test.
	await openSignedIn(page);
	await page.goto("/admin/settings");
	const sources = page.getByRole("group", { name: "Sources to ask" });
	const crossref = sources.getByRole("checkbox", { name: "Crossref" });
	await expect(crossref).toBeChecked();
	await crossref.uncheck();
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByRole("status")).toHaveText("Saved.");

	await page.reload();
	await expect(crossref).not.toBeChecked();
	const answer = await (await page.request.get("/api/v1/settings")).json();
	const providers = answer.settings.find(
		(s: { key: string }) => s.key === "metadata.providers",
	);
	expect(providers).toMatchObject({
		value: "openlibrary,hardcover",
		multiple: true,
		choices: ["openlibrary", "crossref", "hardcover"],
	});
	await expect(
		page.getByRole("combobox", { name: "Genres for books" }),
	).toBeVisible();
	await expect(
		page.getByRole("switch", { name: "Look up new books by themselves" }),
	).toBeVisible();

	await crossref.check();
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByRole("status")).toHaveText("Saved.");
});
