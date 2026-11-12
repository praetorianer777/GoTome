import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { makeLibrary, uploadFile } from "../fixtures/accounts";
import { openSignedIn } from "../fixtures/app";
import { makePdf, proseOnEveryPage } from "../fixtures/pdf";

test("a waiting job is cancelled on the jobs page and tried again", async ({
	page,
}, testInfo) => {
	test.setTimeout(45_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const title = `Lookup ${stamp}`;
	await openSignedIn(page);
	const library = await makeLibrary(
		page.request,
		`Jobs controls ${stamp}`,
		"private",
	);
	// Reading the file queues its lookup half a minute later, so it waits
	// long enough to be cancelled.
	await uploadFile(
		page.request,
		library,
		`${title}.pdf`,
		"application/pdf",
		makePdf(2, 0, stamp),
	);

	await page.goto("/jobs");
	await expect(
		page.getByRole("heading", { name: "Background jobs", level: 1 }),
	).toBeVisible();
	const job = page.getByRole("listitem", { name: `Look up ${title}` });
	// The list is asked for once: the lookup is queued once the file is read.
	await expect(async () => {
		await page.getByRole("button", { name: "Waiting", exact: true }).click();
		await page.getByRole("button", { name: "All", exact: true }).click();
		await expect(job).toBeVisible({ timeout: 1_000 });
	}).toPass({ timeout: 20_000 });
	await expect(job.getByText("Scheduled")).toBeVisible();
	await job.getByRole("button", { name: `Cancel: Look up ${title}` }).click();
	await expect(job.getByText("Cancelled")).toBeVisible();

	await page.getByRole("button", { name: "Failed", exact: true }).click();
	await expect(job).toBeVisible();
	await job
		.getByRole("button", { name: `Try again: Look up ${title}` })
		.click();
	await expect(job).toBeHidden();
	await page.getByRole("button", { name: "All", exact: true }).click();
	await expect(job).toBeVisible();
	await expect(job.getByText("Cancelled")).toHaveCount(0);

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});

test("embedding is paused and started again from the jobs page", async ({
	page,
}, testInfo) => {
	// Settings are global: the browser projects, which share one stack and
	// run at once, would change them under each other's feet.
	test.skip(
		testInfo.project.name !== "chromium",
		"settings are global to the stack",
	);
	await openSignedIn(page);
	await page.goto("/jobs");
	const panel = page.getByRole("region", {
		name: "Working out what books are about",
	});
	await expect(panel.getByText(/^(Running, with |Paused\.)/)).toBeVisible();
	const wasRunning = await panel.getByText(/^Running/).isVisible();
	if (!wasRunning) {
		await panel.getByRole("button", { name: "Start" }).click();
		await expect(panel.getByText(/^Running, with /)).toBeVisible();
	}

	await panel.getByRole("button", { name: "Pause" }).click();
	await expect(
		panel.getByText(
			/^Paused\. .+ goes on with the books not yet done once it is started again\.$/,
		),
	).toBeVisible();
	await page.reload();
	await expect(panel.getByText(/^Paused\./)).toBeVisible();
	const settings = await (await page.request.get("/api/v1/settings")).json();
	expect(
		settings.settings.find(
			(s: { key: string }) => s.key === "embedding.enabled",
		).value,
	).toBe("off");

	await panel.getByRole("button", { name: "Start" }).click();
	await expect(panel.getByText(/^Running, with /)).toBeVisible();
	await expect(
		panel.getByRole("link", { name: "Choose the model" }),
	).toHaveAttribute("href", /^\/admin\/settings/);

	if (!wasRunning) {
		await panel.getByRole("button", { name: "Pause" }).click();
		await expect(panel.getByText(/^Paused\./)).toBeVisible();
	}
});

test("a library's text is read again for search from its row", async ({
	page,
}, testInfo) => {
	test.setTimeout(45_000);
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const name = `Reread ${stamp}`;
	await openSignedIn(page);
	const library = await makeLibrary(page.request, name, "private");
	await uploadFile(
		page.request,
		library,
		`Reread ${stamp}.pdf`,
		"application/pdf",
		makePdf(3, 0, stamp, proseOnEveryPage(3)),
	);
	const indexed = async () => {
		const status = await (
			await page.request.get(`/api/v1/search/status?library=${library}`)
		).json();
		return `${status.indexed} of ${status.files}`;
	};
	await expect.poll(indexed, { timeout: 20_000 }).toBe("1 of 1");

	await page.goto("/admin/libraries");
	const row = page.getByRole("listitem", { name });
	await row
		.getByRole("button", { name: "Read this library's text again for search" })
		.click();
	await expect(
		row.getByText(
			"Books whose text is read again in the background: 1. Search keeps the old text until then.",
		),
	).toBeVisible();
	await expect.poll(indexed, { timeout: 20_000 }).toBe("1 of 1");

	expect(
		(await page.request.delete(`/api/v1/libraries/${library}`)).status(),
	).toBe(204);
});
