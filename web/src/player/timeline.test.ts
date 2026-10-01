import { describe, expect, it } from "vitest";
import { chapterAt, formatClock, partAt, type Timeline } from "@/player/timeline";

const timeline: Timeline = {
	durationMs: 110_000,
	complete: true,
	parts: [
		{ fileId: "a", format: "mp3", mediaType: "audio/mpeg", name: "1.mp3", startMs: 0, durationMs: 60_000 },
		{ fileId: "b", format: "mp3", mediaType: "audio/mpeg", name: "2.mp3", startMs: 60_000, durationMs: 50_000 },
	],
	chapters: [
		{ title: "One", fileId: "a", startMs: 0, endMs: 30_000 },
		{ title: "Two", fileId: "a", startMs: 30_000, endMs: 60_000 },
		{ title: "Three", fileId: "b", startMs: 60_000, endMs: 110_000 },
	],
};

describe("the timeline", () => {
	it("finds the part and the time within it", () => {
		expect(partAt(timeline, 0)).toEqual({ index: 0, offsetMs: 0 });
		expect(partAt(timeline, 59_999)).toEqual({ index: 0, offsetMs: 59_999 });
		expect(partAt(timeline, 60_000)).toEqual({ index: 1, offsetMs: 0 });
		expect(partAt(timeline, 75_500)).toEqual({ index: 1, offsetMs: 15_500 });
		expect(partAt(timeline, 500_000)).toEqual({ index: 1, offsetMs: 50_000 });
	});

	it("finds the chapter", () => {
		expect(chapterAt(timeline, 0)).toBe(0);
		expect(chapterAt(timeline, 45_000)).toBe(1);
		expect(chapterAt(timeline, 60_000)).toBe(2);
	});

	it("reads times as a clock does", () => {
		expect(formatClock(5_000)).toBe("0:05");
		expect(formatClock(245_000)).toBe("4:05");
		expect(formatClock(3_723_000)).toBe("1:02:03");
	});
});
