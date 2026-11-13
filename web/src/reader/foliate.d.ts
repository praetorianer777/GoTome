// The part of foliate-js's view the reader uses. foliate-js has no types of
// its own and no stable API; this is what web/vendor/foliate-js/COMMIT has.
declare module "foliate-js/view.js" {
	export interface FoliateTocItem {
		label: string;
		href: string;
		subitems?: FoliateTocItem[];
	}

	export interface FoliateLocation {
		fraction: number;
		cfi: string;
		tocItem?: { label?: string; href?: string };
		location?: { current: number; next: number; total: number };
		/** The visible part of the section, from its start. */
		range?: Range;
		section?: { current: number; total: number };
	}

	export interface FoliateSearchResult {
		cfi: string;
		excerpt: { pre: string; match: string; post: string };
	}

	export interface FoliateOverlayer {
		add(key: string, range: Range, draw: unknown, options?: object): void;
		remove(key: string): void;
	}

	export interface FoliateRenderer extends HTMLElement {
		setStyles?(css: string): void;
		atEnd?: boolean;
		scrollToAnchor(anchor: Range): Promise<void>;
		nextSection(): Promise<void>;
		getContents(): { index: number; doc?: Document; overlayer?: FoliateOverlayer }[];
		next(): Promise<void>;
		prev(): Promise<void>;
	}

	export interface FoliateBook {
		dir?: string;
		toc?: FoliateTocItem[];
		sections: unknown[];
	}

	export class View extends HTMLElement {
		book: FoliateBook;
		renderer: FoliateRenderer;
		lastLocation: FoliateLocation | null;
		language?: { canonical?: string };
		open(book: File | Blob): Promise<void>;
		init(options: {
			lastLocation?: string;
			showTextStart?: boolean;
		}): Promise<void>;
		goTo(target: string | number): Promise<void>;
		/** Yields progress, then each section's places, then "done". */
		search(options: {
			query: string;
			index?: number;
		}): AsyncGenerator<
			| "done"
			| { progress: number }
			| { label: string; subitems: FoliateSearchResult[] }
		>;
		goToFraction(fraction: number): Promise<void>;
		/** The CFI of a range in the section at the index. */
		getCFI(index: number, range?: Range): string;
		/** Draws the annotation at its CFI, once "draw-annotation" is answered. */
		addAnnotation(annotation: { value: string }, remove?: boolean): Promise<unknown>;
		deleteAnnotation(annotation: { value: string }): Promise<unknown>;
		clearSearch(): void;
		goLeft(): Promise<void>;
		goRight(): Promise<void>;
		prev(): Promise<void>;
		next(): Promise<void>;
		close(): void;
	}
}

declare module "foliate-js/overlayer.js" {
	export class Overlayer {
		static highlight(rects: unknown, options?: { color?: string }): SVGElement;
	}
}
