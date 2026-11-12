import { expect, type Page } from "@playwright/test";
import { test } from "../fixtures/test";
import { libraryOfBooks, openSignedIn } from "../fixtures/app";
import { VIEWS } from "../fixtures/views";

// axe-core checks each main view for what a screen reader or a keyboard
// cannot get past. @axe-core/playwright is not among the suite's packages
// yet, as the agent that wrote this does not download them: these tests
// skip until it is added (make e2e-npm ARGS="install -D @axe-core/playwright"),
// and then run with the rest of the full suite, in the light and the dark
// colours and, as the phone is a project of its own, on a phone. To run
// them alone: make test-e2e FULL=1 E2E_PROJECT=chromium ONLY="accessibility".
// The checks that need no package (landmarks, headings, live regions, the
// system's settings for motion and colours) run regardless.
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

const THEMES = ["light", "dark"] as const;

test("the sign-in page has no serious accessibility violations", async ({
	page,
}) => {
	const Axe = await loadAxe();
	test.skip(!Axe, `${AXE} is not installed`);
	await page.goto("/login");
	for (const colorScheme of THEMES) {
		await page.emulateMedia({ colorScheme });
		await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
		expect(
			await seriousViolations(page, Axe as AxeBuilderClass),
			`${colorScheme} colours`,
		).toEqual([]);
	}
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
		for (const colorScheme of THEMES) {
			await page.emulateMedia({ colorScheme });
			expect(
				await seriousViolations(page, Axe as AxeBuilderClass),
				`${colorScheme} colours`,
			).toEqual([]);
		}
	});
}

for (const view of VIEWS) {
	test(`${view.path} has one h1, a banner and a main landmark`, async ({
		page,
	}) => {
		await openSignedIn(page);
		await page.goto(view.path);
		await expect(
			page.getByRole("heading", { name: view.heading, level: 1 }),
		).toBeVisible();
		await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
		await expect(page.getByRole("main")).toHaveCount(1);
		await expect(page.getByRole("banner")).toHaveCount(1);
		await expect(page.locator("main#content")).toHaveCount(1);
		await expect(page.getByRole("link", { name: /skip/i }).first()).toHaveAttribute(
			"href",
			"#content",
		);
	});
}

test("a finished book is announced in a live region that was already there", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	const title = `Announced ${stamp}`;
	await openSignedIn(page);
	const { books } = await libraryOfBooks(page, `Announced ${stamp}`, [title]);
	await page.goto(`/books/${books[title]}`);
	await expect(page.locator('[aria-live="polite"][aria-atomic="true"]')).toHaveCount(1);
	await page
		.getByRole("region", { name: "Your reading" })
		.getByRole("combobox", { name: "Your status" })
		.selectOption({ label: "Completed" });
	await expect(
		page.locator('[aria-live="polite"][aria-atomic="true"]').getByText("Finished!"),
	).toBeVisible();
});

test("form errors are announced as alerts", async ({ page }) => {
	await page.goto("/login");
	await page.getByLabel("User name").fill("nobody");
	await page.getByLabel("Password").fill("wrong password");
	await page.getByRole("button", { name: "Sign in", exact: true }).click();
	await expect(page.getByRole("alert")).toBeVisible();
});

test("with reduced motion nothing on the page transitions or animates", async ({
	page,
}) => {
	await page.emulateMedia({ reducedMotion: "reduce" });
	await openSignedIn(page);
	await page.goto("/");
	await expect(page.getByRole("heading", { name: "Library", level: 1 })).toBeVisible();
	const moving = await page.evaluate(() => {
		const long = (value: string) =>
			value.split(",").some((v) => parseFloat(v) > 0.001 * (v.includes("ms") ? 1 : 1000));
		return [...document.querySelectorAll("*")]
			.filter((el) => {
				const style = getComputedStyle(el);
				return long(style.transitionDuration) || long(style.animationDuration);
			})
			.slice(0, 5)
			.map((el) => el.outerHTML.slice(0, 120));
	});
	expect(moving).toEqual([]);
});

test("in forced colours the focus ring and a button's edge are system colours", async ({
	page,
	browserName,
}) => {
	test.skip(browserName !== "chromium", "only Chromium emulates forced colours");
	await page.emulateMedia({ forcedColors: "active" });
	await page.goto("/login");
	const submit = page.getByRole("button", { name: "Sign in", exact: true });
	await expect(submit).toBeVisible();
	await expect(submit).toHaveCSS("outline-style", "solid");
	await page.getByLabel("User name").focus();
	await page.keyboard.press("Tab");
	await page.keyboard.press("Shift+Tab");
	const ring = await page.getByLabel("User name").evaluate((el) => {
		const style = getComputedStyle(el);
		return { width: style.outlineWidth, style: style.outlineStyle };
	});
	expect(ring).toEqual({ width: "3px", style: "solid" });
});
