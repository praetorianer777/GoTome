import type { BookDetail } from "@/books/api";
import {
	MAX_RATING,
	type ReadingStatus,
	STATUS_LABELS,
	STATUSES,
	useSetReading,
} from "@/books/reading";
import { FormError } from "@/components/form";
import { t } from "@/i18n";

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

/** Where the signed-in person stands with the book, theirs to change. */
export function ReadingControls({ book }: { book: BookDetail }) {
	const set = useSetReading(book.id);
	const { status, rating, startedOn, finishedOn } = book.reading;
	return (
		<section
			aria-labelledby="book-reading"
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-3 text-sm dark:border-slate-800"
		>
			<h2 id="book-reading" className="sr-only">
				{t("reading.title")}
			</h2>
			<div className="flex flex-wrap items-center gap-x-6 gap-y-3">
				<label className="flex items-center gap-2">
					<span>{t("reading.status")}</span>
					<select
						value={status}
						disabled={set.isPending}
						onChange={(e) =>
							set.mutate({ status: e.target.value as ReadingStatus })
						}
						className={control}
					>
						{STATUSES.map((s) => (
							<option key={s} value={s}>
								{t(STATUS_LABELS[s])}
							</option>
						))}
					</select>
				</label>
				<fieldset className="flex items-center gap-1">
					<legend className="float-left mr-2">{t("reading.rating")}</legend>
					{Array.from({ length: MAX_RATING }, (_, i) => i + 1).map((n) => (
						<button
							key={n}
							type="button"
							disabled={set.isPending}
							aria-pressed={rating === n}
							aria-label={t("reading.stars", { count: n })}
							title={
								rating === n
									? t("reading.unrate")
									: t("reading.stars", { count: n })
							}
							// The same star again takes the rating away.
							onClick={() => set.mutate({ rating: rating === n ? 0 : n })}
							className={`text-xl leading-none ${
								rating && n <= rating
									? "text-amber-500"
									: "text-slate-300 dark:text-slate-600"
							}`}
						>
							★
						</button>
					))}
				</fieldset>
			</div>
			<div className="flex flex-wrap items-center gap-x-6 gap-y-3">
				<label className="flex items-center gap-2">
					<span>{t("reading.started")}</span>
					<input
						type="date"
						value={startedOn ?? ""}
						disabled={set.isPending}
						onChange={(e) => set.mutate({ startedOn: e.target.value })}
						className={control}
					/>
				</label>
				<label className="flex items-center gap-2">
					<span>{t("reading.finished")}</span>
					<input
						type="date"
						value={finishedOn ?? ""}
						disabled={set.isPending}
						onChange={(e) => set.mutate({ finishedOn: e.target.value })}
						className={control}
					/>
				</label>
			</div>
			<FormError error={set.error} />
		</section>
	);
}
