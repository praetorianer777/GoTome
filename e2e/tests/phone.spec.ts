import { expect } from "@playwright/test";
import { test } from "../fixtures/test";
import { openSignedIn } from "../fixtures/app";
import {
	crowdedTargets,
	overflowProblems,
	smallFields,
} from "../fixtures/quality";
import { open, seeded, VIEWS } from "../fixtures/views";
import { bookFold } from "../fixtures/quality";

// The phone's own rules (#390, docs/design.md "Phone, tablet, desktop"),
// beside the layout rules every project meets: no sideways scroll at the
// widths of small phones, fields large enough that iOS does not zoom into
// them, room of 8 pixels between neighbouring targets (the targets themselves are layout.spec.ts's), and what the book
// page shows without scrolling.
test.skip(({ isMobile }) => !isMobile, "the phone project's rules");
test.describe.configure({ timeout: 120_000 });

const GAP = 8;

test("every main view fits the small phones and its fields and targets suit a thumb", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const extra = await seeded(page, stamp);
	const problems: string[] = [];
	const views = [...VIEWS.map((v) => ({ label: v.path, ...v })), ...extra.views];
	try {
		for (const view of views) {
			const note = (rule: string, found: string[]) =>
				found.forEach((f) => problems.push(`${view.label}: ${rule}: ${f}`));
			for (const width of [320, 360, 390]) {
				await page.setViewportSize({ width, height: 844 });
				await open(page, view);
				note(`overflow at ${width} px`, await overflowProblems(page));
			}
			note("field under 16 px", await smallFields(page));
			note(`targets within ${GAP} px of each other`, await crowdedTargets(page, GAP));
		}
	} finally {
		await extra.cleanup();
	}
	expect(problems).toEqual([]);
});

test("the book page shows the cover, the title and Read in the first screen, within the thumb's reach", async ({
	page,
}, testInfo) => {
	const stamp = `${testInfo.project.name} ${Date.now()}`;
	await openSignedIn(page);
	const extra = await seeded(page, stamp);
	try {
		await page.setViewportSize({ width: 390, height: 844 });
		const view = extra.views[0];
		if (!view) throw new Error("no book was seeded");
		await open(page, view);
		const { height, cover, title, action } = await bookFold(page);
		expect(cover, "the cover").toBeLessThanOrEqual(height);
		expect(title, "the title").toBeLessThanOrEqual(height);
		expect(action.bottom, "the primary action ends in the first screen").toBeLessThanOrEqual(height);
		expect(action.top, "the primary action is in the lower two thirds").toBeGreaterThanOrEqual(height / 3);
	} finally {
		await extra.cleanup();
	}
});
