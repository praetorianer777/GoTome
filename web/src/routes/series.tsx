import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { type CurrentUser, can } from "@/auth/session";
import {
	type BookSummary,
	booksQuery,
	type ListedBook,
	type SeriesDetail,
	seriesQuery,
} from "@/books/api";
import { Grid, GridSkeleton, MoreBooks } from "@/books/book-list";
import { Cover } from "@/books/cover";
import { candidateCoverUrl } from "@/books/edit";
import { filterTree } from "@/books/filters";
import { mediumOf } from "@/books/medium";
import { useWish } from "@/books/wishes";
import { FormError } from "@/components/form";
import { button, EmptyState, PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { bookCount } from "@/lib/format";
import { usePlayer } from "@/player/player";
import { useQueue } from "@/player/queue";
import { Follow } from "@/releases/follow";

/** A series in its order: how far the person is, and what the library lacks. */
export function SeriesPage({
	user,
	seriesKey,
}: {
	user: CurrentUser;
	seriesKey: string;
}) {
	const series = useQuery(seriesQuery(seriesKey));
	const s = series.data;
	if (series.error instanceof ApiError && series.error.status === 404) {
		return <EmptyState mood="confused" title={t("series.notFound")} />;
	}
	if (!s) {
		return (
			<>
				{series.isPending && <GridSkeleton />}
				<FormError error={series.error} />
			</>
		);
	}
	const personal = can(user, "personal:manage");
	return (
		<div className="flex flex-col gap-8">
			<PageHeader
				title={s.name}
				count={bookCount(s.books)}
				intro={
					s.read >= s.books
						? t("series.readAll")
						: t("series.read", { read: s.read, books: s.books })
				}
				actions={
					personal && <Follow subjects={[{ kind: "series", name: s.name }]} />
				}
			/>
			{s.next && <NextBook book={s.next} />}
			<Missing series={s} canWish={personal} />
			<SeriesBooks seriesKey={s.key} queueing={personal} />
		</div>
	);
}

function NextBook({ book }: { book: BookSummary }) {
	return (
		<section aria-labelledby="series-next" className="flex items-center gap-4">
			<Link
				to="/books/$bookId"
				params={{ bookId: book.id }}
				className="w-16 shrink-0"
				tabIndex={-1}
				aria-hidden="true"
			>
				<Cover book={book} size="small" />
			</Link>
			<div className="flex min-w-0 flex-col gap-1">
				<h2 id="series-next" className="text-lg font-medium">
					{t("series.next")}
				</h2>
				<Link
					to="/books/$bookId"
					params={{ bookId: book.id }}
					className="font-medium text-brand-strong hover:underline dark:text-brand"
				>
					{book.seriesIndex != null
						? t("series.placeTitle", { index: book.seriesIndex, title: book.title })
						: book.title}
				</Link>
			</div>
		</section>
	);
}

/** The places no book takes, and the books the sources know to be missing. */
function Missing({ series, canWish }: { series: SeriesDetail; canWish: boolean }) {
	if (series.missing.length === 0 && series.listed.length === 0) {
		return null;
	}
	return (
		<section aria-labelledby="series-missing" className="flex flex-col gap-3">
			<h2 id="series-missing" className="text-lg font-medium">
				{t("series.missing")}
			</h2>
			{series.missing.length > 0 && (
				<ul className="flex flex-col gap-1 text-slate-600 dark:text-slate-400">
					{series.missing.map((gap) => (
						<li key={gap.from}>
							{gap.from === gap.to
								? t("series.missing.one", { from: gap.from })
								: t("series.missing.many", { from: gap.from, to: gap.to })}
						</li>
					))}
				</ul>
			)}
			{series.listed.length > 0 && (
				<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
					{series.listed.map((book) => (
						<li key={`${book.source}-${book.title}`}>
							<Listed book={book} series={series} canWish={canWish} />
						</li>
					))}
				</ul>
			)}
		</section>
	);
}

function Listed({
	book,
	series,
	canWish,
}: {
	book: ListedBook;
	series: SeriesDetail;
	canWish: boolean;
}) {
	const wish = useWish();
	return (
		<div className="flex items-start gap-3 py-2">
			{book.coverToken ? (
				<img src={candidateCoverUrl(book.coverToken)} alt="" className="w-12 rounded" />
			) : (
				<span className="w-12" />
			)}
			<div className="min-w-0 flex-1">
				<p className="font-medium">
					{book.seriesIndex != null
						? t("series.placeTitle", { index: book.seriesIndex, title: book.title })
						: book.title}
				</p>
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{[book.authors.join(", "), book.published, book.source]
						.filter(Boolean)
						.join(" · ")}
				</p>
				<FormError error={wish.error} />
			</div>
			{book.wished ? (
				<span className="text-sm text-slate-600 dark:text-slate-400">
					{t("series.wished")}
				</span>
			) : (
				canWish &&
				book.wishable && (
					<button
						type="button"
						className={button()}
						disabled={wish.isPending}
						aria-label={t("series.wishNamed", { title: book.title })}
						onClick={() =>
							wish.mutate({
								library: series.library,
								provider: book.source,
								title: book.title,
								published: book.published,
								series: { name: series.name, index: book.seriesIndex },
								contributors: book.authors.map((name) => ({ name, role: "author" })),
								identifiers: book.isbns.map((value) => ({ type: "isbn", value })),
								coverToken: book.coverToken,
							})
						}
					>
						{t("series.wish")}
					</button>
				)
			)}
		</div>
	);
}

function SeriesBooks({
	seriesKey,
	queueing,
}: {
	seriesKey: string;
	queueing: boolean;
}) {
	const books = useInfiniteQuery(
		booksQuery({
			filter: filterTree({ series: [seriesKey] }),
			sort: "series",
			order: "asc",
		}),
	);
	const shown = books.data?.pages.flatMap((page) => page.books) ?? [];
	return (
		<section aria-label={t("library.books")} className="flex flex-col gap-4">
			<FormError error={books.error} />
			{queueing && shown.length > 0 && (
				<QueueRest load={async () => {
					let data = books.data;
					let more = books.hasNextPage;
					while (more) {
						const next = await books.fetchNextPage();
						data = next.data;
						more = next.hasNextPage;
					}
					return data?.pages.flatMap((page) => page.books) ?? [];
				}} />
			)}
			{books.isPending && <GridSkeleton />}
			{shown.length > 0 && <Grid books={shown} />}
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
	);
}

/**
 * Lines up the series' audiobooks the person has not finished, in the
 * series' order, after what is already queued.
 */
function QueueRest({ load }: { load: () => Promise<BookSummary[]> }) {
	const queue = useQueue();
	const playing = usePlayer().state.bookId;
	const [told, setTold] = useState<number>();
	const [busy, setBusy] = useState(false);
	return (
		<div className="flex flex-wrap items-center gap-3">
			<button
				type="button"
				className={button("secondary", "md")}
				disabled={busy}
				onClick={async () => {
					setBusy(true);
					try {
						const ids = (await load())
							.filter((b) => {
								const medium = mediumOf(b.formats);
								return (
									(medium === "audio" || medium === "both") &&
									!b.placeholder &&
									b.status !== "completed" &&
									b.id !== playing
								);
							})
							.map((b) => b.id);
						if (ids.length > 0) queue.add(ids, "last");
						setTold(ids.length);
					} finally {
						setBusy(false);
					}
				}}
			>
				{t("queue.series")}
			</button>
			<span role="status" className="text-sm text-slate-600 dark:text-slate-400">
				{told === undefined
					? ""
					: told === 0
						? t("queue.seriesNone")
						: t("queue.seriesDone", { count: told })}
			</span>
		</div>
	);
}
