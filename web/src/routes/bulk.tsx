import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { BULK_OUTCOMES, type BulkOutcome, bulkQuery } from "@/books/bulk";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";

const OUTCOMES: Record<BulkOutcome, MessageKey> = {
	changed: "bulk.outcome.changed",
	unchanged: "bulk.outcome.unchanged",
	locked: "bulk.outcome.locked",
	review: "bulk.outcome.review",
	notFound: "bulk.outcome.notFound",
	failed: "bulk.outcome.failed",
};

/** The fields a book's result may name as skipped, by what the form calls them. */
function fieldName(field: string): string {
	const known: Record<string, MessageKey> = {
		contributors: "bulk.field.authors",
		series: "bulk.field.series",
		publisher: "bulk.field.publisher",
		published: "bulk.field.published",
		language: "bulk.field.language",
		tags: "bulk.field.tags",
	};
	const key = known[field];
	return key ? t(key) : field;
}

/** How a bulk change goes, and what it came to for each book. */
export function BulkProgress({ id }: { id: string }) {
	const bulk = useQuery(bulkQuery(id));
	const [shown, setShown] = useState<BulkOutcome | undefined>();
	const status = bulk.data;

	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">
				{status ? t(`bulk.title.${status.action}`) : t("bulk.title")}
			</h1>
			{bulk.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={bulk.error} />
			{status && (
				<>
					<div className="flex flex-col gap-2">
						<progress
							value={status.done}
							max={Math.max(status.total, 1)}
							aria-label={t("bulk.progressLabel")}
							className="h-2 w-full max-w-xl"
						/>
						<p>
							{t("bulk.progress", { done: status.done, total: status.total })}{" "}
							<span className="text-slate-600 dark:text-slate-400">
								{status.finishedAt ? t("bulk.finished") : t("bulk.running")}
							</span>
						</p>
					</div>
					<fieldset className="flex flex-wrap gap-2 text-sm">
						<legend className="sr-only">{t("bulk.show")}</legend>
						<button
							type="button"
							aria-pressed={shown === undefined}
							onClick={() => setShown(undefined)}
							className={pill}
						>
							{t("bulk.show.all", { count: status.total })}
						</button>
						{BULK_OUTCOMES.filter((o) => status.counts[o]).map((o) => (
							<button
								key={o}
								type="button"
								aria-pressed={shown === o}
								onClick={() => setShown(o)}
								className={pill}
							>
								{t(OUTCOMES[o])}: {status.counts[o]}
							</button>
						))}
					</fieldset>
					{(status.counts.review ?? 0) > 0 && (
						<p>
							<Link
								to="/review"
								className="text-brand-strong hover:underline dark:text-brand"
							>
								{t("bulk.toReview")}
							</Link>
						</p>
					)}
					<ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
						{status.books
							.filter((b) => !shown || b.outcome === shown)
							.map((b) => (
								<li
									key={b.bookId}
									className="flex flex-wrap items-baseline gap-x-4 gap-y-1 px-3 py-2"
								>
									<Link
										to="/books/$bookId"
										params={{ bookId: b.bookId }}
										className="font-medium hover:underline"
									>
										{b.title}
									</Link>
									<span
										className={
											b.outcome === "failed"
												? "text-red-700 dark:text-red-400"
												: "text-slate-600 dark:text-slate-400"
										}
									>
										{b.outcome
											? t(OUTCOMES[b.outcome])
											: t("bulk.outcome.waiting")}
									</span>
									{b.skipped.length > 0 && (
										<span className="text-sm text-amber-800 dark:text-amber-300">
											{t("bulk.skipped", {
												fields: b.skipped.map(fieldName).join(", "),
											})}
										</span>
									)}
									{b.message && (
										<span className="w-full text-sm text-slate-600 dark:text-slate-400">
											{b.message}
										</span>
									)}
								</li>
							))}
					</ul>
				</>
			)}
		</div>
	);
}

const pill =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 aria-pressed:bg-slate-900 aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:bg-slate-100 dark:aria-pressed:text-slate-900";
