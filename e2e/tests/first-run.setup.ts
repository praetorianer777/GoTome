import { expect, test } from "@playwright/test";
import { ADMIN } from "../fixtures/stack";

// The gate's stack is always fresh, so there this walks the setup page. On a
// stack that has been used before it only checks that the account is the
// suite's own.
test("the first account is created through the setup page", {
	tag: "@smoke",
}, async ({ page, request }) => {
	const status = await request.get("/api/v1/setup");
	expect(status.ok(), "the app answers its setup status").toBe(true);
	const { needed } = (await status.json()) as { needed: boolean };

	if (!needed) {
		const login = await request.post("/api/v1/auth/login", { data: ADMIN });
		expect(
			login.ok(),
			"This stack was set up with another account. The suite needs its own: run make clean, then make up.",
		).toBe(true);
		return;
	}

	// Any address leads a fresh installation to setup.
	await page.goto("/");
	await expect(page).toHaveURL(/\/setup$/);
	await expect(
		page.getByRole("heading", { name: "Welcome to GOtome" }),
	).toBeVisible();
	await expect(page.getByRole("img", { name: "GOtome" })).toBeVisible();

	await page.getByLabel("User name").fill(ADMIN.username);
	await page.getByLabel("Password").fill(ADMIN.password);
	await page.getByRole("button", { name: "Create the account" }).click();

	// Setup signs the new administrator in and shows the empty library.
	await expect(page).toHaveURL(/\/$/);
	await expect(
		page.getByRole("heading", { name: "No library yet" }),
	).toBeVisible();
	await expect(page.getByText(`Signed in as ${ADMIN.username}`)).toBeVisible();

	// And it is closed from then on.
	await page.goto("/setup");
	await expect(page).toHaveURL(/\/$/);
});
