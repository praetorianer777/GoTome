import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { BookDetail } from "@/books/api";
import { useSetNote } from "@/books/history";
import { type ProgressState, progressQuery } from "@/books/progress";
import {
	MAX_RATING,
	type ReadingStatus,
	STATUSES,
	statusLabel,
	useSetReading,
} from "@/books/reading";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { formatDuration } from "@/lib/format";

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

/** Where the signed-in person stands with the book, theirs to change. */
export function ReadingControls({ book }: { book: BookDetail }) {
	const set = useSetReading(book.id);
	const progress = useQuery(progressQuery(book.id)).data;
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
								{t(statusLabel(s, book.placeholder))}
							</option>
						))}
					</select>
				</label>
				<fieldset className="flex items-center gap-2">
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
							className={`size-6 text-xl leading-none ${
								rating && n <= rating
									? "text-amber-600 dark:text-amber-400"
									: "text-slate-500 dark:text-slate-400"
							}`}
						>
							{rating && n <= rating ? "★" : "☆"}
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
			<Note book={book} />
			{progress && <Where progress={progress} />}
			<FormError error={set.error} />
		</section>
	);
}

/** The person's private note or review of the book. */
function Note({ book }: { book: BookDetail }) {
	const save = useSetNote(book.id);
	const saved = book.reading.note ?? "";
	const [typed, setTyped] = useState<string>();
	const text = typed ?? saved;
	return (
		<div className="flex flex-col gap-2">
			<label className="flex flex-col gap-1">
				<span>{t("reading.note")}</span>
				<textarea
					value={text}
					rows={3}
					onChange={(e) => setTyped(e.target.value)}
					className={`${control} w-full`}
				/>
			</label>
			<div className="flex items-center gap-3">
				<button
					type="button"
					disabled={save.isPending || text === saved}
					onClick={() => save.mutate(text, { onSuccess: () => setTyped(undefined) })}
					className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
				>
					{t("reading.noteSave")}
				</button>
				<span className="text-slate-600 dark:text-slate-400">{t("reading.noteHint")}</span>
			</div>
			<FormError error={save.error} />
		</div>
	);
}

/** How far the person is, in the text and in the audio, and how often done. */
function Where({ progress }: { progress: ProgressState }) {
	const lines: string[] = [];
	if (progress.ebook) {
		lines.push(
			t("progress.ebook", { percent: Math.round(progress.ebook.fraction * 100) }) +
				(progress.ebook.chapter ? ` · ${progress.ebook.chapter}` : ""),
		);
	}
	if (progress.audio) {
		lines.push(
			progress.audio.positionMs != null
				? t("progress.audioAt", {
						percent: Math.round(progress.audio.fraction * 100),
						at: formatDuration(progress.audio.positionMs),
					})
				: t("progress.audio", { percent: Math.round(progress.audio.fraction * 100) }),
		);
	}
	if (progress.finishes > 1) {
		lines.push(t("progress.finishes", { count: progress.finishes }));
	}
	if (lines.length === 0) {
		return null;
	}
	return (
		<ul className="flex flex-col gap-1 text-slate-600 dark:text-slate-400">
			{lines.map((line) => (
				<li key={line}>{line}</li>
			))}
		</ul>
	);
}
