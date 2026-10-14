import { expect, type Page, test } from "@playwright/test";
import { openSignedIn } from "../fixtures/app";

// axe-core checks each main view for what a screen reader or a keyboard
// cannot get past. @axe-core/playwright is not among the suite's packages
// yet, as the agent that wrote this does not download them: these tests
// skip until it is added (npm install -D @axe-core/playwright in e2e/),
// and then run with the rest of the full suite.
const AXE = "@axe-core/playwright";

interface AxeResults {
	violations: {
		id: string;
		impact?: string | null;
		help: string;
		nodes: { target: unknown }[];
	}[];
}
interface AxeBuilder {
	withTags(tags: string[]): AxeBuilder;
	analyze(): Promise<AxeResults>;
}
type AxeBuilderClass = new (options: { page: Page }) => AxeBuilder;

async function loadAxe(): Promise<AxeBuilderClass | undefined> {
	try {
		const loaded = (await import(AXE)) as { default: AxeBuilderClass };
		return loaded.default;
	} catch {
		return undefined;
	}
}

/** The violations that stop someone, rather than inconvenience them. */
async function seriousViolations(page: Page, Axe: AxeBuilderClass) {
	const results = await new Axe({ page })
		.withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
		.analyze();
	return results.violations
		.filter((v) => v.impact === "serious" || v.impact === "critical")
		.map(
			(v) =>
				`${v.id}: ${v.help} (${v.nodes.map((n) => JSON.stringify(n.target)).join(", ")})`,
		);
}

const VIEWS: { path: string; heading: string }[] = [
	{ path: "/", heading: "Library" },
	{ path: "/search", heading: "Search the text" },
	{ path: "/duplicates", heading: "Duplicates" },
	{ path: "/collections", heading: "Collections" },
	{ path: "/wishlist", heading: "Wishlist" },
	{ path: "/releases", heading: "New books" },
	{ path: "/stats", heading: "Your reading" },
	{ path: "/overview", heading: "Overview" },
	{ path: "/jobs", heading: "Background jobs" },
	{ path: "/trash", heading: "Trash" },
	{ path: "/profile", heading: "Your account" },
	{ path: "/admin/libraries", heading: "Libraries" },
	{ path: "/admin/users", heading: "Users" },
	{ path: "/admin/settings", heading: "Settings" },
];

test("the sign-in page has no serious accessibility violations", async ({
	page,
}) => {
	const Axe = await loadAxe();
	test.skip(!Axe, `${AXE} is not installed`);
	await page.goto("/login");
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	expect(await seriousViolations(page, Axe as AxeBuilderClass)).toEqual([]);
});

for (const view of VIEWS) {
	test(`${view.path} has no serious accessibility violations`, async ({
		page,
	}) => {
		const Axe = await loadAxe();
		test.skip(!Axe, `${AXE} is not installed`);
		await openSignedIn(page);
		await page.goto(view.path);
		await expect(
			page.getByRole("heading", { name: view.heading, level: 1 }),
		).toBeVisible();
		expect(await seriousViolations(page, Axe as AxeBuilderClass)).toEqual([]);
		// The same view in the dark.
		await page.emulateMedia({ colorScheme: "dark" });
		expect(await seriousViolations(page, Axe as AxeBuilderClass)).toEqual([]);
	});
}
