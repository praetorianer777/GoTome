import type { Sleep } from "@/player/player";
import type { Timeline } from "@/player/timeline";

/** How long before the sleep timer ends the volume goes down to nothing. */
export const FADE_MS = 15_000;
/** How much a tap, a shake or the button adds to a timer. */
export const EXTEND_MS = 15 * 60_000;
/** How near its end a shake extends the timer. */
export const SHAKE_FROM_MS = 2 * 60_000;
/** How long after the timer stopped a shake still starts the book again. */
export const RESUME_MS = 30_000;

/**
 * How long is left until the timer stops the player, in the listener's time
 * (at the playback rate); undefined for no timer.
 */
export function sleepLeftMs(
	sleep: Sleep,
	timeline: Timeline,
	positionMs: number,
	rate: number,
	now: number,
): number | undefined {
	if (sleep.kind === "until") return sleep.at - now;
	if (sleep.kind === "chapter") {
		const end = timeline.chapters[sleep.chapter]?.endMs ?? timeline.durationMs;
		return (end - positionMs) / (rate > 0 ? rate : 1);
	}
	return undefined;
}

/** The volume, 0 to 1, with so long left on the timer. */
export function fadeFactor(leftMs: number | undefined): number {
	if (leftMs === undefined || leftMs >= FADE_MS) return 1;
	return Math.max(leftMs, 0) / FADE_MS;
}

/** The hours between which a timer starts by itself. */
export interface Night {
	enabled: boolean;
	/** "HH:MM". */
	from: string;
	to: string;
	minutes: number;
}

export const NIGHT: Night = { enabled: false, from: "22:00", to: "06:00", minutes: 30 };

function minutesOf(clock: string): number {
	const [h, m] = clock.split(":").map(Number);
	return (h ?? 0) * 60 + (m ?? 0);
}

/**
 * Whether the time falls in the hours, which may run past midnight. The same
 * hour twice means all day, so that a person who wants a timer at every
 * start can say so.
 */
export function inNight(night: Night, date: Date): boolean {
	if (!night.enabled) return false;
	const now = date.getHours() * 60 + date.getMinutes();
	const from = minutesOf(night.from);
	const to = minutesOf(night.to);
	if (from === to) return true;
	return from < to ? now >= from && now < to : now >= from || now < to;
}

/** Acceleration in m/s² past which a movement of the phone is a shake. */
const SHAKE = 18;
const SHAKE_WINDOW_MS = 1000;

/** Notices a shake from the readings of a device's motion sensor. */
export class ShakeDetector {
	private hits: number[] = [];

	/** Takes a reading; true when it is the second hard jolt within a second. */
	push(x: number, y: number, z: number, now: number): boolean {
		if (Math.hypot(x, y, z) < SHAKE) return false;
		this.hits = this.hits.filter((t) => now - t < SHAKE_WINDOW_MS);
		// Readings come many times a jolt; two within 100 ms are one.
		const last = this.hits.at(-1);
		if (last !== undefined && now - last < 100) return false;
		this.hits.push(now);
		if (this.hits.length < 2) return false;
		this.hits = [];
		return true;
	}
}
