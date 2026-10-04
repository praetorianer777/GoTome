import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { statsQuery } from "@/books/stats";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";

const WINDOWS: { days: number; label: MessageKey }[] = [
	{ days: 7, label: "stats.window.week" },
	{ days: 30, label: "stats.window.month" },
	{ days: 90, label: "stats.window.quarter" },
	{ days: 365, label: "stats.window.year" },
];

const number = (v: number) => v.toLocaleString(undefined, { maximumFractionDigits: 1 });

/** What the signed-in person read and listened to. Only ever their own. */
export function Stats() {
	const [days, setDays] = useState(30);
	const stats = useQuery(statsQuery(days));
	const data = stats.data;
	const most = Math.max(1, ...(data?.days ?? []).map((d) => d.readingMinutes + d.listeningMinutes));

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-wrap items-end justify-between gap-4">
				<h1 className="text-2xl font-semibold">{t("stats.title")}</h1>
				<fieldset className="flex gap-1 text-sm">
					<legend className="sr-only">{t("stats.window")}</legend>
					{WINDOWS.map((w) => (
						<button
							key={w.days}
							type="button"
							aria-pressed={days === w.days}
							onClick={() => setDays(w.days)}
							className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 aria-pressed:bg-slate-900 aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:bg-slate-100 dark:aria-pressed:text-slate-900"
						>
							{t(w.label)}
						</button>
					))}
				</fieldset>
			</div>
			{stats.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={stats.error} />
			{data && (
				<>
					<dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
						<Figure label={t("stats.pagesPerDay")} value={`${data.pagesEstimated ? "≈ " : ""}${number(data.pagesPerDay)}`} />
						<Figure label={t("stats.readingPerDay")} value={t("stats.minutes", { minutes: number(data.readingMinutesPerDay) })} />
						<Figure label={t("stats.listeningPerDay")} value={t("stats.minutes", { minutes: number(data.listeningMinutesPerDay) })} />
						<Figure label={t("stats.totalPages")} value={`${data.pagesEstimated ? "≈ " : ""}${number(data.totalPages)}`} />
						<Figure label={t("stats.totalReading")} value={hours(data.totalReadingMinutes)} />
						<Figure label={t("stats.totalListening")} value={hours(data.totalListeningMinutes)} />
					</dl>
					{data.pagesEstimated && (
						<p className="text-sm text-slate-600 dark:text-slate-400">{t("stats.estimated")}</p>
					)}

					<section aria-labelledby="stats-days" className="flex flex-col gap-2">
						<h2 id="stats-days" className="text-lg font-medium">
							{t("stats.days")}
						</h2>
						<div aria-hidden="true" className="flex h-32 items-end gap-px rounded-md border border-slate-200 p-2 dark:border-slate-800">
							{data.days.map((d) => (
								<div
									key={d.date}
									title={`${d.date}: ${t("stats.minutes", { minutes: number(d.readingMinutes) })} · ${t("stats.minutesListened", { minutes: number(d.listeningMinutes) })}`}
									className="flex h-full min-w-0 flex-1 flex-col justify-end"
								>
									<div className="bg-sky-400 dark:bg-sky-600" style={{ height: `${(d.listeningMinutes / most) * 100}%` }} />
									<div className="bg-brand-strong dark:bg-brand" style={{ height: `${(d.readingMinutes / most) * 100}%` }} />
								</div>
							))}
						</div>
						<p className="flex gap-4 text-sm text-slate-600 dark:text-slate-400">
							<span className="flex items-center gap-1">
								<span className="inline-block size-3 bg-brand-strong dark:bg-brand" />
								{t("stats.legend.reading")}
							</span>
							<span className="flex items-center gap-1">
								<span className="inline-block size-3 bg-sky-400 dark:bg-sky-600" />
								{t("stats.legend.listening")}
							</span>
						</p>
						<table className="sr-only">
							<caption>{t("stats.days")}</caption>
							<thead>
								<tr>
									<th scope="col">{t("stats.column.date")}</th>
									<th scope="col">{t("stats.column.pages")}</th>
									<th scope="col">{t("stats.column.reading")}</th>
									<th scope="col">{t("stats.column.listening")}</th>
								</tr>
							</thead>
							<tbody>
								{data.days
									.filter((d) => d.pages || d.readingMinutes || d.listeningMinutes)
									.map((d) => (
										<tr key={d.date}>
											<td>{d.date}</td>
											<td>{number(d.pages)}</td>
											<td>{number(d.readingMinutes)}</td>
											<td>{number(d.listeningMinutes)}</td>
										</tr>
									))}
							</tbody>
						</table>
					</section>

					<section aria-labelledby="stats-years" className="flex flex-col gap-2">
						<h2 id="stats-years" className="text-lg font-medium">
							{t("stats.years")}
						</h2>
						{data.years.length === 0 ? (
							<p className="text-slate-600 dark:text-slate-400">{t("stats.noYears")}</p>
						) : (
							<ul className="flex flex-col gap-1">
								{data.years.map((y) => (
									<li key={y.year}>
										<span className="font-medium">{y.year}</span>:{" "}
										{y.finishes === y.books
											? t("stats.finished", { count: y.finishes })
											: t("stats.finishedAgain", { count: y.finishes, books: y.books })}
									</li>
								))}
							</ul>
						)}
					</section>

					<section aria-labelledby="stats-history" className="flex flex-col gap-2">
						<h2 id="stats-history" className="text-lg font-medium">
							{t("stats.history")}
						</h2>
						{data.history.length === 0 ? (
							<p className="text-slate-600 dark:text-slate-400">{t("stats.noHistory")}</p>
						) : (
							<ol className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
								{data.history.map((e) => (
									<li key={`${e.date}-${e.event}-${e.bookId}`} className="flex gap-4 px-3 py-2 text-sm">
										<span className="w-24 shrink-0 tabular-nums text-slate-500">{e.date}</span>
										<span className="w-20 shrink-0">{t(e.event === "started" ? "stats.started" : "stats.finishedOne")}</span>
										<Link to="/books/$bookId" params={{ bookId: e.bookId }} className="min-w-0 truncate font-medium hover:underline">
											{e.title}
										</Link>
									</li>
								))}
							</ol>
						)}
					</section>
				</>
			)}
		</div>
	);
}

function Figure({ label, value }: { label: string; value: string }) {
	return (
		<div className="rounded-lg border border-slate-200 p-3 dark:border-slate-800">
			<dt className="text-sm text-slate-600 dark:text-slate-400">{label}</dt>
			<dd className="text-2xl font-semibold tabular-nums">{value}</dd>
		</div>
	);
}

function hours(minutes: number): string {
	return minutes < 60
		? t("stats.minutes", { minutes })
		: t("stats.hours", { hours: Math.floor(minutes / 60), minutes: minutes % 60 });
}
