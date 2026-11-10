import { expect, type Page, test } from "@playwright/test";
import { bookFrame, openSignedIn } from "../fixtures/app";
import { makeEpub } from "../fixtures/ebooks";
import { newLibrary, uploadEpub } from "../fixtures/library";
import {
	observeVitals,
	readVitals,
	resetInteractions,
	slowestInteraction,
	throttle,
	type Vitals,
} from "../fixtures/quality";

// Core Web Vitals in the lab (#378, docs/performance.md): the largest
// contentful paint, the layout shifts and the latency of the actions people
// take most, with the browser's own observers and no field data. Only
// Chromium has them and can be throttled, so the desktop and the phone
// project run this and the others skip. The phone is throttled the same way
// on every run (fixtures/quality.ts: a CPU four times slower, 9 Mbit/s and
// 60 ms); the desktop is not. The limits are the vitals' "good" thresholds.
// A shared CI machine adds noise to any one measurement, so a load is
// measured three times and the median counts, an action three times and the
// fastest counts: the budget guards what the app does, not what else ran.
const LCP_MS = 2_500;
const CLS = 0.1;
const INTERACTION_MS = 200;
const TIMES = 3;

test.skip(({ browserName }) => browserName !== "chromium", "only Chromium is throttled and has Event Timing");
test.describe.configure({ timeout: 120_000 });

const median = (values: number[]) => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)] ?? 0;

async function seed(page: Page, stamp: string) {
	const library = await newLibrary(page, `Vitals ${stamp}`, "shared");
	const first = await uploadEpub(page, library, `Vitals One ${stamp}`, makeEpub(`Vitals One ${stamp}`, 3, stamp));
	await uploadEpub(page, library, `Vitals Two ${stamp}`, makeEpub(`Vitals Two ${stamp}`, 3, `${stamp} two`));
	const detail = (await (await page.request.get(`/api/v1/books/${first}`)).json()) as {
		files: { id: string }[];
	};
	return { library, book: first, file: detail.files[0]?.id ?? "" };
}

/** The page loaded three times, each time to the state `ready` waits for. */
async function loads(page: Page, path: string, ready: () => Promise<void>): Promise<Vitals[]> {
	const all: Vitals[] = [];
	for (let i = 0; i < TIMES; i++) {
		await page.goto(path);
		await ready();
		await page.waitForLoadState("networkidle");
		// The largest paint is final once the page is idle and left alone.
		await page.waitForTimeout(500);
		all.push(await readVitals(page));
	}
	return all;
}

test("the main views load fast and stay put, and the common actions answer at once", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const { library, book, file } = await seed(page, stamp);
	await observeVitals(page);
	if (testInfo.project.name === "phone") await throttle(page);
	const problems: string[] = [];
	const figures: string[] = [];
	const judge = (view: string, all: Vitals[]) => {
		const lcp = median(all.map((v) => v.lcp));
		const cls = median(all.map((v) => v.cls));
		const worst = all.find((v) => v.cls === cls);
		figures.push(
			`${view}: LCP ${Math.round(lcp)} ms, CLS ${cls.toFixed(3)}${(worst?.shifts ?? []).map((x) => ` [${x.value.toFixed(3)} ${x.node}]`).join("")}`,
		);
		if (lcp > LCP_MS) problems.push(`${view}: LCP ${Math.round(lcp)} ms is over ${LCP_MS}`);
		if (cls > CLS) problems.push(`${view}: CLS ${cls.toFixed(3)} is over ${CLS}`);
	};
	const fastest = async (action: string, times: number[]) => {
		const ms = Math.min(...times);
		figures.push(`${action}: ${Math.round(ms)} ms (${times.map(Math.round).join(", ")})`);
		if (ms > INTERACTION_MS) problems.push(`${action}: ${Math.round(ms)} ms is over ${INTERACTION_MS}`);
	};

	const books = page.getByRole("region", { name: "Books" });
	judge(
		"library",
		await loads(page, `/?library=${library}`, async () => {
			await expect(books.getByRole("link")).toHaveCount(2);
		}),
	);

	const toggle = page.getByRole("button", { name: "Filters", exact: true });
	if (await toggle.isVisible()) await toggle.click();
	const format = page.getByRole("complementary", { name: "Filters" }).getByRole("checkbox", { name: /^EPUB/ });
	const ticks: number[] = [];
	for (let i = 0; i < TIMES; i++) {
		await resetInteractions(page);
		await (i % 2 === 0 ? format.check() : format.uncheck());
		await expect(format).toBeChecked({ checked: i % 2 === 0 });
		await expect(books.getByRole("link")).toHaveCount(2);
		ticks.push(await slowestInteraction(page));
	}
	await fastest("ticking a filter", ticks);

	const opens: number[] = [];
	for (let i = 0; i < TIMES; i++) {
		if (await toggle.isVisible()) {
			if ((await toggle.getAttribute("aria-expanded")) === "true") await toggle.click();
		}
		await resetInteractions(page);
		await books.getByRole("link", { name: /^Vitals One/ }).click();
		await expect(page.getByRole("heading", { name: `Vitals One ${stamp}`, level: 1 })).toBeVisible();
		opens.push(await slowestInteraction(page));
		await page.goBack();
		await expect(books.getByRole("link")).toHaveCount(2);
	}
	await fastest("opening a book", opens);

	judge(
		"book page",
		await loads(page, `/books/${book}`, async () => {
			await expect(page.getByRole("heading", { name: `Vitals One ${stamp}`, level: 1 })).toBeVisible();
		}),
	);
	judge(
		"search",
		await loads(page, "/search?q=paragraph", async () => {
			await expect(page.getByRole("heading", { name: "Search the text", level: 1 })).toBeVisible();
		}),
	);
	judge(
		"reader",
		await loads(page, `/books/${book}/read/${file}`, async () => {
			await bookFrame(page, "Chapter 1, paragraph 1.");
		}),
	);

	const percent = page.getByText(/^\d+%$/);
	const turns: number[] = [];
	for (let i = 0; i < TIMES; i++) {
		const before = await percent.textContent();
		await resetInteractions(page);
		await page.getByRole("button", { name: "Page right" }).click();
		await expect(percent).not.toHaveText(before ?? "");
		turns.push(await slowestInteraction(page));
	}
	await fastest("turning a page", turns);

	await page.request.delete(`/api/v1/libraries/${library}`, { timeout: 30_000 });
	testInfo.annotations.push({ type: "vitals", description: figures.join("; ") });
	console.log(`vitals (${testInfo.project.name}):\n${figures.join("\n")}`);
	expect(problems).toEqual([]);
});
