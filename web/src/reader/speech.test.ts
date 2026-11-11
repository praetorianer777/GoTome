import { describe, expect, it } from "vitest";
import { SETTLE_MS, moveGuard } from "@/reader/speech";

const page = (text: string) => {
	const p = document.createElement("p");
	p.textContent = text;
	document.body.append(p);
	return (from: number, to: number) => {
		const r = document.createRange();
		r.setStart(p.firstChild as Node, from);
		r.setEnd(p.firstChild as Node, to);
		return r;
	};
};

describe("moveGuard", () => {
	const text = "One sentence. Two sentences. Three sentences.";

	it("takes a relocate that still holds the marked sentence for the reader's own", () => {
		const t = 1000;
		const guard = moveGuard(undefined, () => t);
		const at = page(text);
		guard.mark(at(14, 28));
		expect(guard.personal(at(0, text.length))).toBe(false);
	});

	it("takes a relocate within a moment of an own move for its aftermath", async () => {
		let t = 1000;
		const guard = moveGuard(undefined, () => t);
		await guard.own(async () => {
			expect(guard.personal(undefined)).toBe(false);
		});
		t += SETTLE_MS - 1;
		expect(guard.personal(undefined)).toBe(false);
		t += 2;
		expect(guard.personal(undefined)).toBe(true);
	});

	it("takes a turn to a page without the marked sentence for the person's", () => {
		const t = 1000;
		const guard = moveGuard(undefined, () => t);
		const at = page(text);
		guard.mark(at(0, 13));
		expect(guard.personal(page("Another page.")(0, 13))).toBe(true);
		expect(guard.personal(at(14, 28))).toBe(true);
	});
});
