import { expect, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";
import { KEYCLOAK_URL } from "../fixtures/stack";

// The realm, its client and its people are deploy/keycloak/gotome-realm.json.
const ISSUER = `${KEYCLOAK_URL}/realms/gotome`;
const KEYCLOAK_PASSWORD = "e2e-keycloak-only";

test.beforeAll(async () => {
	let running = false;
	try {
		running = (await fetch(`${ISSUER}/.well-known/openid-configuration`)).ok;
	} catch {
		// Not started: answered below.
	}
	if (!running && process.env.CI) {
		throw new Error(
			`Keycloak does not answer at ${ISSUER}: the full gate starts it with make sso-up.`,
		);
	}
	test.skip(!running, "Keycloak is not running: start it with make sso-up.");
});

test("a person signs in through Keycloak with the role their groups give", async ({
	page,
	browser,
}, testInfo) => {
	// Settings are global: the browser projects, which share one stack and
	// run at once, would change them under each other's feet.
	test.skip(testInfo.project.name !== "chromium", "settings are global to the stack");
	await openSignedIn(page);
	const saved = await page.request.patch("/api/v1/settings", {
		data: {
			values: {
				"oidc.issuer": ISSUER,
				"oidc.clientId": "gotome",
				"oidc.clientSecret": "e2e-keycloak-client-secret",
				"oidc.name": "Keycloak",
				"oidc.editorGroups": "gotome-editors",
			},
		},
	});
	expect(saved.status()).toBe(200);

	// Someone without an account here, in a browser of their own, on their
	// way to a page of the app.
	const theirs = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await theirs.newPage();
	await them.goto("/collections");
	await them.getByRole("button", { name: "Sign in with Keycloak" }).click();

	// Keycloak's first page after its start takes a while to render.
	await them.locator("#username").fill("kc-editor", { timeout: 20_000 });
	await them.locator("#password").fill(KEYCLOAK_PASSWORD);
	await them.locator("#kc-login").click();

	await expect(
		them.getByRole("heading", { name: "Collections", level: 1 }),
	).toBeVisible();
	await them.getByRole("link", { name: "Signed in as kc-editor" }).click();
	await expect(them.getByText("Signed in as kc-editor, Editor.")).toBeVisible();
	await expect(
		them
			.getByRole("region", { name: "Single sign-on" })
			.getByText("kc-editor@example.org"),
	).toBeVisible();
	await theirs.close();
});
