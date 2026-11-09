import type { CDPSession, Page } from "@playwright/test";

/**
 * Measurements the quality specs share (#378, docs/design.md, docs/performance.md).
 * They run in the page and read the DOM, so they hold in every browser
 * project; each answers a list of problems, empty when the page keeps the
 * rule, so a failure names what is wrong and where.
 */

/** Where an element is, for a message: tag, a few classes and its text. */
const DESCRIBE = `(el) => {
	const cls = String(el.getAttribute("class") ?? "").split(/\\s+/).slice(0, 3).join(".");
	const text = (el.getAttribute("aria-label") ?? el.textContent ?? "").trim().replace(/\\s+/g, " ").slice(0, 30);
	return el.tagName.toLowerCase() + (cls ? "." + cls : "") + ' "' + text + '"';
}`;

/** Elements nobody sees: hidden, collapsed, or text for screen readers only. */
const SEEN = `(el) => {
	const r = el.getBoundingClientRect();
	if (r.width <= 1 || r.height <= 1) return false;
	for (let e = el; e; e = e.parentElement) {
		const s = getComputedStyle(e);
		if (s.display === "none" || s.visibility === "hidden" || s.opacity === "0") return false;
		if (e.hasAttribute("inert") || e.getAttribute("aria-hidden") === "true") return false;
		// What a closed disclosure holds is laid out nowhere.
		if (e.tagName === "DETAILS" && !e.open && el.closest("summary") === null && el !== e && !e.querySelector(":scope > summary")?.contains(el)) return false;
	}
	return true;
}`;

/** The page is not wider than the window, nor any box past its right edge. */
export async function overflowProblems(page: Page): Promise<string[]> {
	return page.evaluate(`(() => {
		const describe = ${DESCRIBE};
		const seen = ${SEEN};
		const found = [];
		const wide = document.documentElement.scrollWidth - document.documentElement.clientWidth;
		if (wide > 0) found.push("the page is " + wide + "px wider than the window");
		const limit = document.documentElement.clientWidth + 1;
		for (const el of document.body.querySelectorAll("*")) {
			if (!seen(el)) continue;
			const style = getComputedStyle(el);
			if (style.position === "fixed" || style.display === "inline" || style.display === "contents") continue;
			let rail = false;
			for (let up = el.parentElement; up; up = up.parentElement) {
				const x = getComputedStyle(up).overflowX;
				if (x === "auto" || x === "scroll") rail = true;
			}
			if (rail) continue;
			if (el.getBoundingClientRect().right > limit) found.push(describe(el) + " reaches past the window");
		}
		return found.slice(0, 8);
	})()`) as Promise<string[]>;
}

/**
 * WCAG 1.4.12: the author's text spacing may be overridden by the reader
 * (line height 1.5, paragraphs 2 times the size, letters 0.12, words 0.16),
 * and nothing may be cut off or lie over other text. Applied as a style
 * sheet, which the caller removes with the returned function.
 */
export async function withTextSpacing<T>(
	page: Page,
	check: () => Promise<T>,
): Promise<T> {
	const handle = await page.addStyleTag({
		content: `* { line-height: 1.5 !important; letter-spacing: 0.12em !important; word-spacing: 0.16em !important; }
p { margin-bottom: 2em !important; }`,
	});
	try {
		return await check();
	} finally {
		await handle.evaluate((el) => (el as unknown as { remove(): void }).remove());
	}
}

