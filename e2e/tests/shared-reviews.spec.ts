import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import { makeAccount, makeLibrary, signedIn, uploadFile } from "../fixtures/accounts";
import { openSignedIn } from "../fixtures/app";
import { makePdf } from "../fixtures/pdf";

async function saved(page: Page, click: () => Promise<void>) {
	const done = page.waitForResponse(
		(r) => r.url().endsWith("/review") && r.request().method() === "PUT",
	);
	await click();
	expect((await done).status()).toBe(204);
}

test("a review is shared with others once its writer chooses, and not while it is private", async ({
	page,
	browser,
}, testInfo) => {
	test.setTimeout(90_000);
	const stamp = `${testInfo.project.name}-${Date.now()}`;
	await openSignedIn(page);
	const writer = await makeAccount(page.request, "reader", `w-${stamp}`);
	const viewer = await makeAccount(page.request, "reader", `v-${stamp}`);
	const library = await makeLibrary(page.request, `Shared reviews ${stamp}`, "shared");
	const { bookId } = await uploadFile(
		page.request,
		library,
		`Shared reviews ${stamp}.pdf`,
		"application/pdf",
		makePdf(2, 0, stamp),
	);
	const text = `A long Wolfeschlegelsteinhausenbergerdorff review ${stamp}`;

	const mine = await signedIn(browser, testInfo, writer);
	const theirs = await signedIn(browser, testInfo, viewer);
	try {
		const rated = await mine.page.request.put(`/api/v1/books/${bookId}/reading`, {
			data: { rating: 4 },
		});
		expect(rated.ok()).toBe(true);
		await mine.page.goto("/profile");
		const share = mine.page.getByRole("switch", {
			name: "Share my ratings and reviews with others on this server",
		});
		await expect(share).toHaveAttribute("aria-checked", "false");
		await share.click();
		await expect(share).toHaveAttribute("aria-checked", "true");

		await mine.page.goto(`/books/${bookId}`);
		await mine.page.getByLabel("Your review").fill(text);
		const save = mine.page.getByRole("button", { name: "Save review" });
		await saved(mine.page, () => save.click());
		await expect(
			mine.page.getByText("Others on this server see this review."),
		).toBeVisible();

		await theirs.page.goto(`/books/${bookId}`);
		const reviews = theirs.page.getByRole("region", { name: "Reviews" });
		await expect(reviews.getByText(text)).toBeVisible();
		await expect(reviews.getByText(/4\.0 of 5 stars, rated by 1/)).toBeVisible();
		await reviews.getByRole("link", { name: writer.username }).click();
		await expect(
			theirs.page.getByRole("heading", { name: `Reviews by ${writer.username}`, level: 1 }),
		).toBeVisible();
		await expect(theirs.page.getByText(text)).toBeVisible();

		await mine.page.getByRole("checkbox", { name: "Keep this review private" }).check();
		await saved(mine.page, () => save.click());
		await expect(mine.page.getByText("Only you see this review.")).toBeVisible();
		await theirs.page.goto(`/books/${bookId}`);
		await expect(
			theirs.page.getByRole("region", { name: "Reviews" }).getByText(text),
		).toHaveCount(0);
		await theirs.page.goto(`/people/${writer.username}`);
		await expect(
			theirs.page.getByRole("heading", { name: "This person shares no reviews with you." }),
		).toBeVisible();
	} finally {
		await mine.context.close();
		await theirs.context.close();
	}
});
