import { describe, expect, it } from "vitest";
import { mediumOf } from "@/books/medium";

describe("mediumOf", () => {
	it("tells ebooks, audiobooks, both, and none", () => {
		expect(mediumOf(["epub", "pdf"])).toBe("ebook");
		expect(mediumOf(["m4b"])).toBe("audio");
		expect(mediumOf(["mp3", "epub"])).toBe("both");
		expect(mediumOf([])).toBeUndefined();
	});
});
