import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import {
	makeAccount,
	makeLibrary,
	signedIn,
	uploadFile,
} from "../fixtures/accounts";
import { mainNav, openSignedIn } from "../fixtures/app";
import { KEYCLOAK_URL } from "../fixtures/stack";
import { makePdf } from "../fixtures/pdf";

test("an administrator renames a library, chooses its members and opens it to everybody", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(45_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	const name = `Members ${stamp}`;
	const renamed = `Members renamed ${stamp}`;
	await openSignedIn(page);
	const reader = await makeAccount(page.request, "reader", stamp);
	const library = await makeLibrary(page.request, name, "private");
	const { bookId } = await uploadFile(
		page.request,
		library,
		`Members ${stamp}.pdf`,
		"application/pdf",
		makePdf(2, 0, stamp),
	);
	const { context, page: them } = await signedIn(browser, testInfo, reader);
	const theySee = async () =>
		(await them.request.get(`/api/v1/books/${bookId}`)).status();
	expect(await theySee()).toBe(404);

	await page.goto("/admin/libraries");
	let row = page.getByRole("listitem", { name });
	await row.getByLabel("Name").fill(renamed);
	await row.getByRole("button", { name: "Rename" }).click();
	row = page.getByRole("listitem", { name: renamed });
	await expect(row.getByLabel("Name")).toHaveValue(renamed);
	expect(
		(await (await page.request.get(`/api/v1/libraries/${library}`)).json())
			.name,
	).toBe(renamed);

	const members = row.getByRole("region", { name: "Members" });
	await expect(members.getByText("No members yet.")).toBeVisible();
	await members.getByLabel("Person").selectOption({ label: reader.username });
	await members.getByRole("button", { name: "Add" }).click();
	await expect(
		members.getByRole("listitem").filter({ hasText: reader.username }),
	).toBeVisible();
	expect(await theySee()).toBe(200);

	await members
		.getByRole("button", { name: `Remove ${reader.username}` })
		.click();
	await expect(members.getByText("No members yet.")).toBeVisible();
	expect(await theySee()).toBe(404);

	await row.getByLabel("Who sees it").selectOption({ label: "Everybody" });
	await expect(members).toHaveCount(0);
	expect(await theySee()).toBe(200);
	await context.close();

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("an administrator changes a role, signs a person out everywhere and sets their quota", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(45_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	await openSignedIn(page);
	const account = await makeAccount(page.request, "reader", stamp);
	const { context, page: them } = await signedIn(browser, testInfo, account);
	const { context: second } = await signedIn(browser, testInfo, account);
	await them.goto("/");
	const theirNav = await mainNav(them);
	await expect(theirNav.getByRole("link", { name: "Statistics" })).toBeVisible();
	await expect(theirNav.getByRole("link", { name: "Add books" })).toHaveCount(0);

	await (await mainNav(page)).getByRole("link", { name: "Users" }).click();
	const row = page.getByRole("listitem", { name: account.username });
	await row
		.getByRole("combobox", { name: "Role" })
		.selectOption({ label: "Editor" });
	await expect(row.getByRole("combobox", { name: "Role" })).toHaveValue(
		"editor",
	);
	await them.reload();
	await expect(
		(await mainNav(them)).getByRole("link", { name: "Add books" }),
	).toBeVisible();

	await row.getByLabel("Upload quota in GB").fill("1.5");
	await row.getByRole("button", { name: "Set the quota" }).click();
	await expect(row.getByText("Uploads take up 0 B of 1.5 GB.")).toBeVisible();
	await them.goto("/upload");
	await expect(
		them.getByText("Your uploads take up 0 B of the 1.5 GB you may use."),
	).toBeVisible();
	await row.getByLabel("Upload quota in GB").fill("lots");
	await row.getByRole("button", { name: "Set the quota" }).click();
	await expect(
		row.getByText("Give a number of gigabytes, or nothing for no limit."),
	).toBeVisible();
	await row.getByLabel("Upload quota in GB").fill("");
	const cleared = page.waitForResponse((r) => r.url().endsWith(`/users/${account.id}/quota`));
	await row.getByRole("button", { name: "Set the quota" }).click();
	expect((await cleared).status()).toBe(200);
	await expect(row.getByText("Uploads take up 0 B; no limit.")).toBeVisible();

	await page.reload();
	await row.getByRole("button", { name: "Sign out everywhere (2)" }).click();
	await expect(
		row.getByRole("button", { name: "Sign out everywhere (0)" }),
	).toBeDisabled();
	await them.reload();
	await expect(them.getByRole("heading", { name: "Sign in" })).toBeVisible();
	const other = await second.newPage();
	await other.goto("/");
	await expect(other.getByRole("heading", { name: "Sign in" })).toBeVisible();
	await second.close();
	await context.close();
});

test("the embedding model is chosen from the list, and a switch that embeds every book again is asked first", async ({
	page,
}, testInfo) => {
	// Settings are global: the browser projects, which share one stack and
	// run at once, would change them under each other's feet.
	test.skip(
		testInfo.project.name !== "chromium",
		"settings are global to the stack",
	);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	await openSignedIn(page);
	// A book for the switch to have to embed again.
	const library = await makeLibrary(page.request, `Models ${stamp}`, "private");
	await uploadFile(
		page.request,
		library,
		`Models ${stamp}.pdf`,
		"application/pdf",
		makePdf(2, 0, stamp),
	);

	const models = (
		await (await page.request.get("/api/v1/embedding/models")).json()
	).models as { name: string; chosen: boolean }[];
	const inUse = models.find((m) => m.chosen)?.name ?? "";
	const other = models.find((m) => !m.chosen)?.name ?? "";
	expect(inUse).not.toBe("");
	expect(other).not.toBe("");

	await page.goto("/admin/settings");
	const field = page.getByLabel("Language model");
	await expect(field).toHaveValue(inUse);
	const list = page.getByRole("list", { name: "The models to choose from" });
	await expect(list.getByRole("listitem", { name: inUse, exact: true })).toContainText(
		"dimensions",
	);
	await expect(list.getByRole("listitem", { name: other, exact: true })).toBeVisible();

	await field.selectOption(other);
	await page.getByRole("button", { name: "Save" }).click();
	const dialog = page.getByRole("alertdialog", { name: `Switch to ${other}?` });
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Keep the model in use" }).click();
	await expect(dialog).toBeHidden();
	await page.reload();
	await expect(page.getByLabel("Language model")).toHaveValue(inUse);

	await page.getByLabel("Language model").selectOption(other);
	await page.getByRole("button", { name: "Save" }).click();
	await dialog.getByRole("button", { name: "Switch and embed again" }).click();
	await expect(page.getByRole("status")).toHaveText("Saved.");
	await page.reload();
	await expect(page.getByLabel("Language model")).toHaveValue(other);

	// And back, asked about too where that model lacks the vectors, as it
	// does on a stack that embeds nothing.
	await page.getByLabel("Language model").selectOption(inUse);
	await page.getByRole("button", { name: "Save" }).click();
	const back = page.getByRole("alertdialog", { name: `Switch to ${inUse}?` });
	const saved = page.getByRole("status").filter({ hasText: "Saved." });
	await expect(back.or(saved)).toBeVisible();
	if (await back.isVisible()) {
		await back.getByRole("button", { name: "Switch and embed again" }).click();
	}
	await expect(saved).toBeVisible();
	expect(
		(
			await (await page.request.get("/api/v1/embedding/models")).json()
		).models.find((m: { chosen: boolean }) => m.chosen).name,
	).toBe(inUse);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("the identity provider is set up in the settings form, which refuses an address that is none", async ({
	page,
	browser,
}, testInfo) => {
	test.skip(
		testInfo.project.name !== "chromium",
		"settings are global to the stack",
	);
	// The values sso.spec.ts sets through the API, so that the two leave
	// the stack alike whichever runs last.
	const issuer = `${KEYCLOAK_URL || "http://keycloak.invalid"}/realms/gotome`;
	await openSignedIn(page);
	await page.goto("/admin/settings");

	await page.getByLabel("Identity provider: issuer URL").fill("not an address");
	await page.getByRole("button", { name: "Save" }).click();
	await expect(
		page.getByText(
			"Give the provider's issuer URL, such as https://auth.example.org/application/o/gotome/.",
		),
	).toBeVisible();

	await page.getByLabel("Identity provider: issuer URL").fill(issuer);
	await page.getByLabel("Identity provider: client ID").fill("gotome");
	await page
		.getByLabel("Identity provider: client secret")
		.fill("e2e-keycloak-client-secret");
	await page
		.getByLabel("Identity provider: name on the button")
		.fill("Keycloak");
	await page.getByLabel("Groups of editors").fill("gotome-editors");
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByRole("status")).toHaveText("Saved.");

	await page.reload();
	await expect(page.getByLabel("Identity provider: issuer URL")).toHaveValue(
		issuer,
	);
	await expect(page.getByLabel("Identity provider: client ID")).toHaveValue(
		"gotome",
	);
	await expect(page.getByLabel("Identity provider: client secret")).toHaveValue(
		"",
	);
	await expect(
		page.getByLabel("Identity provider: client secret"),
	).toHaveAccessibleDescription(/Saved on/);
	expect(
		await (await page.request.get("/api/v1/settings")).text(),
	).not.toContain("e2e-keycloak-client-secret");

	// The sign-in page offers it to someone signed out.
	const theirs = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await theirs.newPage();
	await them.goto("/login");
	await expect(
		them.getByRole("button", { name: "Sign in with Keycloak" }),
	).toBeVisible();
	await theirs.close();
});
