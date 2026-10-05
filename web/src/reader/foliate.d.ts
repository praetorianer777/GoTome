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
		section?: { current: number; total: number };
	}

	export interface FoliateSearchResult {
		cfi: string;
		excerpt: { pre: string; match: string; post: string };
	}

	export interface FoliateRenderer extends HTMLElement {
		setStyles?(css: string): void;
		atEnd?: boolean;
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
		goLeft(): Promise<void>;
		goRight(): Promise<void>;
		prev(): Promise<void>;
		next(): Promise<void>;
		close(): void;
	}
}
