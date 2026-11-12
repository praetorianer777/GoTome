import {
	type APIRequestContext,
	type Browser,
	expect,
	type Page,
	type TestInfo,
	request as playwrightRequest,
} from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn, signIn } from "../fixtures/app";
import { makePdf } from "../fixtures/pdf";

// Device tokens sign a person in: nothing here prints one, and the device
// side asks with Basic auth, so no token stands in an address a trace keeps.

/** Someone of their own, signed in in a browser of their own. */
async function person(
	page: Page,
	browser: Browser,
	testInfo: TestInfo,
	prefix: string,
): Promise<{ them: Page; username: string; close: () => Promise<void> }> {
	// Two people sign in, each through argon2.
	test.setTimeout(60_000);
	const username = `${prefix}-${testInfo.project.name}-${Date.now()}`;
	const password = `pw-${Date.now()}-long-enough`;
	await openSignedIn(page);
	const created = await page.request.post("/api/v1/users", {
		data: { username, password, role: "reader" },
	});
	expect(created.status()).toBe(201);
	const context = await browser.newContext({
		baseURL: testInfo.project.use.baseURL,
	});
	const them = await context.newPage();
	await them.goto("/");
	await signIn(them, { username, password });
	await expect(
		them.getByRole("heading", { name: "Library", level: 1 }),
	).toBeVisible();
	return { them, username, close: () => context.close() };
}

/** An e-reader, which signs in with its user name and token on every request. */
function eReader(
	testInfo: TestInfo,
	username: string,
	token: string,
): Promise<APIRequestContext> {
	return playwrightRequest.newContext({
		baseURL: testInfo.project.use.baseURL,
		httpCredentials: { username, password: token, send: "always" },
	});
}

test("a device added on the profile is shown its token once, reads with it, and is revoked", async ({
	page,
	browser,
}, testInfo) => {
	const { them, username, close } = await person(
		page,
		browser,
		testInfo,
		"kobo",
	);
	const name = `Kobo of ${username}`;
	await them.goto("/profile");
	const section = them.getByRole("region", { name: "E-readers" });
	await section.getByLabel("Name of the device").fill(name);
	await section.getByRole("button", { name: "Add a device" }).click();
	const created = section.getByRole("status");
	await expect(created).toContainText(`${name} is added.`);
	await expect(created).toContainText(
		"The token is shown only now. If it is lost, revoke the device and add it again.",
	);
	const token =
		(await created
			.locator("dt", { hasText: "Password (token)" })
			.locator("+ dd")
			.textContent()) ?? "";
	expect(token).toMatch(/^[a-z0-9]{4}(-[a-z0-9]{4}){4}$/);
	await expect(
		section.getByRole("link", { name: `Books on ${name}` }),
	).toBeVisible();
	await expect(section.getByText("Not used yet")).toBeVisible();

	const device = await eReader(testInfo, username, token);
	expect((await device.get("/api/v1/opds")).status()).toBe(200);

	await them.reload();
	await expect(
		section.getByRole("link", { name: `Books on ${name}` }),
	).toBeVisible();
	await expect(section.getByText(/^Last used /)).toBeVisible();
	await expect(section.getByRole("status")).toHaveCount(0);
	expect((await them.content()).includes(token)).toBe(false);

	await section.getByRole("button", { name: `Revoke ${name}` }).click();
	await expect(
		section.getByRole("link", { name: `Books on ${name}` }),
	).toHaveCount(0);
	expect((await device.get("/api/v1/opds")).status()).toBe(401);
	await device.dispose();
	await close();
});

