import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { makeAccount, signedIn } from "../fixtures/accounts";
import { accountMenu, openSignedIn } from "../fixtures/app";

test("a person signs another browser out from their profile", async ({
	page,
	browser,
}, testInfo) => {
	// Three sign-ins, each a password hash to check.
	test.setTimeout(45_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	await openSignedIn(page);
	// An account of its own, whose sessions are only this test's.
	const account = await makeAccount(page.request, "reader", stamp);
	const { context, page: here } = await signedIn(browser, testInfo, account);
	// The other browser says what it is, which is what the list names it by.
	const { context: elsewhere, page: there } = await signedIn(
		browser,
		testInfo,
		account,
		{
			userAgent: `Other browser ${stamp}`,
		},
	);
	await there.goto("/collections");
	await expect(
		there.getByRole("heading", { name: "Collections", level: 1 }),
	).toBeVisible();

	await here.goto("/profile");
	const sessions = here.getByRole("region", {
		name: "Where you are signed in",
	});
	await expect(sessions.getByText("This browser")).toBeVisible();
	await sessions
		.getByRole("button", { name: `Sign out Other browser ${stamp}` })
		.click();
	await expect(sessions.getByText(`Other browser ${stamp}`)).toHaveCount(0);
	await expect(sessions.getByRole("listitem")).toHaveCount(1);

	await there.reload();
	await expect(there.getByRole("heading", { name: "Sign in" })).toBeVisible();
	// This browser stays signed in.
	await here.reload();
	await expect(
		here.getByRole("heading", { name: "Your account", level: 1 }),
	).toBeVisible();
	await elsewhere.close();

	// Signing out here ends this session too.
	await (await accountMenu(here, account.username))
		.getByRole("button", { name: "Sign out" })
		.click();
	await expect(here.getByRole("heading", { name: "Sign in" })).toBeVisible();
	expect((await here.request.get("/api/v1/auth/sessions")).status()).toBe(401);
	await context.close();
});

test("the language is chosen on the profile and kept in this browser", async ({
	page,
}) => {
	await openSignedIn(page);
	await page.goto("/profile");
	const choice = page.getByLabel("The language GOtome speaks in this browser.");
	await expect(choice).toHaveValue("browser");
	await choice.selectOption({ label: "Deutsch" });
	await expect(
		page.getByRole("heading", { name: "Ihr Konto", level: 1 }),
	).toBeVisible();
	await expect(page.getByRole("heading", { name: "Sprache" })).toBeVisible();

	await page.reload();
	await expect(
		page.getByRole("heading", { name: "Ihr Konto", level: 1 }),
	).toBeVisible();
	await page.goto("/");
	await expect(
		page.getByRole("heading", { name: "Bibliothek", level: 1 }),
	).toBeVisible();

	await page.goto("/profile");
	await page
		.getByLabel("Die Sprache, in der GOtome in diesem Browser spricht.")
		.selectOption({ label: "Wie der Browser" });
	await expect(
		page.getByRole("heading", { name: "Your account", level: 1 }),
	).toBeVisible();
});
