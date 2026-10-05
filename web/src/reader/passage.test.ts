import { describe, expect, it } from "vitest";
import { closest, closestPage } from "@/reader/passage";

const at = (cfi: string, pre: string, match: string, post: string) => ({
	cfi,
	excerpt: { pre, match, post },
});

describe("the place a passage found by the server is at", () => {
	it("is the one whose surrounding words the passage shares most", () => {
		const found = [
			at("a", "Call me ", "Ishmael", ". Some years ago"),
			at("b", "the old man said, ", "Ishmael", " was never seen again at sea"),
			at("c", "whom they named ", "Ishmael", " after his father"),
		];
		expect(
			closest(found, "And the old man said: Ishmael was never seen again.")
				?.cfi,
		).toBe("b");
	});

	it("is the first on a tie, and none when nothing was found", () => {
		const found = [at("a", "", "whale", ""), at("b", "", "whale", "")];
		expect(closest(found, "WHALE")?.cfi).toBe("a");
		expect(closest([], "whale")).toBeUndefined();
	});
});

describe("the page a passage found by the server is on", () => {
	it("is the one holding the word whose text the passage shares most", () => {
		const pages = [
			{ page: 3, text: "The clerk counted the coins twice." },
			{ page: 4, text: "A whale was seen, they said, far off." },
			{ page: 5, text: "The whale counted the coins twice before closing." },
		];
		expect(closestPage(pages, "Whale", "the whale counted the coins")).toBe(5);
		expect(closestPage(pages, "walrus", "the walrus")).toBeUndefined();
	});
});