/** Text cut off by a box that clips, or lying over other text. */
export async function clippedTextProblems(page: Page): Promise<string[]> {
	return page.evaluate(`(() => {
		const describe = ${DESCRIBE};
		const seen = ${SEEN};
		const found = [];
		const leaves = [];
		for (const el of document.body.querySelectorAll("*")) {
			if (!seen(el)) continue;
			const own = [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim());
			if (!own) continue;
			const style = getComputedStyle(el);
			const clamp = style.getPropertyValue("-webkit-line-clamp");
			const ellipsis = style.textOverflow === "ellipsis";
			const clips = (v) => v === "hidden" || v === "clip";
			// A box that clips is the one that holds the text, or the nearest
			// one around it that has a fixed height.
			for (let box = el; box && box !== document.body; box = box.parentElement) {
				const s = getComputedStyle(box);
				if (!clips(s.overflowY) && !clips(s.overflowX)) continue;
				if (ellipsis || (clamp && clamp !== "none")) break;
				if (clips(s.overflowY) && box.scrollHeight > box.clientHeight + 1 && s.height !== "auto" && box.clientHeight > 0) {
					found.push(describe(el) + " is cut off by " + describe(box) + " (" + (box.scrollHeight - box.clientHeight) + "px)");
					break;
				}
			}
			let layer = false;
			for (let up = el; up && up !== document.body; up = up.parentElement) {
				const position = getComputedStyle(up).position;
				if (position === "fixed" || position === "sticky" || position === "absolute") layer = true;
			}
			if (!layer) leaves.push(el);
		}
		for (const [i, a] of leaves.entries()) {
			const ra = a.getBoundingClientRect();
			for (const b of leaves.slice(i + 1)) {
				if (a.contains(b) || b.contains(a)) continue;
				const rb = b.getBoundingClientRect();
				const across = Math.min(ra.right, rb.right) - Math.max(ra.left, rb.left);
				const down = Math.min(ra.bottom, rb.bottom) - Math.max(ra.top, rb.top);
				// More than a line's leading, so a box that only touches is not a hit.
				if (across > 2 && down > 4) found.push(describe(a) + " lies over " + describe(b));
			}
		}
		return found.slice(0, 8);
	})()`) as Promise<string[]>;
}

/** Filled primary buttons (and links that look like them) in view: at most one. */
export async function primaryButtons(page: Page): Promise<string[]> {
	return page.evaluate(`(() => {
		const describe = ${DESCRIBE};
		const seen = ${SEEN};
		return [...document.querySelectorAll("a, button")]
			.filter((el) => el.classList.contains("bg-brand-strong") && seen(el))
			.map(describe);
	})()`) as Promise<string[]>;
}

/**
 * Controls smaller than the minimum side (24 CSS pixels: WCAG 2.5.8; 44 on a
 * phone). A link inside a sentence is exempt, as the criterion has it, and
 * so is a control that another of the minimum size lies over (a cover's link
 * and its title). Gaps of 8 pixels are not measured here.
 */
export async function smallTargets(page: Page, min: number): Promise<string[]> {
	return page.evaluate(`((min) => {
		const describe = ${DESCRIBE};
		const seen = ${SEEN};
		const found = [];
		const inline = (el) => {
			if (el.tagName !== "A") return false;
			const p = el.parentElement;
			return !!p && getComputedStyle(el).display === "inline" && [...p.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim().length > 1);
		};
		const sel = "a[href], button, input:not([type=hidden]), select, textarea, summary, [role=button], [role=tab], [role=menuitem], [role=switch]";
		for (const el of document.querySelectorAll(sel)) {
			if (!seen(el) || inline(el) || el.matches(".sr-only, [tabindex='-1']")) continue;
			if (el.matches("input[type=checkbox], input[type=radio]") && el.closest("label")) {
				const r = el.closest("label").getBoundingClientRect();
				if (r.width >= min && r.height >= min) continue;
			}
			const r = el.getBoundingClientRect();
			// A link of words is as wide as they are; its height is the target's.
			const words = el.tagName === "A" && (el.textContent || "").trim().length > 0;
			if ((r.width < (words ? Math.min(min, 24) : min) - 0.5) || r.height < min - 0.5) {
				found.push(describe(el) + " is " + Math.round(r.width) + "x" + Math.round(r.height));
			}
		}
		return found;
	})(${min})`) as Promise<string[]>;
}

/**
 * Fields set in under 16 pixels, which iOS zooms into when they take focus
 * (#390). Checkboxes, radios and file fields have no text to type.
 */
export async function smallFields(page: Page, min = 16): Promise<string[]> {
	return page.evaluate(`((min) => {
		const describe = ${DESCRIBE};
		const seen = ${SEEN};
		return [...document.querySelectorAll("input:not([type=checkbox]):not([type=radio]):not([type=hidden]):not([type=file]):not([type=range]), select, textarea")]
			.filter((el) => seen(el) && parseFloat(getComputedStyle(el).fontSize) < min)
			.map((el) => describe(el) + " is set in " + getComputedStyle(el).fontSize);
	})(${min})`) as Promise<string[]>;
}

