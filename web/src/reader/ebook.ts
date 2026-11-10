import { Overlayer } from "foliate-js/overlayer.js";
import "foliate-js/view.js";
import type { FoliateTocItem, View } from "foliate-js/view.js";
import { closest, type Found } from "@/reader/passage";
import { type Sentence, type Source, sentenceSpans } from "@/reader/speech";

export type Flow = "paginated" | "scrolled";
export type Theme = "light" | "sepia" | "dark";

/** How the person likes their books set. */
export interface Look {
	flow: Flow;
	theme: Theme;
	/** The text's size, in percent of the book's own. */
	fontSize: number;
}

/** Where the reader is: the place as an EPUB CFI, and how far into the book. */
export interface Place {
	cfi: string;
	fraction: number;
	chapter?: string;
	/** The last page of the book is in view. */
	atEnd: boolean;
}

export interface TocEntry {
	key: string;
	label: string;
	href: string;
	depth: number;
}

/** A sentence of the book's text, with where it stands in the section. */
export interface Spoken extends Sentence {
	start: number;
	end: number;
}

export interface Ebook {
	toc: TocEntry[];
	/** The language the book says it is in, as a BCP 47 tag. */
	language?: string;
	/** What read-aloud reads from. */
	speech: Source<Spoken>;
	/** Goes to a CFI or a table of contents entry's href. */
	goTo(target: string): Promise<void>;
	/**
	 * Looks for the word in the book, outlines where it stands, and goes to
	 * the place whose text is most like the passage. False when the word is
	 * nowhere in the book.
	 */
	find(word: string, passage: string): Promise<boolean>;
	/**
	 * The places that hold the words, at most limit of them, outlined in the
	 * book until clearSearch.
	 */
	search(query: string, limit: number): Promise<Found[]>;
	clearSearch(): void;
	/** Goes as far into the book as the fraction, 0 its start and 1 its end. */
	goToFraction(fraction: number): Promise<void>;
	/** Turns the page towards the left or the right, whichever way the book reads. */
	goLeft(): Promise<void>;
	goRight(): Promise<void>;
	/** Turns to the next or the previous page in reading order. */
	next(): Promise<void>;
	prev(): Promise<void>;
	setLook(look: Look): void;
	close(): void;
}

const SPOKEN_KEY = "gotome-speech";
const BLOCKS = "p,div,li,h1,h2,h3,h4,h5,h6,blockquote,td,th,dd,dt,section,article,pre,figcaption";
const SKIPPED = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "RT", "RP", "HEAD"]);

interface Piece {
	node: Text;
	from: number;
	at: number;
}

/**
 * The text of a section from the range on, with where each text node starts
 * in it. Blocks are told apart by a line break, as no text ends a heading.
 */
function textFrom(doc: Document, from: Range | undefined) {
	const pieces: Piece[] = [];
	let text = "";
	let block: Element | null = null;
	const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
	for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
		const parent = node.parentElement;
		if (!parent || SKIPPED.has(parent.tagName) || parent.closest("script,style,noscript,rt")) {
			continue;
		}
		let begin = 0;
		if (from) {
			if (from.comparePoint(node, node.length) < 0) continue;
			if (node === from.startContainer) begin = from.startOffset;
		}
		const now = parent.closest(BLOCKS);
		if (text && now !== block) text += "\n";
		block = now;
		pieces.push({ node, from: begin, at: text.length });
		text += node.data.slice(begin);
	}
	return { pieces, text };
}

/** The point in the section's text, as a position in a text node. */
function pointAt(pieces: Piece[], offset: number, end: boolean): [Text, number] | undefined {
	let low = 0;
	let high = pieces.length - 1;
	while (low < high) {
		const mid = Math.ceil((low + high) / 2);
		if ((pieces[mid]?.at ?? 0) <= offset) low = mid;
		else high = mid - 1;
	}
	const piece = pieces[low];
	if (!piece) return undefined;
	const inNode = offset - piece.at + piece.from;
	if (end && inNode === 0 && low > 0) {
		const before = pieces[low - 1];
		if (before) return [before.node, before.node.length];
	}
	return [piece.node, Math.min(inNode, piece.node.length)];
}

const COLORS: Record<
	Theme,
	{ background: string; text: string; link: string }
> = {
	light: { background: "#ffffff", text: "#111827", link: "#0369a1" },
	sepia: { background: "#f4ecd8", text: "#3b2f1e", link: "#7c4a03" },
	dark: { background: "#0f172a", text: "#e2e8f0", link: "#7dd3fc" },
};

/** The style sheet put over a book's own: the colours of the theme and the size of the text. */
export function lookCss(look: Look): string {
	const c = COLORS[look.theme];
	return `
html {
	color-scheme: ${look.theme === "dark" ? "dark" : "light"};
	background: ${c.background} !important;
	color: ${c.text} !important;
	font-size: ${look.fontSize}% !important;
}
body { background: transparent !important; color: inherit !important; }
p, li, blockquote, dd { line-height: 1.5; }
a:link, a:visited { color: ${c.link} !important; }
pre { white-space: pre-wrap !important; }
`;
}

