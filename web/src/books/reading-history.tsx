import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { Cover } from "@/books/cover";
import { type HistoryFilter, historyQuery } from "@/books/history";
import { STATUS_LABELS, type ReadingStatus } from "@/books/reading";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

const STATUSES: ReadingStatus[] = ["completed", "reading", "abandoned", "wishlist"];

/**
 * Everything the person read, are reading or gave up, the books the library
 * holds and those it does not, with filters and a search. Theirs alone.
 */
export function ReadingHistory() {
	const [status, setStatus] = useState("");
	const [year, setYear] = useState("");
	const [stars, setStars] = useState("");
	const [typed, setTyped] = useState("");
	const filter: HistoryFilter = {
		status: (status || undefined) as HistoryFilter["status"],
		year: year ? Number(year) : undefined,
		stars: stars ? Number(stars) : undefined,
		q: typed.trim() || undefined,
	};
	const history = useInfiniteQuery(historyQuery(filter));
	const entries = history.data?.pages.flatMap((p) => p.entries) ?? [];
	const years = history.data?.pages[0]?.years ?? [];

	return (
		<section aria-labelledby="reading-history" className="flex flex-col gap-3">
			<h2 id="reading-history" className="text-lg font-medium">
				{t("history.title")}
			</h2>
			<div className="flex flex-wrap items-end gap-3 text-sm">
				<label className="flex flex-col gap-1">
					<span>{t("history.year")}</span>
					<select value={year} onChange={(e) => setYear(e.target.value)} className={control}>
						<option value="">{t("history.anyYear")}</option>
						{years.map((y) => (
							<option key={y} value={y}>
								{y}
							</option>
						))}
					</select>
				</label>
				<label className="flex flex-col gap-1">
					<span>{t("history.status")}</span>
					<select value={status} onChange={(e) => setStatus(e.target.value)} className={control}>
						<option value="">{t("history.anyStatus")}</option>
						{STATUSES.map((s) => (
							<option key={s} value={s}>
								{t(STATUS_LABELS[s])}
							</option>
						))}
					</select>
				</label>
				<label className="flex flex-col gap-1">
					<span>{t("history.stars")}</span>
					<select value={stars} onChange={(e) => setStars(e.target.value)} className={control}>
						<option value="">{t("history.anyStars")}</option>
						{[5, 4, 3, 2, 1].map((n) => (
							<option key={n} value={n}>
								{t("reading.stars", { count: n })}
							</option>
						))}
					</select>
				</label>
				<label className="flex flex-col gap-1">
					<span>{t("history.search")}</span>
					<input
						type="search"
						value={typed}
						onChange={(e) => setTyped(e.target.value)}
						className={control}
					/>
				</label>
			</div>
			<FormError error={history.error} />
			{history.isSuccess && entries.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("history.none")}</p>
			)}
			<ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
				{entries.map(({ book, startedOn, finishedOn }) => (
					<li key={book.id} className="flex items-center gap-3 px-3 py-2 text-sm">
						<div className="w-10 shrink-0">
							<Cover book={book} size="small" />
						</div>
						<div className="min-w-0 flex-1">
							<Link
								to="/books/$bookId"
								params={{ bookId: book.id }}
								className="line-clamp-1 font-medium hover:underline"
							>
								{book.title}
							</Link>
							<p className="line-clamp-1 text-slate-600 dark:text-slate-400">
								{[
									book.authors.join(", "),
									t(STATUS_LABELS[book.status]),
									finishedOn
										? t("history.finishedOn", { date: formatDate(finishedOn) })
										: startedOn
											? t("history.startedOn", { date: formatDate(startedOn) })
											: "",
									book.placeholder ? t("history.notOwned") : "",
								]
									.filter(Boolean)
									.join(" · ")}
							</p>
						</div>
						{book.rating ? (
							<span
								role="img"
								aria-label={t("reading.stars", { count: book.rating })}
								className="shrink-0 text-amber-600 dark:text-amber-400"
							>
								{"★".repeat(book.rating)}
							</span>
						) : null}
					</li>
				))}
			</ul>
			{history.hasNextPage && (
				<button
					type="button"
					className="self-center rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
					disabled={history.isFetchingNextPage}
					onClick={() => history.fetchNextPage()}
				>
					{t("library.more")}
				</button>
			)}
		</section>
	);
}
