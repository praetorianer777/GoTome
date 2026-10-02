import { queryOptions } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Timeline = components["schemas"]["AudioTimeline"];
export type Part = components["schemas"]["AudioPart"];

export function timelineQuery(bookId: string) {
	return queryOptions({
		queryKey: ["audio", bookId],
		queryFn: async () =>
			(await api.GET("/books/{bookId}/audio", { params: { path: { bookId } } })).data,
	});
}

/** The part that plays at a time of the whole, and the time within it. */
export function partAt(timeline: Timeline, ms: number): { index: number; offsetMs: number } {
	const parts = timeline.parts;
	let index = parts.findIndex((p) => ms < p.startMs + p.durationMs);
	if (index < 0) {
		index = parts.length - 1;
	}
	const part = parts[index];
	return { index, offsetMs: part ? Math.min(Math.max(ms - part.startMs, 0), part.durationMs) : 0 };
}

/** The chapter that plays at a time of the whole; -1 before the first. */
export function chapterAt(timeline: Timeline, ms: number): number {
	let at = -1;
	timeline.chapters.forEach((c, i) => {
		if (c.startMs <= ms) at = i;
	});
	return at;
}

/** A time as a clock shows it: 4:05, or 1:02:03 past an hour. */
export function formatClock(ms: number): string {
	const total = Math.max(0, Math.floor(ms / 1000));
	const h = Math.floor(total / 3600);
	const m = Math.floor((total % 3600) / 60);
	const s = String(total % 60).padStart(2, "0");
	return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}
