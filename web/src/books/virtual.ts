import { useWindowVirtualizer, type VirtualItem } from "@tanstack/react-virtual";
import {
	type RefObject,
	useEffect,
	useLayoutEffect,
	useRef,
	useState,
	useSyncExternalStore,
} from "react";

/**
 * Lists of fewer books render whole: every card is in the page, for the
 * browser's find, the keyboard and screen readers alike. From this many on,
 * only the rows in view and a few around them are, so that scrolling into a
 * library of 50,000 books keeps the page as light as its first screen.
 */
export const VIRTUAL_FROM = 300;

/** The grid's columns at each width, as its Tailwind classes lay it out. */
const COLUMNS: [query: string, columns: number][] = [
	["(min-width: 1024px)", 6],
	["(min-width: 768px)", 4],
	["(min-width: 640px)", 3],
];

function columnsNow(): number {
	if (typeof window === "undefined" || !window.matchMedia) {
		return 2;
	}
	return COLUMNS.find(([query]) => window.matchMedia(query).matches)?.[1] ?? 2;
}

function onColumnsChange(notify: () => void): () => void {
	if (typeof window === "undefined" || !window.matchMedia) {
		return () => {};
	}
	const lists = COLUMNS.map(([query]) => window.matchMedia(query));
	for (const list of lists) {
		list.addEventListener("change", notify);
	}
	return () => {
		for (const list of lists) {
			list.removeEventListener("change", notify);
		}
	};
}

/** How many books a row of the grid holds at the window's width. */
export function useColumns(): number {
	return useSyncExternalStore(onColumnsChange, columnsNow, () => 2);
}

/**
 * The rows measured in each list a person has left, by its address and
 * layout. Coming back, the list starts from them: with the estimate in
 * their place the page would be another height, and the place scrolled back
 * to another row.
 */
const measured = new Map<string, VirtualItem[]>();

/**
 * The first row in view when a person left a list, by the history entry it
 * was on: back to that entry, the list scrolls to that row. A pixel offset
 * would not do, as rows passed by unseen have only their estimate.
 */
const anchors = new Map<string, number>();

/**
 * A virtualizer over the window's scroll for a list that starts where the
 * element starts. Rows are measured as they render; the estimate only places
 * those not rendered yet. layout names what a row's height depends on
 * besides the address, such as the grid's columns.
 */
export function useWindowRows(
	count: number,
	estimate: number,
	list: RefObject<HTMLElement | null>,
	layout = "",
) {
	const key = `${window.location.pathname}${window.location.search}|${layout}`;
	const entry = (window.history.state as { __TSR_key?: string } | null)?.__TSR_key ?? "";
	const place = `${entry}|${key}`;
	const [start, setStart] = useState(0);
	useLayoutEffect(() => {
		const element = list.current;
		if (element) {
			setStart(element.getBoundingClientRect().top + window.scrollY);
		}
	}, [list]);
	const rows = useWindowVirtualizer({
		count,
		estimateSize: () => estimate,
		initialMeasurementsCache: measured.get(key),
		// A row has a height once laid out; one measured as nothing is not
		// laid out (or not yet), and takes the estimate.
		measureElement: (element) => element.getBoundingClientRect().height || estimate,
		overscan: 4,
		scrollMargin: start,
	});
	useEffect(() => () => void measured.set(key, rows.measurementsCache), [key, rows]);
	// Once the list knows where on the page it starts, which a row's offset
	// counts from, and after the router has put the window back where it
	// was.
	const restored = useRef(false);
	useEffect(() => {
		if (restored.current || start === 0) {
			return;
		}
		restored.current = true;
		const index = anchors.get(place);
		if (index) {
			requestAnimationFrame(() => rows.scrollToIndex(index, { align: "start" }));
		}
	}, [start, place, rows]);
	useEffect(() => () => void anchors.set(place, rows.range?.startIndex ?? 0), [place, rows]);
	return rows;
}
