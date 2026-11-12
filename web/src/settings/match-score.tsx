import {
	queryOptions,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { type ChangeEvent, type ReactNode, useRef } from "react";
import { api, assetUrl } from "@/api/client";
import type { components } from "@/api/schema";
import { FormError, fieldErrors } from "@/components/form";
import { button, card } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { en } from "@/i18n/en";
import { formatCount, formatDateTime, formatDuration } from "@/lib/format";

type Status = components["schemas"]["ScoringStatus"];
type Run = components["schemas"]["RunStatus"];
type Check = components["schemas"]["CheckReport"];
type Train = components["schemas"]["TrainReport"];
type Variant = components["schemas"]["Variant"];
type Tally = components["schemas"]["Tally"];

const POLL_MS = 3000;

function isRunning(run?: Run) {
	return run?.state === "queued" || run?.state === "running";
}

/** The match score's state and last results, asked for again while a run goes. */
export const matchScoringQuery = queryOptions({
	queryKey: ["metadata", "scoring"],
	queryFn: async (): Promise<Status | undefined> =>
		(await api.GET("/metadata/scoring")).data,
	refetchInterval: (query) =>
		isRunning(query.state.data?.measure) || isRunning(query.state.data?.learn)
			? POLL_MS
			: false,
});

function useRun(path: "/metadata/scoring/measure" | "/metadata/scoring/learn") {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async () => {
			const { error } = await api.POST(path, { body: {} });
			if (error) {
				throw error;
			}
		},
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: matchScoringQuery.queryKey }),
	});
}

function useImport() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (file: File) => {
			let body: components["schemas"]["Profile"];
			try {
				body = JSON.parse(await file.text());
			} catch {
				throw new Error(t("matchScore.import.unreadable"));
			}
			await api.PUT("/metadata/scoring/import", { body });
		},
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: matchScoringQuery.queryKey }),
	});
}

function useForget() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async () => {
			await api.DELETE("/metadata/scoring/import");
		},
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: matchScoringQuery.queryKey }),
	});
}

function percent(n: number, of: number): string {
	return of === 0 ? "-" : `${formatCount((100 * n) / of, 0)}%`;
}

function weightName(feature: string): string {
	const key = `matchScore.feature.${feature}`;
	if (feature.startsWith("provider:")) {
		return t("matchScore.feature.provider", { name: feature.slice(9) });
	}
	return key in en ? t(key as MessageKey) : feature;
}

function scoreName(name: string): string {
	if (name.startsWith("profile:")) {
		return t("matchScore.score.profile", { name: name.slice(8) });
	}
	const key = `matchScore.score.${name}`;
	return key in en ? t(key as MessageKey) : name;
}

function Table({
	caption,
	head,
	rows,
}: {
	caption: string;
	head: string[];
	rows: ReactNode[][];
}) {
	return (
		<div className="min-w-0 overflow-x-auto">
			<table className="w-full text-sm">
				<caption className="pb-1 text-left font-medium">{caption}</caption>
				<thead>
					<tr className="border-b border-slate-200 text-left dark:border-slate-700">
						{head.map((h, i) => (
							<th
								key={h}
								scope="col"
								className={`py-1 pr-3 font-normal text-slate-600 dark:text-slate-400 ${i > 0 ? "text-right" : ""}`}
							>
								{h}
							</th>
						))}
					</tr>
				</thead>
				<tbody>
					{rows.map((row) => (
						<tr
							key={String(row[0])}
							className="border-b border-slate-100 dark:border-slate-800"
						>
							{row.map((cell, i) => (
								// biome-ignore lint/suspicious/noArrayIndexKey: columns are fixed
								<td key={i} className={`py-1 pr-3 ${i > 0 ? "text-right tabular-nums" : ""}`}>
									{cell}
								</td>
							))}
						</tr>
					))}
				</tbody>
			</table>
		</div>
	);
}

function outcomeRow(name: string, x: Tally): ReactNode[] {
	const asked = x.books - x.failed;
	return [
		name,
		formatCount(x.books, 0),
		percent(x.right, asked),
		percent(x.sameWork, asked),
		percent(x.edition, asked),
		percent(x.wrong, asked),
		percent(x.none, asked),
	];
}

