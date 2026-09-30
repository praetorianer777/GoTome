import { expect, test } from "@playwright/test";
import { openSignedIn, signIn } from "../fixtures/app";
import { ADMIN } from "../fixtures/stack";

test("signing in leads to the empty library, signing out back to sign-in", {
	tag: "@smoke",
}, async ({ page }) => {
	await page.goto("/");
	await expect(page).toHaveURL(/\/login$/);
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();

	await signIn(page);
	await expect(page).toHaveURL(/\/$/);
	await expect(
		page.getByRole("heading", { name: "Library", level: 1 }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "No books yet" }),
	).toBeVisible();
	await expect(page.getByText(`Signed in as ${ADMIN.username}`)).toBeVisible();

	// The session survives a reload: it lives in a cookie, not in the page.
	await page.reload();
	await expect(
		page.getByRole("heading", { name: "Library", level: 1 }),
	).toBeVisible();

	await page.getByRole("button", { name: "Sign out" }).click();
	await expect(page).toHaveURL(/\/login$/);
	await page.goto("/");
	await expect(page).toHaveURL(/\/login$/);
});

test(
	"a wrong password is refused with the reason",
	{ tag: "@smoke" },
	async ({ page }, testInfo) => {
		await page.goto("/login");
		// A name of its own per run: failed attempts are counted per account, and
		// four browsers failing as the same one would trip the limit for all.
		await signIn(page, {
			username: `nobody-${Date.now()}-${testInfo.project.name}`,
			password: "not the password",
		});
		await expect(page.getByRole("alert")).toHaveText(
			"The user name or the password is wrong.",
		);
		await expect(page).toHaveURL(/\/login$/);
	},
);

test("after signing in, the person lands where they were going", async ({
	page,
}) => {
	await page.goto("/?view=grid");
	await expect(page).toHaveURL(/\/login\?redirect=/);
	await signIn(page);
	await expect(page).toHaveURL(/\/\?view=grid$/);
});

test("an unknown address says so and offers the way back", async ({ page }) => {
	await page.goto("/no/such/page");
	await expect(
		page.getByRole("heading", { name: "There is no page here" }),
	).toBeVisible();
	await expect(
		page.getByRole("link", { name: "Back to the library" }),
	).toBeVisible();
});

test("the chosen colours survive a reload", async ({ page }) => {
	await openSignedIn(page);
	await page.getByLabel("Colours").selectOption({ label: "Dark" });
	await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
	await page.reload();
	await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
	await expect(page.getByLabel("Colours")).toHaveValue("dark");
});

// Wider than the screen means a horizontal scrollbar, which on a phone is a
// page that slides sideways under the thumb.
for (const path of ["/login", "/"]) {
	test(`nothing on ${path} is wider than the screen`, async ({ page }) => {
		if (path === "/") {
			await openSignedIn(page);
		} else {
			await page.goto(path);
			await expect(
				page.getByRole("heading", { name: "Sign in" }),
			).toBeVisible();
		}
		const overflow = await page.evaluate(
			() =>
				document.documentElement.scrollWidth -
				document.documentElement.clientWidth,
		);
		expect(overflow).toBeLessThanOrEqual(0);
	});
}
