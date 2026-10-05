import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import {
	type TextBook,
	type TextHit,
	facetsQuery,
	textSearchQuery,
} from "@/books/api";
import { Cover } from "@/books/cover";
import { FilterPanel } from "@/books/filter-panel";
import {
	FACET_FIELDS,
	type FilterPicks,
	filterTree,
	pickCount,
} from "@/books/filters";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { librariesQuery } from "@/libraries/api";
import { EBOOK_FORMATS, type ReaderSearch } from "@/routes/read";

/** What the search page shows, as the address carries it. */
export interface TextSearch extends FilterPicks {
	q?: string;
	library?: string;
}

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The text of the books searched, with the libraries and filters of the library page. */
export function SearchPage({
	search,
	onSearch,
}: {
	search: TextSearch;
	onSearch: (change: Partial<TextSearch>) => void;
}) {
	const libraries = useQuery(librariesQuery);
	const list = libraries.data ?? [];
	const library = list.find((l) => l.id === search.library)?.id;
	const picks: FilterPicks = Object.fromEntries(
		FACET_FIELDS.map((field) => [field, search[field]]),
	);
	const filter = filterTree(picks);
	const filtered = pickCount(picks);
	const [filtersOpen, setFiltersOpen] = useState(false);
	const q = search.q?.trim() ?? "";
	const [words, setWords] = useState(q);
	const [shownFor, setShownFor] = useState(q);
	// The box follows the address when it changes from elsewhere, as from
	// the header's search.
	if (shownFor !== q) {
		setShownFor(q);
		setWords(q);
	}

	const results = useInfiniteQuery({
		...textSearchQuery({ q, library, filter }),
		enabled: q !== "",
	});
	const facets = useQuery(facetsQuery({ library, filter }));
	const books = results.data?.pages.flatMap((page) => page.books) ?? [];
	const setPicks = (next: FilterPicks) =>
		onSearch(
			Object.fromEntries(FACET_FIELDS.map((field) => [field, next[field]])),
		);

	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("fulltext.title")}</h1>
			<search>
			<form
				className="flex flex-wrap items-center gap-2"
				onSubmit={(e) => {
					e.preventDefault();
					onSearch({ q: words.trim() || undefined });
				}}
			>
				<input
					type="search"
					aria-label={t("fulltext.label")}
					placeholder={t("fulltext.placeholder")}
					value={words}
					onChange={(e) => setWords(e.target.value)}
					className={`${control} min-w-0 flex-1 px-3 sm:max-w-xl`}
				/>
				<button type="submit" className={button}>
					{t("fulltext.submit")}
				</button>
			</form>
			</search>
			<div className="flex flex-wrap items-end gap-x-6 gap-y-3 text-sm">
				{list.length > 1 && (
					<label className="flex items-center gap-2">
						<span>{t("library.choose")}</span>
						<select
							value={library ?? ""}
							onChange={(e) =>
								onSearch({ library: e.target.value || undefined })
							}
							className={control}
						>
							<option value="">{t("library.all")}</option>
							{list.map((l) => (
								<option key={l.id} value={l.id}>
									{l.name}
								</option>
							))}
						</select>
					</label>
				)}
				<button
					type="button"
					aria-expanded={filtersOpen}
					aria-controls="filters"
					onClick={() => setFiltersOpen(!filtersOpen)}
					className={`${button} lg:hidden`}
				>
					{filtered > 0
						? t("filter.toggleActive", { count: filtered })
						: t("filter.toggle")}
				</button>
			</div>

			<div className="flex flex-col gap-6 lg:flex-row lg:items-start">
				<aside
					id="filters"
					aria-label={t("filter.title")}
					className={`${filtersOpen ? "" : "hidden"} shrink-0 lg:block lg:w-60`}
				>
					<FilterPanel
						facets={facets.data ?? []}
						picks={picks}
						onChange={setPicks}
					/>
				</aside>
				<section
					aria-label={t("fulltext.results")}
					className="flex min-w-0 flex-1 flex-col gap-6"
				>
					<FormError error={results.error} />
					{q === "" && (
						<p className="text-slate-600 dark:text-slate-400">
							{t("fulltext.prompt")}
						</p>
					)}
					{results.isPending && q !== "" && (
						<p className="text-slate-500">{t("loading")}</p>
					)}
					{results.isSuccess && books.length === 0 && (
						<p role="status" className="text-slate-600 dark:text-slate-400">
							{t("fulltext.none")}
						</p>
					)}
					{books.length > 0 && (
						<ol className="flex flex-col gap-6">
							{books.map((found) => (
								<Found key={found.book.id} found={found} />
							))}
						</ol>
					)}
					{results.hasNextPage && (
						<button
							type="button"
							disabled={results.isFetchingNextPage}
							onClick={() => results.fetchNextPage()}
							className={`${button} self-center px-4 py-2`}
						>
							{results.isFetchingNextPage ? t("loading") : t("fulltext.more")}
						</button>
					)}
				</section>
			</div>
		</div>
	);
}