function VariantTables({
	variants,
	threshold,
}: {
	variants: Variant[];
	threshold: number;
}) {
	return (
		<>
			<Table
				caption={t("matchScore.byScore", { threshold: formatCount(threshold, 2) })}
				head={[
					t("matchScore.col.score"),
					t("matchScore.col.right"),
					t("matchScore.col.edition"),
					t("matchScore.col.wrong"),
					t("matchScore.col.nothing"),
					t("matchScore.col.taken"),
					t("matchScore.col.takenRight"),
					t("matchScore.col.takenWrong"),
				]}
				rows={variants.map((v) => {
					const asked = v.all.books - v.all.failed;
					const taken = v.thresholds.find((p) => p.threshold === threshold);
					return [
						scoreName(v.name),
						percent(v.all.right, asked),
						percent(v.all.edition + v.all.sameWork, asked),
						percent(v.all.wrong, asked),
						percent(v.all.none, asked),
						formatCount(taken?.taken ?? 0, 0),
						percent(taken?.right ?? 0, taken?.taken ?? 0),
						percent(taken?.wrong ?? 0, taken?.taken ?? 0),
					];
				})}
			/>
			{variants.map((v) => (
				<Table
					key={v.name}
					caption={t("matchScore.calibration", { score: scoreName(v.name) })}
					head={[
						t("matchScore.col.band"),
						t("matchScore.col.candidates"),
						t("matchScore.col.predicted"),
						t("matchScore.col.found"),
					]}
					rows={v.calibration
						.filter((b) => b.count > 0)
						.map((b) => [
							`${formatCount(b.from, 2)} - ${formatCount(b.to, 2)}`,
							formatCount(b.count, 0),
							`${formatCount(100 * b.predicted, 0)}%`,
							`${formatCount(100 * b.found, 0)}%`,
						])}
				/>
			))}
		</>
	);
}

function CheckResult({ report }: { report: Check }) {
	return (
		<div className="flex flex-col gap-4">
			<p className="text-sm">
				{t("matchScore.measured", {
					sample: formatCount(report.sample, 0),
					books: formatCount(report.books, 0),
					providers: report.providers.join(", "),
					using: scoreName(report.using),
				})}
			</p>
			<Table
				caption={t("matchScore.firstCandidate")}
				head={[
					t("matchScore.col.ranking"),
					t("matchScore.col.books"),
					t("matchScore.col.right"),
					t("matchScore.col.sameWork"),
					t("matchScore.col.edition"),
					t("matchScore.col.wrong"),
					t("matchScore.col.nothing"),
				]}
				rows={[report.all, ...report.perProvider, ...report.perLanguage].map(
					(x) =>
						outcomeRow(
							x.name === "all" ? t("matchScore.all") : languageName(x.name),
							x,
						),
				)}
			/>
			<Table
				caption={t("matchScore.thresholds", {
					threshold: formatCount(report.threshold, 2),
				})}
				head={[
					t("matchScore.col.threshold"),
					t("matchScore.col.taken"),
					t("matchScore.col.right"),
					t("matchScore.col.sameWork"),
					t("matchScore.col.edition"),
					t("matchScore.col.wrong"),
				]}
				rows={report.thresholds.map((p) => [
					formatCount(p.threshold, 2),
					formatCount(p.taken, 0),
					percent(p.right, p.taken),
					percent(p.sameWork, p.taken),
					percent(p.edition, p.taken),
					percent(p.wrong, p.taken),
				])}
			/>
			{report.variants.length > 1 && (
				<VariantTables variants={report.variants} threshold={report.threshold} />
			)}
		</div>
	);
}

function languageName(name: string): string {
	const key = `matchScore.language.${name}`;
	return key in en ? t(key as MessageKey) : name;
}

function TrainResult({ report }: { report: Train }) {
	return (
		<div className="flex flex-col gap-4">
			<p className="text-sm">
				{t("matchScore.learnedFrom", {
					sample: formatCount(report.sample, 0),
					books: formatCount(report.books, 0),
					providers: report.providers.join(", "),
					candidates: formatCount(report.candidates, 0),
					right: formatCount(report.right, 0),
					held: formatCount(report.testBooks, 0),
					lossTrain: formatCount(report.logLossTrain, 3),
					lossTest: formatCount(report.logLossTest, 3),
				})}
			</p>
			<Table
				caption={t("matchScore.weights")}
				head={[
					t("matchScore.col.feature"),
					t("matchScore.col.handSet"),
					t("matchScore.col.learned"),
				]}
				rows={report.weights.map((w) => [
					weightName(w.feature),
					w.handSet === undefined ? "-" : formatCount(w.handSet, 2),
					formatCount(w.learned, 2),
				])}
			/>
			<p className="text-sm font-medium">{t("matchScore.heldOut")}</p>
			<VariantTables
				variants={[report.handSet, report.learned]}
				threshold={report.threshold}
			/>
		</div>
	);
}

function Progress({ run }: { run: Run }) {
	if (!isRunning(run)) {
		return null;
	}
	const elapsed = Date.now() - new Date(run.startedAt).getTime();
	const left =
		run.done > 0
			? (elapsed / run.done) * (run.total - run.done)
			: run.estimateSeconds * 1000;
	return (
		<p role="status" className="text-sm">
			{run.state === "queued"
				? t("matchScore.queued")
				: t("matchScore.progress", {
						done: formatCount(run.done, 0),
						total: formatCount(run.total, 0),
						left: formatDuration(Math.max(left, 0)),
					})}
		</p>
	);
}

