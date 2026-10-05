/** A place the reader's own search found a word at, with the text around it. */
export interface Found {
	cfi: string;
	excerpt: { pre: string; match: string; post: string };
}

function words(text: string): string[] {
	return text.toLocaleLowerCase().match(/[\p{L}\p{N}]+/gu) ?? [];
}

function overlap(text: string, wanted: Set<string>): number {
	return new Set(words(text).filter((w) => wanted.has(w))).size;
}

/**
 * The page that holds the word and shares the most words with the passage,
 * the first of those on a tie; none when no page holds the word.
 */
export function closestPage(
	pages: { page: number; text: string }[],
	word: string,
	passage: string,
): number | undefined {
	const wanted = new Set(words(passage));
	const needle = word.toLocaleLowerCase();
	let best: number | undefined;
	let bestScore = -1;
	for (const { page, text } of pages) {
		if (!text.toLocaleLowerCase().includes(needle)) continue;
		const score = overlap(text, wanted);
		if (score > bestScore) {
			best = page;
			bestScore = score;
		}
	}
	return best;
}

/**
 * The place whose surrounding text shares the most words with the passage
 * the server found, the first of those on a tie. The server's text and the
 * book's pages are not character for character the same, so the places are
 * compared by their words rather than by offsets.
 */
export function closest(found: Found[], passage: string): Found | undefined {
	const wanted = new Set(words(passage));
	let best: Found | undefined;
	let bestScore = -1;
	for (const f of found) {
		const { pre, match, post } = f.excerpt;
		const score = overlap(`${pre} ${match} ${post}`, wanted);
		if (score > bestScore) {
			best = f;
			bestScore = score;
		}
	}
	return best;
}
