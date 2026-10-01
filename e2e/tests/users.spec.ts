import { expect, test } from "@playwright/test";
import { openSignedIn, signIn } from "../fixtures/app";

test("an account is added, used, and disabled again", async ({
	page,
	browser,
}, testInfo) => {
	const username = `reader-${testInfo.project.name}-${Date.now()}`;
	const first = `first-${Date.now()}`;
	const second = `second-${Date.now()}`;
	await openSignedIn(page);

	await page
		.getByRole("navigation", { name: "Main" })
		.getByRole("link", { name: "Users" })
		.click();
	const form = page.getByRole("region", { name: "Add a user" });
	await form.getByLabel("User name").fill(username);
	await form.getByLabel("Password").fill(first);
	await form.getByRole("button", { name: "Add the user" }).click();
	const row = page.getByRole("listitem", { name: username });
	await expect(row.getByRole("combobox", { name: "Role" })).toHaveValue(
		"reader",
	);

	// The new person, in a browser of their own.
	const theirs = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await theirs.newPage();
	await them.goto("/");
	await signIn(them, { username, password: first });
	await expect(
		them.getByRole("heading", { name: "Library", level: 1 }),
	).toBeVisible();
	await them.getByRole("link", { name: `Signed in as ${username}` }).click();
	const password = them.getByRole("region", { name: "Password" });
	await password.getByLabel("Current password").fill(first);
	await password.getByLabel("New password").fill(second);
	await password.getByRole("button", { name: "Change the password" }).click();
	await expect(password.getByRole("status")).toHaveText(
		"Changed. Other devices are signed out.",
	);

	// Disabled, they are signed out at once.
	await row.getByRole("button", { name: "Disable" }).click();
	await expect(row.getByText("Disabled")).toBeVisible();
	await them.reload();
	await expect(them.getByRole("heading", { name: "Sign in" })).toBeVisible();
	await signIn(them, { username, password: second });
	await expect(
		them.getByText("The user name or the password is wrong."),
	).toBeVisible();
	await theirs.close();
});