test("a device that asks to be added is accepted or declined on the profile", async ({
	page,
	browser,
	request,
}, testInfo) => {
	const { them, username, close } = await person(
		page,
		browser,
		testInfo,
		"pair",
	);
	const ask = async (deviceName: string) => {
		const asked = await request.post("/api/v1/device/requests", {
			data: { username, deviceName },
		});
		expect(asked.status()).toBe(201);
		return (await asked.json()) as { id: string; secret: string; code: string };
	};
	const collect = async (ticket: { id: string; secret: string }) => {
		const answer = await request.post(
			`/api/v1/device/requests/${ticket.id}/token`,
			{ data: { secret: ticket.secret } },
		);
		expect(answer.status()).toBe(200);
		return (await answer.json()) as {
			state: string;
			username?: string;
			token?: string;
		};
	};
	const welcome = `Tolino of ${username}`;
	const stranger = `Stranger of ${username}`;
	const accepted = await ask(welcome);
	const declined = await ask(stranger);
	expect((await collect(accepted)).state).toBe("pending");

	await them.goto("/profile");
	const requests = them.getByRole("list", {
		name: "Devices asking to be added",
	});
	const asking = (deviceName: string) =>
		requests.getByRole("listitem", {
			name: `${deviceName} asks to be added to your account.`,
		});
	await expect(asking(welcome)).toContainText(accepted.code);
	await expect(asking(stranger)).toContainText(declined.code);

	await asking(stranger).getByRole("button", { name: "Decline" }).click();
	await expect(asking(stranger)).toHaveCount(0);
	const refused = await collect(declined);
	expect(refused.state).toBe("declined");
	expect(refused.token).toBeUndefined();

	await asking(welcome).getByRole("button", { name: "Accept" }).click();
	await expect(asking(welcome)).toHaveCount(0);
	const answer = await collect(accepted);
	expect(answer.state).toBe("accepted");
	expect(answer.username).toBe(username);
	// The token is handed out once.
	expect((await collect(accepted)).token).toBeUndefined();

	// The device the app came for is on the profile once it is made.
	const section = them.getByRole("region", { name: "E-readers" });
	await expect(
		section.getByRole("link", { name: `Books on ${welcome}` }),
	).toBeVisible({
		timeout: 10_000,
	});
	await expect(
		section.getByRole("link", { name: `Books on ${stranger}` }),
	).toHaveCount(0);
	const device = await eReader(testInfo, username, answer.token ?? "");
	expect((await device.get("/api/v1/opds")).status()).toBe(200);
	await device.dispose();
	await close();
});

