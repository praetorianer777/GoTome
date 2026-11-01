import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import type { BookSummary } from "@/books/api";
import { Cover } from "@/books/cover";
import { STATUS_LABELS } from "@/books/reading";
import { useColumns, useWindowRows, VIRTUAL_FROM } from "@/books/virtual";
import { button, Skeleton } from "@/components/ui";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";

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

/** A book's series and its place in it, or that it stands alone. */
function SeriesLine({ book }: { book: BookSummary }) {
	if (!book.series) {
		return null;
	}
	return (
		<span className="line-clamp-1 block text-sm text-slate-600 dark:text-slate-400">
			{book.seriesIndex != null
				? t("book.seriesPosition", { index: book.seriesIndex, series: book.series })
				: book.series}
		</span>
	);
}

/** Which books are ticked, while books are being selected. */
export interface Selection {
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

export function Grid({
	books,
	selection,
	showSeries,
}: {
	books: BookSummary[];
	selection?: Selection;
	showSeries?: boolean;
}) {
	if (books.length >= VIRTUAL_FROM) {
		return <VirtualGrid books={books} selection={selection} showSeries={showSeries} />;
	}
	return (
		<ul className={GRID}>
			{books.map((book) => (
				<li key={book.id} className="relative">
					<GridCard book={book} selection={selection} showSeries={showSeries} />
				</li>
			))}
		</ul>
	);
}

const GRID =
	"grid grid-cols-2 gap-x-5 gap-y-7 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-3 xl:grid-cols-5";

/** Covers standing for the books while the first page loads. */
export function GridSkeleton() {
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
function VirtualGrid({
	books,
	selection,
	showSeries,
}: {
	books: BookSummary[];
	selection?: Selection;
	showSeries?: boolean;
}) {
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
							<GridCard book={book} selection={selection} showSeries={showSeries} />
						</div>
					))}
				</div>
			))}
		</div>
	);
}

function GridCard({
	book,
	selection,
	showSeries,
}: {
	book: BookSummary;
	selection?: Selection;
	showSeries?: boolean;
}) {
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
					{showSeries && <SeriesLine book={book} />}
					<Standing book={book} />
					<FilesMissing book={book} />
				</span>
			</Link>
		</>
	);
}

/** A row of the table before it is measured. */
const TABLE_ROW_ESTIMATE = 41;

export function Table({
	books,
	selection,
	showSeries,
}: {
	books: BookSummary[];
	selection?: Selection;
	showSeries?: boolean;
}) {
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
					<VirtualRows
						books={books}
						selection={selection}
						showSeries={showSeries}
						columns={columns}
					/>
				) : (
					<tbody className="divide-y divide-slate-200 dark:divide-slate-800">
						{books.map((book, i) => (
							<BookRow key={book.id} book={book} selection={selection} showSeries={showSeries} index={i} />
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
	showSeries,
	columns,
}: {
	books: BookSummary[];
	selection?: Selection;
	showSeries?: boolean;
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
							showSeries={showSeries}
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
	showSeries,
	index,
	measure,
}: {
	book: BookSummary;
	selection?: Selection;
	showSeries?: boolean;
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
				{showSeries && <SeriesLine book={book} />}
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
export function MoreBooks({
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
