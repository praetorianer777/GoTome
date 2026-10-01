import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { type CurrentUser, can } from "@/auth/session";
import {
	type BookSort,
	type BookSummary,
	booksQuery,
	facetsQuery,
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
import { type MessageKey, t } from "@/i18n";
import { formatDate } from "@/lib/format";
import { libraryIsBusy, librariesQuery } from "@/libraries/api";
import { ScanStatus } from "@/libraries/scan";

export type LibraryView = "grid" | "list";

/** What the library page shows, as the address carries it. */
export interface LibrarySearch extends FilterPicks {
	library?: string;
	sort?: BookSort;
	view?: LibraryView;
}

const SORTS: { value: BookSort; label: MessageKey }[] = [
	{ value: "title", label: "library.sort.title" },
	{ value: "author", label: "library.sort.author" },
	{ value: "added", label: "library.sort.added" },
];

const VIEWS: { value: LibraryView; label: MessageKey }[] = [
	{ value: "grid", label: "library.view.grid" },
	{ value: "list", label: "library.view.list" },
];

const BOOKS_POLL_MS = 3000;

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

export function Library({
	user,
	search,
	onSearch,
}: {
	user: CurrentUser;
	search: LibrarySearch;
	onSearch: (change: Partial<LibrarySearch>) => void;
}) {
	const libraries = useQuery(librariesQuery);
	const list = libraries.data ?? [];
	// With one library there is nothing to choose between.
	const selected =
		list.find((l) => l.id === search.library) ??
		(list.length === 1 ? list[0] : undefined);
	const sort = search.sort ?? "title";
	const view = search.view ?? "grid";
	const busy = (selected ? [selected] : list).some(libraryIsBusy);
	const picks: FilterPicks = Object.fromEntries(
		FACET_FIELDS.map((field) => [field, search[field]]),
	);
	const filter = filterTree(picks);
	const filtered = pickCount(picks);
	const [filtersOpen, setFiltersOpen] = useState(false);

	const books = useInfiniteQuery({
		...booksQuery(
			{
				library: selected?.id,
				filter,
				sort,
				order: sort === "added" ? "desc" : "asc",
			},
			busy ? BOOKS_POLL_MS : false,
		),
		enabled: list.length > 0,
	});
	const facets = useQuery({
		...facetsQuery(
			{ library: selected?.id, filter },
			busy ? BOOKS_POLL_MS : false,
		),
		enabled: list.length > 0,
	});
	const shown = books.data?.pages.flatMap((page) => page.books) ?? [];
	// Every field is named, so that one taken out of the picks leaves the
	// address too.
	const setPicks = (next: FilterPicks) =>
		onSearch(
			Object.fromEntries(FACET_FIELDS.map((field) => [field, next[field]])),
		);

	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("library.title")}</h1>
			{libraries.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={libraries.error} />

			{libraries.data?.length === 0 && (
				<div className="rounded-lg border border-dashed border-slate-300 px-6 py-16 text-center dark:border-slate-700">
					<h2 className="text-lg font-medium">{t("library.none.title")}</h2>
					<p className="mx-auto mt-2 max-w-md text-slate-600 dark:text-slate-400">
						{t(
							can(user, "storage:manage")
								? "library.none.admin"
								: "library.none.other",
						)}
					</p>
					{can(user, "storage:manage") && (
						<Link
							to="/admin/libraries"
							className="mt-4 inline-block rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800"
						>
							{t("library.none.add")}
						</Link>
					)}
				</div>
			)}

			{list.length > 0 && (
				<>
					<div className="flex flex-wrap items-end gap-x-6 gap-y-3 text-sm">
						{list.length > 1 && (
							<label className="flex items-center gap-2">
								<span>{t("library.choose")}</span>
								<select
									value={selected?.id ?? ""}
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
						<label className="flex items-center gap-2">
							<span>{t("library.sort")}</span>
							<select
								value={sort}
								onChange={(e) => onSearch({ sort: e.target.value as BookSort })}
								className={control}
							>
								{SORTS.map((s) => (
									<option key={s.value} value={s.value}>
										{t(s.label)}
									</option>
								))}
							</select>
						</label>
						<fieldset className="flex items-center gap-2">
							<legend className="sr-only">{t("library.view")}</legend>
							{VIEWS.map((v) => (
								<button
									key={v.value}
									type="button"
									aria-pressed={view === v.value}
									onClick={() => onSearch({ view: v.value })}
									className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 aria-pressed:bg-slate-900 aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:bg-slate-100 dark:aria-pressed:text-slate-900"
								>
									{t(v.label)}
								</button>
							))}
						</fieldset>
						<button
							type="button"
							aria-expanded={filtersOpen}
							aria-controls="filters"
							onClick={() => setFiltersOpen(!filtersOpen)}
							className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 lg:hidden dark:border-slate-600 dark:hover:bg-slate-800"
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
						<div className="flex min-w-0 flex-1 flex-col gap-6">
							{selected && (
								<section
									aria-labelledby="library-name"
									className="flex flex-col gap-2"
								>
									<h2 id="library-name" className="text-lg font-medium">
										{selected.name}
									</h2>
									<ScanStatus
										library={selected}
										canScan={can(user, "index:rebuild")}
									/>
								</section>
							)}

							<section
								aria-label={t("library.books")}
								className="flex flex-col gap-4"
							>
								<FormError error={books.error} />
								{books.isSuccess && shown.length === 0 && (
									<div className="rounded-lg border border-dashed border-slate-300 px-6 py-10 text-center dark:border-slate-700">
										{filtered > 0 ? (
											<>
												<h3 className="font-medium">
													{t("library.filtered.title")}
												</h3>
												<p className="mx-auto mt-2 max-w-md text-slate-600 dark:text-slate-400">
													{t("library.filtered.body")}
												</p>
												<button
													type="button"
													onClick={() => setPicks({})}
													className="mt-4 rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
												>
													{t("filter.clear")}
												</button>
											</>
										) : busy ? (
											<p className="mx-auto max-w-md text-slate-600 dark:text-slate-400">
												{t("library.adding")}
											</p>
										) : (
											<>
												<h3 className="font-medium">
													{t("library.empty.title")}
												</h3>
												<p className="mx-auto mt-2 max-w-md text-slate-600 dark:text-slate-400">
													{t("library.empty.body")}
												</p>
											</>
										)}
									</div>
								)}
								{shown.length > 0 &&
									(view === "grid" ? (
										<Grid books={shown} />
									) : (
										<Table books={shown} />
									))}
								{books.hasNextPage && (
									<MoreBooks
										loading={books.isFetchingNextPage}
										onMore={() => {
											if (!books.isFetchingNextPage) {
												books.fetchNextPage();
											}
										}}
									/>
								)}
							</section>
						</div>
					</div>
				</>
			)}
		</div>
	);
}

function Grid({ books }: { books: BookSummary[] }) {
	return (
		<ul className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
			{books.map((book) => (
				<li key={book.id}>
					<Link
						to="/books/$bookId"
						params={{ bookId: book.id }}
						className="group flex flex-col gap-2 rounded-md focus-visible:outline-2 focus-visible:outline-offset-4"
					>
						<Cover
							book={book}
							size="small"
							className="transition group-hover:opacity-90"
						/>
						<span className="flex flex-col">
							<span className="line-clamp-2 font-medium leading-snug">
								{book.title}
							</span>
							{book.authors.length > 0 && (
								<span className="line-clamp-1 text-sm text-slate-600 dark:text-slate-400">
									{book.authors.join(", ")}
								</span>
							)}
						</span>
					</Link>
				</li>
			))}
		</ul>
	);
}

function Table({ books }: { books: BookSummary[] }) {
	const head =
		"px-3 py-2 text-left font-medium text-slate-600 dark:text-slate-400";
	return (
		<div className="overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-800">
			<table className="w-full text-sm">
				<thead className="bg-slate-50 dark:bg-slate-900">
					<tr>
						<th scope="col" className={head}>
							{t("library.column.title")}
						</th>
						<th scope="col" className={head}>
							{t("library.column.authors")}
						</th>
						<th scope="col" className={`${head} hidden sm:table-cell`}>
							{t("library.column.formats")}
						</th>
						<th scope="col" className={`${head} hidden md:table-cell`}>
							{t("library.column.added")}
						</th>
					</tr>
				</thead>
				<tbody className="divide-y divide-slate-200 dark:divide-slate-800">
					{books.map((book) => (
						<tr key={book.id}>
							<td className="px-3 py-2">
								<Link
									to="/books/$bookId"
									params={{ bookId: book.id }}
									className="font-medium hover:underline"
								>
									{book.title}
								</Link>
							</td>
							<td className="px-3 py-2">{book.authors.join(", ")}</td>
							<td className="hidden px-3 py-2 uppercase sm:table-cell">
								{book.formats.join(", ")}
							</td>
							<td className="hidden px-3 py-2 md:table-cell">
								{formatDate(book.addedAt)}
							</td>
						</tr>
					))}
				</tbody>
			</table>
		</div>
	);
}

/**
 * The next page comes when the end of the list scrolls into view. The button
 * is for whoever does not scroll: a keyboard, or a browser without the
 * observer.
 */
function MoreBooks({
	loading,
	onMore,
}: {
	loading: boolean;
	onMore: () => void;
}) {
	const button = useRef<HTMLButtonElement>(null);
	const more = useRef(onMore);
	more.current = onMore;
	useEffect(() => {
		const target = button.current;
		if (!target || typeof IntersectionObserver === "undefined") {
			return;
		}
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((e) => e.isIntersecting)) {
					more.current();
				}
			},
			{ rootMargin: "600px" },
		);
		observer.observe(target);
		return () => observer.disconnect();
	}, []);
	return (
		<button
			ref={button}
			type="button"
			disabled={loading}
			onClick={onMore}
			className="self-center rounded-md border border-slate-300 px-4 py-2 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
		>
			{loading ? t("loading") : t("library.more")}
		</button>
	);
}