/**
 * Controls closer to each other than `gap` pixels (WCAG 2.5.8 asks for room
 * around a small target; a phone's thumb for 8 pixels around every one).
 * Controls that hold each other, and those of one joined group, are not
 * neighbours.
 */
export async function crowdedTargets(page: Page, gap: number): Promise<string[]> {
	return page.evaluate(`((gap) => {
		const describe = ${DESCRIBE};
		const seen = ${SEEN};
		const sel = "a[href], button, input:not([type=hidden]), select, textarea, summary, [role=button], [role=tab], [role=menuitem], [role=switch]";
		const inline = (el) => el.tagName === "A" && getComputedStyle(el).display === "inline";
		const all = [...document.querySelectorAll(sel)].filter((el) => seen(el) && !inline(el) && !el.matches(".sr-only, [tabindex='-1']"));
		const found = [];
		for (const [i, a] of all.entries()) {
			const ra = a.getBoundingClientRect();
			for (const b of all.slice(i + 1)) {
				if (a.contains(b) || b.contains(a)) continue;
				if (a.closest("label") && a.closest("label") === b.closest("label")) continue;
				// A bar that sticks passes over the fields as the page scrolls.
				const sticks = (el) => { for (let e = el; e; e = e.parentElement) { const p = getComputedStyle(e).position; if (p === "fixed" || p === "sticky") return true; } return false; };
				if (sticks(a) || sticks(b)) continue;
				const rb = b.getBoundingClientRect();
				// The two halves of a joined choice (Covers, List) touch by design.
				if (a.hasAttribute("aria-pressed") && b.hasAttribute("aria-pressed") && a.parentElement === b.parentElement && getComputedStyle(b).marginLeft === "-1px") continue;
				const dx = Math.max(ra.left - rb.right, rb.left - ra.right, 0);
				const dy = Math.max(ra.top - rb.bottom, rb.top - ra.bottom, 0);
				if (Math.hypot(dx, dy) < gap - 0.5) {
					found.push(describe(a) + " is " + Math.round(Math.hypot(dx, dy)) + "px from " + describe(b));
				}
			}
		}
		return found;
	})(${gap})`) as Promise<string[]>;
}

/** Where the book page's cover, title and primary action end, in a window of this height. */
export async function bookFold(page: Page): Promise<{
	height: number;
	cover: number;
	title: number;
	action: { top: number; bottom: number };
}> {
	return page.evaluate(`(() => {
		const bottom = (el) => (el ? el.getBoundingClientRect().bottom : Infinity);
		const action = document.querySelector("article a.bg-brand-strong, article button.bg-brand-strong, main a.bg-brand-strong");
		const r = action ? action.getBoundingClientRect() : { top: Infinity, bottom: Infinity };
		return {
			height: window.innerHeight,
			cover: bottom(document.querySelector("article img, article [class*=aspect-]")),
			title: bottom(document.querySelector("h1")),
			action: { top: r.top, bottom: r.bottom },
		};
	})()`) as Promise<{ height: number; cover: number; title: number; action: { top: number; bottom: number } }>;
}

/** Tabs through the page: every control that takes focus shows where it is. */
export async function focusProblems(page: Page, steps = 60): Promise<string[]> {
	await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
	const found = new Set<string>();
	for (let i = 0; i < steps; i++) {
		await page.keyboard.press("Tab");
		const problem = (await page.evaluate(`(() => {
			const describe = ${DESCRIBE};
			const el = document.activeElement;
			if (!el || el === document.body) return "";
			// A book's own pages draw their own focus; a date field's calendar
			// button takes focus in the browser's own part of it.
			if (el.tagName.includes("-") || !el.matches(":focus-visible")) return "";
			const s = getComputedStyle(el);
			const outline = s.outlineStyle !== "none" && parseFloat(s.outlineWidth) > 0;
			const ring = s.boxShadow !== "none";
			return outline || ring ? "" : describe(el) + " shows no focus";
		})()`)) as string;
		if (problem) found.add(problem);
	}
	return [...found];
}