function Found({ found }: { found: TextBook }) {
	const { book } = found;
	return (
		<li className="flex gap-4">
			<Link
				to="/books/$bookId"
				params={{ bookId: book.id }}
				tabIndex={-1}
				aria-hidden="true"
				className="w-16 shrink-0"
			>
				<Cover book={book} size="small" />
			</Link>
			<article className="flex min-w-0 flex-1 flex-col gap-2">
				<header>
					<h2 className="font-medium">
						<Link
							to="/books/$bookId"
							params={{ bookId: book.id }}
							className="hover:underline"
						>
							{book.title}
						</Link>
					</h2>
					{book.authors.length > 0 && (
						<p className="text-sm text-slate-600 dark:text-slate-400">
							{book.authors.join(", ")}
						</p>
					)}
					{found.corrected && (
						<p className="text-sm text-amber-700 dark:text-amber-400">
							{t("fulltext.corrected")}
						</p>
					)}
				</header>
				<ul className="flex flex-col gap-3">
					{found.hits.map((hit) => (
						<Passage
							key={`${hit.fileId} ${hit.position}`}
							bookId={book.id}
							hit={hit}
						/>
					))}
				</ul>
			</article>
		</li>
	);
}

function Passage({ bookId, hit }: { bookId: string; hit: TextHit }) {
	const where = [
		hit.chapter,
		hit.format === "pdf" && hit.pageFrom
			? hit.pageTo && hit.pageTo !== hit.pageFrom
				? t("fulltext.pages", { from: hit.pageFrom, to: hit.pageTo })
				: t("fulltext.page", { page: hit.pageFrom })
			: undefined,
	].filter(Boolean);
	const at = readerAt(hit);
	return (
		<li className="flex flex-col gap-1 border-l-2 border-slate-200 pl-3 dark:border-slate-700">
			<p className="text-sm leading-relaxed">
				{keyed(hit.snippet).map(({ key, text, match }) =>
					match ? (
						<mark
							key={key}
							className="rounded-sm bg-amber-200 px-0.5 text-inherit dark:bg-amber-700"
						>
							{text}
						</mark>
					) : (
						<span key={key}>{text}</span>
					),
				)}
			</p>
			<p className="flex flex-wrap gap-x-3 text-sm text-slate-600 dark:text-slate-400">
				{where.length > 0 && <span>{where.join(" · ")}</span>}
				{at && (
					<Link
						to="/books/$bookId/read/$fileId"
						params={{ bookId, fileId: hit.fileId }}
						search={at}
						className="text-brand-strong underline underline-offset-4 dark:text-brand"
					>
						{t("fulltext.open")}
					</Link>
				)}
			</p>
		</li>
	);
}

/** The parts of a snippet, each keyed by where it starts in it. */
function keyed(parts: TextHit["snippet"]) {
	let at = 0;
	return parts.map((part) => {
		const key = at;
		at += part.text.length;
		return { ...part, key };
	});
}

/**
 * Where the reader opens for a passage: at the longest of the words found,
 * looked for near the passage's text, in a PDF on the passage's pages. None
 * for a file the browser does not read.
 */
function readerAt(hit: TextHit): ReaderSearch | undefined {
	if (hit.format !== "pdf" && !EBOOK_FORMATS.includes(hit.format)) {
		return undefined;
	}
	const pages =
		hit.format === "pdf" ? { page: hit.pageFrom, to: hit.pageTo } : {};
	const word = hit.snippet
		.filter((part) => part.match)
		.map((part) => part.text)
		.reduce((a, b) => (b.length > a.length ? b : a), "");
	return word
		? { ...pages, find: word, near: hit.snippet.map((part) => part.text).join("") }
		: pages;
}