test("a book is sent to a device, taken back, and listed on the device's page", async ({
	page,
	browser,
}, testInfo) => {
	const { them, username, close } = await person(
		page,
		browser,
		testInfo,
		"send",
	);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const library = (
		await (
			await page.request.post("/api/v1/libraries", {
				data: { name: `Sent ${stamp}`, mode: "managed", visibility: "shared" },
			})
		).json()
	).id as string;
	const book = async (title: string) => {
		const uploaded = await page.request.post(
			`/api/v1/libraries/${library}/uploads`,
			{
				multipart: {
					file: {
						name: `${title}.pdf`,
						mimeType: "application/pdf",
						buffer: makePdf(1, 0, title),
					},
				},
			},
		);
		expect(uploaded.status()).toBe(200);
		const { bookId } = await uploaded.json();
		expect(
			(
				await page.request.patch(`/api/v1/books/${bookId}`, {
					data: { title },
				})
			).status(),
		).toBe(200);
		return bookId as string;
	};
	const first = `Sent away ${stamp}`;
	const second = `Taken off ${stamp}`;
	const firstId = await book(first);
	const secondId = await book(second);
	const name = `Reader of ${username}`;
	const made = await them.request.post("/api/v1/auth/devices", {
		data: { name },
	});
	expect(made.status()).toBe(201);
	const { token, device: created } = (await made.json()) as {
		token: string;
		device: { id: string };
	};

	await them.goto(`/books/${firstId}`);
	await expect(
		them.getByRole("heading", { name: first, level: 1 }),
	).toBeVisible();
	const sending = them.getByRole("region", { name: "E-readers" });
	const send = sending.getByRole("combobox", { name: "Send to an e-reader" });
	await send.selectOption({ label: name });
	await expect(sending.getByText(`Waiting for ${name}`)).toBeVisible();
	await sending.getByRole("button", { name: "Take back" }).click();
	await expect(sending.getByText(`Waiting for ${name}`)).toHaveCount(0);
	await send.selectOption({ label: name });
	await expect(sending.getByText(`Waiting for ${name}`)).toBeVisible();

	// The device finds the book waiting for it and downloads it.
	const device = await eReader(testInfo, username, token);
	const waiting = await device.get("/api/v1/opds/device");
	expect(waiting.status()).toBe(200);
	const feed = await waiting.text();
	expect(feed).toContain(first);
	const download = feed.match(/href="(\/api\/v1\/opds\/device\/files\/[^"]+)"/);
	expect(download).not.toBeNull();
	const file = await device.get(download?.[1] ?? "");
	expect(file.status()).toBe(200);
	expect((await file.body()).subarray(0, 5).toString()).toBe("%PDF-");
	await device.dispose();
	await them.reload();
	await expect(
		sending.getByText(new RegExp(`^On ${name} since `)),
	).toBeVisible();
	await expect(sending.getByRole("button", { name: "Take back" })).toHaveCount(
		0,
	);

	expect(
		(
			await them.request.post(`/api/v1/devices/${created.id}/books`, {
				data: { books: [secondId] },
			})
		).status(),
	).toBe(200);
	await them.goto("/profile");
	await them
		.getByRole("region", { name: "E-readers" })
		.getByRole("link", { name: `Books on ${name}` })
		.click();
	await expect(them).toHaveURL(new RegExp(`/devices/${created.id}$`));
	await expect(them.getByRole("heading", { name, level: 1 })).toBeVisible();
	const list = them.getByRole("list", { name: "Books on this e-reader" });
	const delivered = list.getByRole("listitem", { name: first });
	const queued = list.getByRole("listitem", { name: second });
	await expect(delivered).toContainText("On the e-reader since");
	await expect(delivered.getByRole("button")).toHaveCount(0);
	await expect(queued).toContainText("Waiting since");
	await queued
		.getByRole("button", { name: `Take ${second} off this device` })
		.click();
	await expect(queued).toHaveCount(0);
	await expect(delivered).toBeVisible();
	await delivered.getByRole("link").click();
	await expect(
		them.getByRole("heading", { name: first, level: 1 }),
	).toBeVisible();

	await close();
	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("what a person is told of is chosen on the profile and kept", async ({
	page,
	browser,
}, testInfo) => {
	const { them, close } = await person(page, browser, testInfo, "told");
	await them.goto("/profile");
	const section = them.getByRole("region", { name: "Notifications" });
	await expect(section).toContainText(
		"What GOtome tells you of, here in the app.",
	);
	const added = section.getByRole("checkbox", {
		name: "New books in a library",
	});
	const wish = section.getByRole("checkbox", {
		name: "A book on my wishlist arrives",
	});
	await expect(added).toBeChecked();
	await expect(wish).toBeChecked();
	// A reader is not offered what only editors and administrators hear of.
	await expect(
		section.getByRole("checkbox", { name: "New possible duplicates" }),
	).toHaveCount(0);

	// The box shows what the server keeps, so it changes once it answers.
	await added.click();
	await expect(added).not.toBeChecked();
	await expect(added).toBeEnabled();
	await them.reload();
	await expect(added).not.toBeChecked();
	await expect(wish).toBeChecked();
	const settings = (await (
		await them.request.get("/api/v1/me/notification-settings")
	).json()) as { kinds: { kind: string; app: boolean }[] };
	expect(settings.kinds.find((k) => k.kind === "books.added")?.app).toBe(false);
	expect(settings.kinds.find((k) => k.kind === "wish.fulfilled")?.app).toBe(
		true,
	);

	await added.click();
	await expect(added).toBeChecked();
	await expect(added).toBeEnabled();
	await them.reload();
	await expect(added).toBeChecked();
	await close();
});
