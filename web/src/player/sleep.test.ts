import { describe, expect, it } from "vitest";
import {
	FADE_MS,
	fadeFactor,
	inNight,
	NIGHT,
	ShakeDetector,
	sleepLeftMs,
} from "@/player/sleep";
import type { Timeline } from "@/player/timeline";

const timeline: Timeline = {
	durationMs: 110_000,
	complete: true,
	parts: [],
	chapters: [
		{ title: "One", fileId: "a", startMs: 0, endMs: 30_000 },
		{ title: "Two", fileId: "a", startMs: 30_000, endMs: 110_000 },
	],
};

describe("sleepLeftMs", () => {
	it("counts to the clock time, or to the end of the chapter at the rate", () => {
		expect(sleepLeftMs({ kind: "off" }, timeline, 0, 1, 0)).toBeUndefined();
		expect(sleepLeftMs({ kind: "until", at: 5000 }, timeline, 0, 1, 2000)).toBe(3000);
		expect(sleepLeftMs({ kind: "chapter", chapter: 0 }, timeline, 20_000, 1, 0)).toBe(10_000);
		expect(sleepLeftMs({ kind: "chapter", chapter: 0 }, timeline, 20_000, 2, 0)).toBe(5000);
	});
});

describe("fadeFactor", () => {
	it("is full until the fade begins, then runs down to nothing", () => {
		expect(fadeFactor(undefined)).toBe(1);
		expect(fadeFactor(FADE_MS)).toBe(1);
		expect(fadeFactor(FADE_MS / 2)).toBe(0.5);
		expect(fadeFactor(-5)).toBe(0);
	});
});

describe("inNight", () => {
	const at = (h: number, m = 0) => new Date(2026, 0, 1, h, m);
	const night = { ...NIGHT, enabled: true };
	it("runs past midnight", () => {
		expect(inNight(night, at(23))).toBe(true);
		expect(inNight(night, at(3))).toBe(true);
		expect(inNight(night, at(6))).toBe(false);
		expect(inNight(night, at(12))).toBe(false);
	});
	it("takes hours within a day, and the same hour twice as all day", () => {
		const day = { ...night, from: "13:00", to: "14:30" };
		expect(inNight(day, at(14, 29))).toBe(true);
		expect(inNight(day, at(14, 30))).toBe(false);
		expect(inNight({ ...night, from: "08:00", to: "08:00" }, at(17))).toBe(true);
	});
	it("is off unless enabled", () => {
		expect(inNight(NIGHT, at(23))).toBe(false);
	});
});

describe("ShakeDetector", () => {
	it("needs two hard jolts within a second, and ignores a single one", () => {
		const d = new ShakeDetector();
		expect(d.push(0, 0, 9.8, 0)).toBe(false);
		expect(d.push(20, 0, 9.8, 1000)).toBe(false);
		expect(d.push(21, 0, 9.8, 1050)).toBe(false);
		expect(d.push(-20, 0, 9.8, 1400)).toBe(true);
		expect(d.push(-20, 0, 9.8, 1900)).toBe(false);
	});
	it("forgets a jolt after a second", () => {
		const d = new ShakeDetector();
		d.push(25, 0, 0, 0);
		expect(d.push(25, 0, 0, 1500)).toBe(false);
	});
});