/** Largest contentful paint, layout shifts and interactions of one page load. */
export interface Vitals {
	lcp: number;
	cls: number;
	/** Shifts of elements gone before they were reported: a book's own pages in the reader's frame. Not counted. */
	unattributed: number;
	interactions: { name: string; duration: number; delay: number; work: number }[];
	shifts: { value: number; node: string }[];
}

/**
 * Starts the browser's own observers before the page's scripts run. The
 * layout shifts count the way CLS does: those without a recent input.
 */
export async function observeVitals(page: Page): Promise<void> {
	await page.addInitScript(() => {
		const w = window as unknown as { __vitals: Vitals };
		w.__vitals = { lcp: 0, cls: 0, unattributed: 0, interactions: [], shifts: [] };
		const watch = (type: string, init: PerformanceObserverInit, each: (e: PerformanceEntry & Record<string, number | boolean>) => void) => {
			try {
				new PerformanceObserver((list) => {
					for (const e of list.getEntries()) each(e as never);
				}).observe({ type, buffered: true, ...init });
			} catch {
				// An engine without this entry type has no number to give.
			}
		};
		watch("largest-contentful-paint", {}, (e) => {
			w.__vitals.lcp = e.startTime as number;
		});
		watch("layout-shift", {}, (e) => {
			if (e.hadRecentInput) return;
			const sources = (e as unknown as { sources?: { node?: Element; previousRect: DOMRectReadOnly; currentRect: DOMRectReadOnly }[] }).sources ?? [];
			const rect = (r: DOMRectReadOnly) => `${Math.round(r.x)},${Math.round(r.y)} ${Math.round(r.width)}x${Math.round(r.height)}`;
			const where = sources.slice(0, 3).map((x) => {
				const n = x.node;
				const name = n ? `${n.tagName.toLowerCase()}.${String(n.getAttribute("class") ?? "").split(" ").slice(0, 2).join(".")} "${(n.textContent ?? "").trim().slice(0, 20)}"` : "removed";
				return `${name} ${rect(x.previousRect)} -> ${rect(x.currentRect)}`;
			});
			if (sources.some((x) => x.node)) w.__vitals.cls += e.value as number;
			else w.__vitals.unattributed += e.value as number;
			w.__vitals.shifts.push({ value: e.value as number, node: where.join(" | ") });
		});
		watch("event", { durationThreshold: 16 } as PerformanceObserverInit, (e) => {
			if ((e.interactionId as number) > 0) {
				w.__vitals.interactions.push({
					name: e.name,
					duration: e.duration,
					delay: (e.processingStart as number) - e.startTime,
					work: (e.processingEnd as number) - (e.processingStart as number),
				});
			}
		});
	});
}

export async function readVitals(page: Page): Promise<Vitals> {
	return page.evaluate(() => (window as unknown as { __vitals: Vitals }).__vitals);
}

/** Forgets the interactions so far, to measure the next action alone. */
export async function resetInteractions(page: Page): Promise<void> {
	await page.evaluate(() => {
		(window as unknown as { __vitals: Vitals }).__vitals.interactions = [];
	});
}

/** The longest interaction since the last reset, as INP takes the worst. */
export async function slowestInteraction(page: Page): Promise<number> {
	// An interaction's frame is presented after the handlers ran, which the
	// observer reports a moment later.
	await page.evaluate(() => new Promise((done) => requestAnimationFrame(() => setTimeout(done, 200))));
	const { interactions } = await readVitals(page);
	return Math.max(0, ...interactions.map((i) => i.duration));
}

/**
 * The lab's phone: the CPU four times slower and a network of 9 Mbit/s with
 * 60 ms round trip, the same on every run. Only Chromium can be told to.
 */
export async function throttle(page: Page): Promise<CDPSession> {
	const cdp = await page.context().newCDPSession(page);
	await cdp.send("Emulation.setCPUThrottlingRate", { rate: 4 });
	await cdp.send("Network.enable");
	await cdp.send("Network.emulateNetworkConditions", {
		offline: false,
		latency: 60,
		downloadThroughput: (9 * 1024 * 1024) / 8,
		uploadThroughput: (3 * 1024 * 1024) / 8,
	});
	return cdp;
}
