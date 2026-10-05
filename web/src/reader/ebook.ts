import "foliate-js/view.js";
import type { FoliateTocItem, View } from "foliate-js/view.js";
import { closest, type Found } from "@/reader/passage";

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

export interface Ebook {
	toc: TocEntry[];
	/** Goes to a CFI or a table of contents entry's href. */
	goTo(target: string): Promise<void>;
	/**
	 * Looks for the word in the book, outlines where it stands, and goes to
	 * the place whose text is most like the passage. False when the word is
	 * nowhere in the book.
	 */
	find(word: string, passage: string): Promise<boolean>;
	/** Turns the page towards the left or the right, whichever way the book reads. */
	goLeft(): Promise<void>;
	goRight(): Promise<void>;
	/** Turns to the next or the previous page in reading order. */
	next(): Promise<void>;
	prev(): Promise<void>;
	setLook(look: Look): void;
	close(): void;
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
	const inTurn =
		<A extends unknown[], R>(move: (...args: A) => Promise<R>) =>
		(...args: A): Promise<R> => {
			const next = moving.then(() => move(...args));
			moving = next.catch(() => {});
			return next;
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
		goTo: inTurn((target: string) => view.goTo(target)),
		find: inTurn(find),
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
