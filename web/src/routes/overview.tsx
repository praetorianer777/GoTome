import { queryOptions, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { can, currentUserQuery } from "@/auth/session";
import { EREADER_FINDINGS } from "@/books/rules";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { formatCount, formatLanguage, formatMonth, formatSize } from "@/lib/format";
import { librariesQuery } from "@/libraries/api";

type Step = components["schemas"]["Step"];
type Queue = components["schemas"]["Queue"];
type Count = components["schemas"]["Count"];

/** How often the work's progress is asked for again while any is left. */
const POLL_MS = 15_000;

function figuresQuery(library: string) {
	return queryOptions({
		queryKey: ["statistics", "figures", library],
		queryFn: async () =>
			(
				await api.GET("/statistics", {
					params: { query: library ? { library } : {} },
				})
			).data,
	});
}

function busy(s: Step) {
	return s.done < s.total;
}

function workQuery(library: string) {
	return queryOptions({
		queryKey: ["statistics", "work", library],
		queryFn: async () =>
			(
				await api.GET("/statistics/work", {
					params: { query: library ? { library } : {} },
				})
			).data,
		refetchInterval: (query) => {
			const w = query.state.data;
			return w &&
				([w.files, w.texts, w.vectors].some(busy) ||
					w.lookups.waiting > 0 ||
					w.checks.waiting > 0)
				? POLL_MS
				: false;
		},
	});
}

/** The time left as people read it: 2 h 5 min, or 3 days. */
function timeLeft(minutes: number): string {
	if (minutes < 60) return t("overview.left.minutes", { minutes });
	if (minutes < 48 * 60) {
		return t("overview.left.hours", {
			hours: Math.floor(minutes / 60),
			minutes: minutes % 60,
		});
	}
	return t("overview.left.days", { days: Math.round(minutes / 1440) });
}

function pace(lastHour: number, minutesLeft?: number) {
	const parts = [t("overview.lastHour", { count: formatCount(lastHour) })];
	if (minutesLeft !== undefined) parts.push(timeLeft(minutesLeft));
	return parts.join(" · ");
}

function StepRow({ label, step }: { label: MessageKey; step: Step }) {
	const share = step.total > 0 ? step.done / step.total : 1;
	return (
		<li className="flex flex-col gap-1">
			<div className="flex flex-wrap justify-between gap-x-4">
				<span>{t(label)}</span>
				<span className="tabular-nums">
					{t("overview.doneOf", {
						done: formatCount(step.done),
						total: formatCount(step.total),
					})}
				</span>
			</div>
			<progress
				value={step.done}
				max={Math.max(step.total, 1)}
				aria-label={t(label)}
				className="h-2 w-full"
			>
				{Math.round(share * 100)}%
			</progress>
			{busy(step) && (
				<span className="text-sm text-slate-600 dark:text-slate-400">
					{pace(step.lastHour, step.minutesLeft)}
				</span>
			)}
		</li>
	);
}

function QueueRow({
	label,
	queue,
	then,
}: {
	label: MessageKey;
	queue: Queue;
	then: string;
}) {
	return (
		<li className="flex flex-col gap-1">
			<div className="flex flex-wrap justify-between gap-x-4">
				<span>{t(label)}</span>
				<span className="tabular-nums">
					{t("overview.waiting", { count: formatCount(queue.waiting) })}
				</span>
			</div>
			<span className="text-sm text-slate-600 dark:text-slate-400">
				{[
					queue.waiting > 0 || queue.lastHour > 0
						? pace(queue.lastHour, queue.minutesLeft)
						: "",
					then,
				]
					.filter(Boolean)
					.join(" · ")}
			</span>
		</li>
	);
}

function Work({ library }: { library: string }) {
	const work = useQuery(workQuery(library));
	const w = work.data;
	return (
		<section
			aria-labelledby="overview-work"
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<h2 id="overview-work" className="text-lg font-medium">
				{t("overview.work")}
			</h2>
			<FormError error={work.error} />
			{w && (
				<ul className="flex flex-col gap-4">
					<StepRow label="overview.files" step={w.files} />
					{w.filesFailed > 0 && (
						<li className="-mt-3 text-sm">
							<Link to="/jobs" className="underline">
								{t("overview.filesFailed", {
									count: formatCount(w.filesFailed),
								})}
							</Link>
						</li>
					)}
					<StepRow label="overview.texts" step={w.texts} />
					<StepRow label="overview.vectors" step={w.vectors} />
					<StepRow label="overview.content" step={w.content} />
					<QueueRow
						label="overview.lookups"
						queue={w.lookups}
						then={t("overview.matchesPending", {
							count: formatCount(w.matchesPending),
						})}
					/>
					<QueueRow
						label="overview.checks"
						queue={w.checks}
						then={t("overview.pairsOpen", {
							count: formatCount(w.pairsOpen),
						})}
					/>
				</ul>
			)}
		</section>
	);
}

function Figure({ label, value }: { label: MessageKey; value: string }) {
	return (
		<div className="flex flex-col rounded-lg border border-slate-200 p-3 dark:border-slate-800">
			<dt className="text-sm text-slate-600 dark:text-slate-400">{t(label)}</dt>
			<dd className="text-xl font-semibold tabular-nums">{value}</dd>
		</div>
	);
}

function CountList({
	id,
	title,
	counts,
	name,
}: {
	id: string;
	title: MessageKey;
	counts: Count[];
	name: (c: Count) => string;
}) {
	if (counts.length === 0) return null;
	const most = Math.max(...counts.map((c) => c.count));
	return (
		<section aria-labelledby={id} className="flex flex-col gap-2">
			<h3 id={id} className="font-medium">
				{t(title)}
			</h3>
			<ul className="flex flex-col gap-1">
				{counts.map((c) => (
					<li
						key={c.name}
						className="grid grid-cols-[8rem_1fr_auto] items-center gap-3 text-sm"
					>
						<span className="truncate">{name(c)}</span>
						<span
							aria-hidden
							className="h-2 rounded bg-sky-600 dark:bg-sky-500"
							style={{ width: `${Math.max(2, (c.count / most) * 100)}%` }}
						/>
						<span className="tabular-nums">{formatCount(c.count)}</span>
					</li>
				))}
			</ul>
		</section>
	);
}

function Figures({ library }: { library: string }) {
	const figures = useQuery(figuresQuery(library));
	const f = figures.data;
	return (
		<section aria-labelledby="overview-library" className="flex flex-col gap-4">
			<h2 id="overview-library" className="text-lg font-medium">
				{t("overview.library")}
			</h2>
			<FormError error={figures.error} />
			{f && (
				<>
					<dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
						<Figure label="overview.books" value={formatCount(f.books)} />
						<Figure
							label="overview.filesCount"
							value={formatCount(f.files)}
						/>
						<Figure label="overview.size" value={formatSize(f.bytes)} />
						<Figure label="overview.pages" value={formatCount(f.pages)} />
						<Figure
							label="overview.audiobooks"
							value={formatCount(f.audiobooks)}
						/>
						<Figure
							label="overview.audioHours"
							value={formatCount(f.audioHours)}
						/>
						<Figure label="overview.authors" value={formatCount(f.authors)} />
						<Figure label="overview.series" value={formatCount(f.series)} />
						<Figure
							label="overview.publishers"
							value={formatCount(f.publishers)}
						/>
						<Figure label="overview.tags" value={formatCount(f.tags)} />
					</dl>
					<dl className="grid grid-cols-1 gap-3 sm:grid-cols-3">
						<Figure
							label="overview.withoutCover"
							value={formatCount(f.withoutCover)}
						/>
						<Figure
							label="overview.withoutAuthor"
							value={formatCount(f.withoutAuthor)}
						/>
						<Figure
							label="overview.withoutText"
							value={formatCount(f.withoutText)}
						/>
					</dl>
					{f.withMissingFiles > 0 && (
						<p className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
							<Link
								to="/"
								search={{ library: library || undefined, files: ["missing"] }}
								className="underline"
							>
								{t("overview.withMissingFiles", {
									count: formatCount(f.withMissingFiles),
								})}
							</Link>
						</p>
					)}
					{f.withEreaderFindings > 0 && (
						<p className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
							<Link
								to="/"
								search={{ library: library || undefined, ereader: [...EREADER_FINDINGS] }}
								className="underline"
							>
								{t("overview.withEreaderFindings", {
									count: number.format(f.withEreaderFindings),
								})}
							</Link>
						</p>
					)}
					<div className="grid gap-6 md:grid-cols-2">
						<CountList
							id="overview-formats"
							title="overview.formats"
							counts={f.formats}
							name={(c) => c.name.toUpperCase()}
						/>
						<CountList
							id="overview-languages"
							title="overview.languages"
							counts={f.languages}
							name={(c) =>
								c.name ? formatLanguage(c.name) : t("overview.noLanguage")
							}
						/>
					</div>
					<CountList
						id="overview-months"
						title="overview.months"
						counts={f.months}
						name={(c) => formatMonth(`${c.name}-01T00:00:00Z`)}
					/>
				</>
			)}
		</section>
	);
}

/**
 * What the libraries a person sees hold and, for whoever may watch the
 * background work, how far it is: one library or all of them.
 */
export function Overview() {
	const user = useQuery(currentUserQuery);
	const libraries = useQuery(librariesQuery);
	const [library, setLibrary] = useState("");
	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-wrap items-end justify-between gap-4">
				<h1 className="text-2xl font-semibold">{t("overview.title")}</h1>
				{(libraries.data?.length ?? 0) > 1 && (
					<label className="flex flex-col gap-1 text-sm">
						{t("overview.which")}
						<select
							value={library}
							onChange={(e) => setLibrary(e.target.value)}
							className="rounded-md border border-slate-300 bg-white px-2 py-1.5 dark:border-slate-600 dark:bg-slate-900"
						>
							<option value="">{t("overview.allLibraries")}</option>
							{libraries.data?.map((l) => (
								<option key={l.id} value={l.id}>
									{l.name}
								</option>
							))}
						</select>
					</label>
				)}
			</div>
			{can(user.data ?? null, "index:rebuild") && <Work library={library} />}
			<Figures library={library} />
		</div>
	);
}