function flatten(
	items: FoliateTocItem[] | undefined,
	depth = 0,
	into: TocEntry[] = [],
): TocEntry[] {
	for (const item of items ?? []) {
		into.push({
			key: `${into.length}`,
			label: item.label.trim(),
			href: item.href,
			depth,
		});
		flatten(item.subitems, depth + 1, into);
	}
	return into;
}

/**
 * Opens an EPUB, MOBI or AZW3 file in the element, at the CFI start or at
 * its beginning. Keys pressed inside the book's pages, which do not reach
 * the page around them, are handed to onKey.
 */
export async function openEbook(
	into: HTMLElement,
	file: File,
	options: {
		/** A CFI, or "fraction:<0 to 1>" for how far into the book. */
		start?: string;
		look: Look;
		onPlace: (place: Place) => void;
		onKey: (event: KeyboardEvent) => void;
	},
): Promise<Ebook> {
	const view = document.createElement("foliate-view") as View;
	view.style.display = "block";
	view.style.height = "100%";
	into.replaceChildren(view);
	view.addEventListener("load", (e) => {
		const { doc } = (e as CustomEvent<{ doc: Document }>).detail;
		doc.addEventListener("keydown", options.onKey);
	});
	view.addEventListener("relocate", (e) => {
		const at = (e as CustomEvent<View["lastLocation"]>).detail;
		if (!at) return;
		options.onPlace({
			cfi: at.cfi,
			fraction: at.fraction,
			chapter: at.tocItem?.label?.trim() || undefined,
			atEnd: Boolean(view.renderer.atEnd),
		});
	});
	await view.open(file);
	const setLook = (look: Look) => {
		view.renderer.setAttribute("flow", look.flow);
		view.renderer.setStyles?.(lookCss(look));
	};
	setLook(options.look);
	const fraction = options.start?.startsWith("fraction:")
		? Number(options.start.slice("fraction:".length))
		: undefined;
	try {
		if (fraction !== undefined) {
			await view.init({});
			await view.goToFraction(Math.min(Math.max(fraction, 0), 1));
		} else {
			await view.init(options.start ? { lastLocation: options.start } : {});
		}
	} catch {
		// A place saved in another file, or by a version of the book that
		// has since changed, is no place in this one.
		await view.init({});
	}
	// A jump made while a page is still turning is undone when the turn
	// settles, so every move waits for the one before.
	let moving: Promise<unknown> = Promise.resolve();
	let seen: Range | undefined;
	view.addEventListener("relocate", (e) => {
		seen = (e as CustomEvent<View["lastLocation"]>).detail?.range;
	});
	let pieces: Piece[] = [];
	const marking = () => view.renderer.getContents()[0];
	const speech: Source<Spoken> = {
		sentences() {
			const doc = marking()?.doc;
			if (!doc?.body) return [];
			const read = textFrom(doc, seen);
			pieces = read.pieces;
			return sentenceSpans(read.text, view.language?.canonical).map((span) => ({
				...span,
				text: read.text.slice(span.start, span.end).replace(/\s+/g, " "),
			}));
		},
		async mark(sentence) {
			const now = marking();
			now?.overlayer?.remove(SPOKEN_KEY);
			if (!sentence || !now?.doc) return;
			const from = pointAt(pieces, sentence.start, false);
			const to = pointAt(pieces, sentence.end, true);
			if (!from || !to) return;
			const range = now.doc.createRange();
			range.setStart(...from);
			range.setEnd(...to);
			await view.renderer.scrollToAnchor(range);
			marking()?.overlayer?.add(SPOKEN_KEY, range, Overlayer.highlight, {
				color: "#facc15",
			});
		},
		async nextSection() {
			const at = view.lastLocation?.section;
			if (!at || at.current + 1 >= at.total) return false;
			await view.renderer.nextSection();
			return true;
		},
	};
	const inTurn =
		<A extends unknown[], R>(move: (...args: A) => Promise<R>) =>
		(...args: A): Promise<R> => {
			const next = moving.then(() => move(...args));
			moving = next.catch(() => {});
			return next;
		};
	const search = async (query: string, limit: number) => {
		const found: Found[] = [];
		for await (const result of view.search({ query })) {
			if (typeof result === "object" && "subitems" in result) {
				found.push(...result.subitems);
				if (found.length >= limit) break;
			}
		}
		return found.slice(0, limit);
	};
	const find = async (word: string, passage: string) => {
		const found: Found[] = [];
		for await (const result of view.search({ query: word })) {
			if (typeof result === "object" && "subitems" in result) {
				found.push(...result.subitems);
			}
		}
		const best = closest(found, passage);
		if (best) await view.goTo(best.cfi);
		return Boolean(best);
	};
	return {
		toc: flatten(view.book.toc),
		language: view.language?.canonical || undefined,
		speech: {
			sentences: speech.sentences,
			mark: inTurn(speech.mark),
			nextSection: inTurn(speech.nextSection),
		},
		goTo: inTurn((target: string) => view.goTo(target)),
		find: inTurn(find),
		search: inTurn(search),
		clearSearch: () => view.clearSearch(),
		goToFraction: inTurn((fraction: number) => view.goToFraction(fraction)),
		goLeft: inTurn(() => view.goLeft()),
		goRight: inTurn(() => view.goRight()),
		next: inTurn(() => view.next()),
		prev: inTurn(() => view.prev()),
		setLook,
		close: () => {
			view.close();
			view.remove();
		},
	};
}
