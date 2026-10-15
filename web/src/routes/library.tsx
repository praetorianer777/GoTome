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
import { BulkBar } from "@/books/bulk-bar";
import { STATUS_LABELS } from "@/books/reading";
import { Cover } from "@/books/cover";
import { FilterPanel } from "@/books/filter-panel";
import { useColumns, useWindowRows, VIRTUAL_FROM } from "@/books/virtual";
import {
	FACET_FIELDS,
	type FilterPicks,
	filterTree,
	pickCount,
} from "@/books/filters";
import { FormError } from "@/components/form";
import { button, control, EmptyState, PageHeader, Skeleton } from "@/components/ui";
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
	const canSelect = can(user, "personal:manage");
	const [selecting, setSelecting] = useState(false);
	const [chosen, setChosen] = useState<ReadonlySet<string>>(new Set());
	const [allMatching, setAllMatching] = useState(false);
	// A selection belongs to the list it was made in.
	const listKey = `${selected?.id ?? ""} ${filter ?? ""}`;
	const [chosenIn, setChosenIn] = useState(listKey);
	if (chosenIn !== listKey) {
		setChosenIn(listKey);
		setChosen(new Set());
		setAllMatching(false);
	}
	const toggle = (id: string) => {
		// Unticking one of every book that matches leaves those shown.
		const next = new Set(allMatching ? shown.map((b) => b.id) : chosen);
		if (!next.delete(id)) {
			next.add(id);
		}
		setChosen(next);
		setAllMatching(false);
	};
	const clearSelection = () => {
		setChosen(new Set());
		setAllMatching(false);
	};
	const selection = selecting
		? { isSelected: (id: string) => allMatching || chosen.has(id), toggle }
		: undefined;
	// Every field is named, so that one taken out of the picks leaves the
	// address too.
	const setPicks = (next: FilterPicks) =>
		onSearch(
			Object.fromEntries(FACET_FIELDS.map((field) => [field, next[field]])),
		);

	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t("library.title")} />
			{libraries.isPending && <GridSkeleton />}
			<FormError error={libraries.error} />

			{libraries.data?.length === 0 && (
				<EmptyState
					title={t("library.none.title")}
					action={
						can(user, "storage:manage") && (
							<Link to="/admin/libraries" className={button("primary", "md")}>
								{t("library.none.add")}
							</Link>
						)
					}
				>
					{t(
						can(user, "storage:manage")
							? "library.none.admin"
							: "library.none.other",
					)}
				</EmptyState>
			)}

			{list.length > 0 && (
				<>
					<div className="flex flex-wrap items-center gap-x-4 gap-y-3 border-b border-slate-200 pb-4 text-sm dark:border-slate-800">
						{list.length > 1 && (
							<label className="flex items-center gap-2">
								<span className="text-slate-600 dark:text-slate-400">{t("library.choose")}</span>
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
							<span className="text-slate-600 dark:text-slate-400">{t("library.sort")}</span>
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
						<fieldset className="flex items-center">
							<legend className="sr-only">{t("library.view")}</legend>
							{VIEWS.map((v) => (
								<button
									key={v.value}
									type="button"
									aria-pressed={view === v.value}
									onClick={() => onSearch({ view: v.value })}
									className={`${button("toggle")} first:rounded-r-none last:-ml-px last:rounded-l-none`}
								>
									{t(v.label)}
								</button>
							))}
						</fieldset>
						{canSelect && (
							<button
								type="button"
								aria-pressed={selecting}
								onClick={() => {
									setSelecting(!selecting);
									clearSelection();
								}}
								className={`${button("toggle")} sm:ml-auto`}
							>
								{t(selecting ? "bulk.selectDone" : "bulk.select")}
							</button>
						)}
						<button
							type="button"
							aria-expanded={filtersOpen}
							aria-controls="filters"
							onClick={() => setFiltersOpen(!filtersOpen)}
							className={`${button()} lg:hidden`}
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
							className={`${filtersOpen ? "" : "hidden"} shrink-0 lg:sticky lg:top-20 lg:block lg:max-h-[calc(100dvh-6rem)] lg:w-56 lg:overflow-y-auto`}
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
								{selecting && (
									<BulkBar
										count={chosen.size}
										canEdit={can(user, "metadata:edit")}
										all={allMatching}
										selection={
											allMatching
												? { library: selected?.id, filter }
												: { books: [...chosen] }
										}
										onSelectAll={() => setAllMatching(true)}
										onClear={clearSelection}
									/>
								)}
								<FormError error={books.error} />
								{books.isPending && <GridSkeleton />}
								{books.isSuccess &&
									shown.length === 0 &&
									(filtered > 0 ? (
										<EmptyState
											title={t("library.filtered.title")}
											action={
												<button
													type="button"
													onClick={() => setPicks({})}
													className={button()}
												>
													{t("filter.clear")}
												</button>
											}
										>
											{t("library.filtered.body")}
										</EmptyState>
									) : busy ? (
										<EmptyState>{t("library.adding")}</EmptyState>
									) : (
										<EmptyState title={t("library.empty.title")}>
											{t("library.empty.body")}
										</EmptyState>
									))}
								{shown.length > 0 &&
									(view === "grid" ? (
										<Grid books={shown} selection={selection} />
									) : (
										<Table books={shown} selection={selection} />
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

function Stars({ rating }: { rating: number }) {
	return (
		<span
			role="img"
			aria-label={t("reading.stars", { count: rating })}
			className="tracking-tight text-candle-strong dark:text-candle"
		>
			{"★".repeat(rating)}
		</span>
	);
}

/** Where the viewer stands with a book, when it is more than unread. */
function Standing({ book }: { book: BookSummary }) {
	if (book.status === "unread" && !book.rating) {
		return null;
	}
	return (
		<span className="flex gap-2 text-sm text-slate-600 dark:text-slate-400">
			{book.status !== "unread" && (
				<span
					className={
						book.status === "reading"
							? "font-medium text-candle-strong dark:text-candle"
							: undefined
					}
				>
					{t(STATUS_LABELS[book.status])}
				</span>
			)}
			{book.rating ? <Stars rating={book.rating} /> : null}
		</span>
	);
}

/** Marks a book none of whose files is in the folder any more. */
function FilesMissing({ book }: { book: BookSummary }) {
	if (!book.filesMissing) {
		return null;
	}
	return (
		<span
			title={t("library.filesMissing.hint")}
			className="self-start rounded bg-amber-100 px-1.5 text-xs font-medium text-amber-900 dark:bg-amber-900/40 dark:text-amber-200"
		>
			{t("library.filesMissing")}
		</span>
	);
}

/** Which books are ticked, while books are being selected. */
interface Selection {
	isSelected: (id: string) => boolean;
	toggle: (id: string) => void;
}

function Tick({ book, selection }: { book: BookSummary; selection: Selection }) {
	return (
		<input
			type="checkbox"
			checked={selection.isSelected(book.id)}
			onChange={() => selection.toggle(book.id)}
			aria-label={t("bulk.selectBook", { title: book.title })}
			className="size-5"
		/>
	);
}

function Grid({ books, selection }: { books: BookSummary[]; selection?: Selection }) {
	if (books.length >= VIRTUAL_FROM) {
		return <VirtualGrid books={books} selection={selection} />;
	}
	return (
		<ul className={GRID}>
			{books.map((book) => (
				<li key={book.id} className="relative">
					<GridCard book={book} selection={selection} />
				</li>
			))}
		</ul>
	);
}

const GRID =
	"grid grid-cols-2 gap-x-5 gap-y-7 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-3 xl:grid-cols-5";

/** Covers standing for the books while the first page loads. */
function GridSkeleton() {
	return (
		<div className={GRID}>
			{Array.from({ length: 10 }, (_, i) => (
				<div key={i} className="flex flex-col gap-2">
					<Skeleton className="aspect-[2/3] w-full" />
					<Skeleton className="h-4 w-4/5" />
					<Skeleton className="h-3 w-1/2" />
				</div>
			))}
		</div>
	);
}

/** A card's height with its cover and three lines, before it is measured. */
const CARD_ROW_ESTIMATE = 360;

/**
 * The grid of a long list, a row of cards at a time: only the rows near the
 * window are in the page. Each says where it stands among them all.
 */
function VirtualGrid({ books, selection }: { books: BookSummary[]; selection?: Selection }) {
	const columns = useColumns();
	const list = useRef<HTMLDivElement>(null);
	const rows = useWindowRows(Math.ceil(books.length / columns), CARD_ROW_ESTIMATE, list, `grid-${columns}`);
	return (
		<div
			ref={list}
			role="list"
			className="relative"
			style={{ height: rows.getTotalSize() }}
		>
			{rows.getVirtualItems().map((row) => (
				<div
					key={row.key}
					data-index={row.index}
					ref={rows.measureElement}
					role="presentation"
					className={`absolute inset-x-0 top-0 pb-6 ${GRID}`}
					style={{ transform: `translateY(${row.start - rows.options.scrollMargin}px)` }}
				>
					{books.slice(row.index * columns, (row.index + 1) * columns).map((book, i) => (
						<div
							key={book.id}
							role="listitem"
							aria-posinset={row.index * columns + i + 1}
							aria-setsize={books.length}
							className="relative"
						>
							<GridCard book={book} selection={selection} />
						</div>
					))}
				</div>
			))}
		</div>
	);
}

function GridCard({ book, selection }: { book: BookSummary; selection?: Selection }) {
	return (
		<>
			{selection && (
				<span className="absolute top-2 left-2 z-10 flex rounded bg-white/90 p-1 shadow-sm dark:bg-slate-900/90">
					<Tick book={book} selection={selection} />
				</span>
			)}
			<Link
				to="/books/$bookId"
				params={{ bookId: book.id }}
				className="group flex flex-col gap-2.5 rounded-md focus-visible:outline-2 focus-visible:outline-offset-4"
			>
				<Cover
					book={book}
					size="small"
					className={`transition duration-200 group-hover:-translate-y-1 motion-reduce:transition-none motion-reduce:group-hover:translate-y-0 ${book.status === "reading" ? "shadow-candle" : ""}`}
				/>
				<span className="flex flex-col gap-0.5">
					<span className="line-clamp-2 font-medium leading-snug group-hover:text-brand-strong dark:group-hover:text-brand">
						{book.title}
					</span>
					{book.authors.length > 0 && (
						<span className="line-clamp-1 text-sm text-slate-600 dark:text-slate-400">
							{book.authors.join(", ")}
						</span>
					)}
					<Standing book={book} />
					<FilesMissing book={book} />
				</span>
			</Link>
		</>
	);
}

/** A row of the table before it is measured. */
const TABLE_ROW_ESTIMATE = 41;

function Table({ books, selection }: { books: BookSummary[]; selection?: Selection }) {
	const head =
		"px-3 py-2 text-left font-medium text-slate-600 dark:text-slate-400";
	const columns = selection ? 7 : 6;
	return (
		<div className="overflow-x-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-950">
			<table className="w-full text-sm" aria-rowcount={books.length + 1}>
				<thead className="border-b border-slate-200 bg-slate-100/60 text-xs tracking-wide uppercase dark:border-slate-800 dark:bg-slate-900">
					<tr aria-rowindex={1}>
						{selection && (
							<th scope="col" className={head}>
								<span className="sr-only">{t("bulk.title")}</span>
							</th>
						)}
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
						<th scope="col" className={`${head} hidden lg:table-cell`}>
							{t("library.column.status")}
						</th>
						<th scope="col" className={`${head} hidden lg:table-cell`}>
							{t("library.column.rating")}
						</th>
					</tr>
				</thead>
				{books.length >= VIRTUAL_FROM ? (
					<VirtualRows books={books} selection={selection} columns={columns} />
				) : (
					<tbody className="divide-y divide-slate-200 dark:divide-slate-800">
						{books.map((book, i) => (
							<BookRow key={book.id} book={book} selection={selection} index={i} />
						))}
					</tbody>
				)}
			</table>
		</div>
	);
}

/**
 * The rows of a long table near the window, between two empty rows that
 * stand for the rest, so that the table keeps its columns and its height.
 */
function VirtualRows({
	books,
	selection,
	columns,
}: {
	books: BookSummary[];
	selection?: Selection;
	columns: number;
}) {
	const body = useRef<HTMLTableSectionElement>(null);
	const rows = useWindowRows(books.length, TABLE_ROW_ESTIMATE, body, "table");
	const items = rows.getVirtualItems();
	const first = items[0];
	const last = items[items.length - 1];
	const before = first ? first.start - rows.options.scrollMargin : 0;
	const after = last ? rows.getTotalSize() - last.end + rows.options.scrollMargin : 0;
	return (
		<tbody ref={body} className="divide-y divide-slate-200 dark:divide-slate-800">
			{before > 0 && (
				<tr aria-hidden="true" style={{ height: before }}>
					<td colSpan={columns} />
				</tr>
			)}
			{items.map((item) => {
				const book = books[item.index];
				return (
					book && (
						<BookRow
							key={book.id}
							book={book}
							selection={selection}
							index={item.index}
							measure={rows.measureElement}
						/>
					)
				);
			})}
			{after > 0 && (
				<tr aria-hidden="true" style={{ height: after }}>
					<td colSpan={columns} />
				</tr>
			)}
		</tbody>
	);
}

function BookRow({
	book,
	selection,
	index,
	measure,
}: {
	book: BookSummary;
	selection?: Selection;
	index: number;
	measure?: (element: HTMLTableRowElement | null) => void;
}) {
	return (
		<tr
			ref={measure}
			data-index={index}
			aria-rowindex={index + 2}
			className="hover:bg-slate-50 dark:hover:bg-slate-900/60"
		>
			{selection && (
				<td className="w-8 px-3 py-2">
					<Tick book={book} selection={selection} />
				</td>
			)}
			<td className="px-3 py-2">
				<Link
					to="/books/$bookId"
					params={{ bookId: book.id }}
					className="font-medium hover:text-brand-strong hover:underline dark:hover:text-brand"
				>
					{book.title}
				</Link>
				{book.filesMissing && (
					<span className="ml-2 inline-flex align-middle">
						<FilesMissing book={book} />
					</span>
				)}
			</td>
			<td className="px-3 py-2">{book.authors.join(", ")}</td>
			<td className="hidden px-3 py-2 text-xs tracking-wide text-slate-600 uppercase sm:table-cell dark:text-slate-400">
				{book.formats.join(", ")}
			</td>
			<td className="hidden px-3 py-2 md:table-cell">
				{formatDate(book.addedAt)}
			</td>
			<td className="hidden px-3 py-2 lg:table-cell">
				{t(STATUS_LABELS[book.status])}
			</td>
			<td className="hidden px-3 py-2 lg:table-cell">
				{book.rating ? <Stars rating={book.rating} /> : null}
			</td>
		</tr>
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
	const end = useRef<HTMLButtonElement>(null);
	const more = useRef(onMore);
	more.current = onMore;
	// Whether the end is near, not only when it comes near: a page that
	// leaves it in view, as one too short to fill the window does, asks for
	// the next one too. The observer says only when that changes.
	const [near, setNear] = useState(false);
	useEffect(() => {
		const target = end.current;
		if (!target || typeof IntersectionObserver === "undefined") {
			return;
		}
		const observer = new IntersectionObserver(
			(entries) => setNear(entries.some((e) => e.isIntersecting)),
			{ rootMargin: "600px" },
		);
		observer.observe(target);
		return () => observer.disconnect();
	}, []);
	useEffect(() => {
		if (near && !loading) {
			more.current();
		}
	}, [near, loading]);
	return (
		<button
			ref={end}
			type="button"
			disabled={loading}
			onClick={onMore}
			className={`${button("secondary", "md")} self-center`}
		>
			{loading ? t("loading") : t("library.more")}
		</button>
	);
}