function Result({
	run,
	children,
}: {
	run?: Run;
	children: ReactNode;
}) {
	if (!run) {
		return <p className="text-sm text-slate-600 dark:text-slate-400">{t("matchScore.never")}</p>;
	}
	return (
		<div className="flex flex-col gap-3">
			<Progress run={run} />
			{run.state === "failed" && (
				<p role="alert" className="text-sm text-red-800 dark:text-red-300">
					{t("matchScore.failed", { error: run.error ?? "" })}
				</p>
			)}
			{run.state === "done" && run.finishedAt && (
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{t("matchScore.finished", { when: formatDateTime(run.finishedAt) })}
				</p>
			)}
			{run.state === "done" && children}
		</div>
	);
}

/** Measure and learn the match score, with their results, and share what was learned. */
export function MatchScorePanel() {
	const status = useQuery(matchScoringQuery);
	const measure = useRun("/metadata/scoring/measure");
	const learn = useRun("/metadata/scoring/learn");
	const upload = useImport();
	const forget = useForget();
	const picker = useRef<HTMLInputElement>(null);
	const s = status.data;
	if (!s) {
		return null;
	}
	const running = isRunning(s.measure) || isRunning(s.learn);
	function picked(event: ChangeEvent<HTMLInputElement>) {
		const file = event.target.files?.[0];
		if (file) {
			upload.mutate(file);
		}
		event.target.value = "";
	}
	return (
		<section
			aria-labelledby="match-score"
			className={`${card} flex min-w-0 flex-col gap-4 p-4`}
		>
			<h3 id="match-score" className="text-base font-medium">
				{t("matchScore.title")}
			</h3>
			<p className="max-w-3xl text-sm text-slate-600 dark:text-slate-400">
				{t("matchScore.intro", { books: formatCount(s.booksWithIsbn, 0) })}
			</p>
			<p className="text-sm">
				{t("matchScore.using", { using: scoreName(s.using) })}
				{s.learned &&
					` ${t("matchScore.learnedOn", { when: formatDateTime(s.learned.learnedAt), books: formatCount(s.learned.books, 0) })}`}
			</p>
			<div className="flex flex-wrap gap-2">
				<button
					type="button"
					className={button("secondary")}
					disabled={running || measure.isPending}
					onClick={() => measure.mutate()}
				>
					{t("matchScore.measure")}
				</button>
				<button
					type="button"
					className={button("secondary")}
					disabled={running || learn.isPending || s.booksWithIsbn === 0}
					onClick={() => learn.mutate()}
				>
					{t("matchScore.learn")}
				</button>
			</div>
			<FormError error={measure.error ?? learn.error} />

			<div className="flex flex-col gap-2">
				<h4 className="font-medium">{t("matchScore.measure")}</h4>
				<Result run={s.measure}>
					{s.measureReport && <CheckResult report={s.measureReport} />}
				</Result>
			</div>
			<div className="flex flex-col gap-2">
				<h4 className="font-medium">{t("matchScore.learn")}</h4>
				<Result run={s.learn}>
					{s.learnReport && <TrainResult report={s.learnReport} />}
				</Result>
			</div>

			<div className="flex flex-col gap-2">
				<h4 className="font-medium">{t("matchScore.share")}</h4>
				<p className="max-w-3xl text-sm text-slate-600 dark:text-slate-400">
					{t("matchScore.share.hint")}
				</p>
				<div className="flex flex-wrap items-center gap-2">
					{s.learned ? (
						<a
							className={button("secondary")}
							href={assetUrl("/metadata/scoring/export")}
							download="gotome-match-weights.json"
						>
							{t("matchScore.export")}
						</a>
					) : (
						<span className="text-sm text-slate-600 dark:text-slate-400">
							{t("matchScore.export.none")}
						</span>
					)}
					<button
						type="button"
						className={button("secondary")}
						disabled={upload.isPending}
						onClick={() => picker.current?.click()}
					>
						{t("matchScore.import")}
					</button>
					<input
						ref={picker}
						type="file"
						accept="application/json,.json"
						className="sr-only"
						tabIndex={-1}
						aria-label={t("matchScore.import")}
						onChange={picked}
					/>
				</div>
				{upload.error && (
					<p role="alert" className="text-sm text-red-800 dark:text-red-300">
						{fieldErrors(upload.error).file ??
							(upload.error instanceof Error ? upload.error.message : "")}
					</p>
				)}
				{s.imported && (
					<div className="flex flex-wrap items-center gap-2 text-sm">
						<span>
							{t("matchScore.imported", {
								books: formatCount(s.imported.books, 0),
								version: s.imported.gotomeVersion,
							})}
						</span>
						<button
							type="button"
							className={button("secondary")}
							disabled={forget.isPending}
							onClick={() => forget.mutate()}
						>
							{t("matchScore.import.forget")}
						</button>
					</div>
				)}
			</div>
		</section>
	);
}
