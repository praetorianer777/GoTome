import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";

// The stack runs offline without an embedding model, so it has no vectors to
// recommend from; the browser is answered what the server would say, and the
// ranking itself is held by the integration tests.
function summary(id: string, title: string, libraryId: string) {
	return {
		id,
		libraryId,
		title,
		authors: [],
		authorKeys: [],
		formats: [],
		addedAt: "2026-01-01T00:00:00Z",
		status: "unread",
		placeholder: false,
		filesMissing: false,
		ereaderFindings: [],
	};
}

test("the For you row shows why a book is recommended, and Not interested takes it away", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const kept = `Kept ${stamp}`;
	const dismissed = `Dismissed ${stamp}`;
	await openSignedIn(page);
	const { id: library } = await libraryOfBooks(page, `Recommended ${stamp}`, [`Held ${stamp}`]);
	let books = [
		{
			book: summary("00000000-0000-0000-0000-0000000000a1", dismissed, library),
			because: [{ id: "00000000-0000-0000-0000-0000000000b1", title: "Liked One" }],
		},
		{
			book: summary("00000000-0000-0000-0000-0000000000a2", kept, library),
			because: [{ id: "00000000-0000-0000-0000-0000000000b2", title: "Liked Two" }],
		},
	];
	const dismissals: string[] = [];
	await page.route("**/api/v1/me/recommendations?*", (route) =>
		route.fulfill({ json: { hasProfile: true, books } }),
	);
	await page.route("**/api/v1/me/recommendations/*/dismiss", (route) => {
		const id = route.request().url().split("/").at(-2) ?? "";
		dismissals.push(id);
		books = books.filter((b) => b.book.id !== id);
		return route.fulfill({ status: 204 });
	});

	await page.goto("/");
	const toggle = page.getByRole("button", { name: "Reading and new arrivals" });
	await expect(toggle).toBeVisible();
	if ((await toggle.getAttribute("aria-expanded")) === "false") {
		await toggle.click();
	}
	const row = page.getByRole("region", { name: "For you" });
	await expect(row.getByRole("link", { name: new RegExp(`^${dismissed}`) })).toBeVisible();
	await expect(row.getByText("Because you liked Liked One")).toBeVisible();

	await row.getByRole("button", { name: `Not interested in ${dismissed}` }).click();
	await expect(row.getByRole("link", { name: new RegExp(`^${dismissed}`) })).toHaveCount(0);
	await expect(row.getByRole("link", { name: new RegExp(`^${kept}`) })).toBeVisible();
	expect(dismissals).toEqual(["00000000-0000-0000-0000-0000000000a1"]);

	await row.getByRole("link", { name: "More for you" }).click();
	await expect(page.getByRole("heading", { name: "For you", level: 1 })).toBeVisible();
	await expect(page.getByRole("link", { name: new RegExp(`^${kept}`) })).toBeVisible();
});
