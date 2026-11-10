import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { FormError } from "@/components/form";
import { button, control, Loading, PageHeader } from "@/components/ui";
import {
	type CurationAction,
	type CurationOutcome,
	isBusy,
	type CurationRun,
	curationActionsQuery,
	curationRunsQuery,
	RUN_KINDS,
	RUN_STATES,
	type RunFilter,
	type RunKind,
	type RunState,
	useConfirmRun,
	useUndoAction,
	useUndoRun,
} from "@/curation/api";
import { type MessageKey, t } from "@/i18n";
import { en } from "@/i18n/en";
import { formatDateTime } from "@/lib/format";

const key = (prefix: string, name: string) => `${prefix}${name}` as MessageKey;

/** What was changed to the books, run by run, with a way back. */
export function Curation({ run: highlighted }: { run?: string }) {
	const [filter, setFilter] = useState<RunFilter>({});
	const runs = useInfiniteQuery(curationRunsQuery(filter));
	const list = runs.data?.pages.flatMap((p) => p?.runs ?? []) ?? [];
	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t("curation.title")} intro={t("curation.intro")} />
			<div className="flex flex-wrap gap-4 text-sm">
				<label className="flex items-center gap-2">
					{t("curation.filterKind")}
					<select
						className={control}
						value={filter.kind ?? ""}
						onChange={(e) =>
							setFilter({ ...filter, kind: (e.target.value || undefined) as RunKind })
						}
					>
						<option value="">{t("curation.all")}</option>
						{RUN_KINDS.map((k) => (
							<option key={k} value={k}>
								{t(key("curation.kind.", k))}
							</option>
						))}
					</select>
				</label>
				<label className="flex items-center gap-2">
					{t("curation.filterState")}
					<select
						className={control}
						value={filter.state ?? ""}
						onChange={(e) =>
							setFilter({ ...filter, state: (e.target.value || undefined) as RunState })
						}
					>
						<option value="">{t("curation.all")}</option>
						{RUN_STATES.map((s) => (
							<option key={s} value={s}>
								{t(key("curation.state.", s))}
							</option>
						))}
					</select>
				</label>
			</div>
			<FormError error={runs.error} />
			{runs.isPending && <Loading />}
			{runs.isSuccess && list.length === 0 && (
				<p role="status" className="text-slate-600 dark:text-slate-400">
					{t("curation.empty")}
				</p>
			)}
			{list.length > 0 && (
				<ul
					aria-label={t("curation.list")}
					className="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800"
				>
					{list.map((run) => (
						<Run key={run.id} run={run} open={run.id === highlighted} />
					))}
				</ul>
			)}
			{runs.hasNextPage && (
				<button
					type="button"
					disabled={runs.isFetchingNextPage}
					onClick={() => runs.fetchNextPage()}
					className={`${button()} self-start`}
				>
					{t("curation.moreRuns")}
				</button>
			)}
		</div>
	);
}

function Run({ run, open: opened }: { run: CurationRun; open: boolean }) {
	const [open, setOpen] = useState(opened);
	const undo = useUndoRun();
	const confirm = useConfirmRun();
	const when = formatDateTime(run.createdAt);
	const kind = t(key("curation.kind.", run.kind));
	const who =
		run.rule === "auto-duplicates"
			? t("curation.ruleDuplicates")
			: run.rule
				? t("curation.byRule", { what: kind, rule: run.rule })
				: t("curation.by", { what: kind, who: run.startedByName ?? "" });
	const undoable =
		!run.dryRun && !run.undoing && run.state !== "running" && run.state !== "planned" && run.undone < run.actions;
	return (
		<li aria-label={`${kind} ${when}`} className="flex flex-col gap-3 px-4 py-3 text-sm">
			<div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
				<span className="min-w-0 flex-1 font-medium">{who}</span>
				<span className="text-slate-600 dark:text-slate-400">{when}</span>
				{run.dryRun && (
					<span
						title={t("curation.dryHint")}
						className="rounded-full border border-amber-400 px-2 py-0.5 dark:border-amber-600"
					>
						{t("curation.dryRun")}
					</span>
				)}
				<span className="rounded-full border border-slate-300 px-2 py-0.5 dark:border-slate-600">
					{run.undoing ? t("curation.stateUndoing") : t(key("curation.state.", run.state))}
				</span>
			</div>
			<p className="text-slate-600 dark:text-slate-400">
				{run.undoing
					? t("curation.undoing", { count: run.actions, undone: run.undone })
					: run.dryRun
						? t("curation.planned")
						: t("curation.counts", { count: run.actions, undone: run.undone })}
				{run.skipped > 0 && ` · ${t("curation.skipped", { count: run.skipped })}`}
			</p>
			<div className="flex flex-wrap gap-2">
				<button
					type="button"
					aria-expanded={open}
					onClick={() => setOpen(!open)}
					className={button()}
				>
					{open ? t("curation.hide") : t("curation.show")}
				</button>
				{undoable && (
					<button
						type="button"
						disabled={undo.isPending}
						aria-label={t("curation.undoRunNamed", { when })}
						onClick={() => undo.mutate(run.id, { onSuccess: () => setOpen(true) })}
						className={button()}
					>
						{t("curation.undoRun")}
					</button>
				)}
				{run.state === "done" && !run.dryRun && !run.undoing && (
					<button
						type="button"
						disabled={confirm.isPending}
						aria-label={t("curation.confirmNamed", { when })}
						title={t("curation.confirmHint")}
						onClick={() => confirm.mutate(run.id)}
						className={button()}
					>
						{t("curation.confirm")}
					</button>
				)}
			</div>
			<FormError error={undo.error ?? confirm.error} />
			{open && <Actions run={run} />}
		</li>
	);
}

function Actions({ run }: { run: CurationRun }) {
	const pages = useInfiniteQuery(curationActionsQuery(run.id, isBusy(run)));
	const actions = pages.data?.pages.flatMap((p) => p?.actions ?? []) ?? [];
	return (
		<div className="flex flex-col gap-3">
			<FormError error={pages.error} />
			{pages.isPending && <Loading rows={1} />}
			{pages.isSuccess && actions.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("curation.noActions")}</p>
			)}
			{actions.length > 0 && (
				<ul
					aria-label={t("curation.actions")}
					className="flex flex-col gap-3"
				>
					{actions.map((a) => (
						<ActionRow key={a.id} action={a} dry={run.dryRun} />
					))}
				</ul>
			)}
			{pages.hasNextPage && (
				<button
					type="button"
					disabled={pages.isFetchingNextPage}
					onClick={() => pages.fetchNextPage()}
					className={`${button()} self-start`}
				>
					{t("curation.more")}
				</button>
			)}
		</div>
	);
}

function outcomeText(o: CurationOutcome): string {
	const tables = (o.conflicts ?? []).map((c) => tableLabel(c.table)).join(", ");
	const message = o.message ?? "";
	switch (o.state) {
		case "undone":
			return t("curation.outcome.undone");
		case "alreadyUndone":
			return t("curation.outcome.alreadyUndone");
		case "conflict":
			return t("curation.outcome.conflict", { tables });
		case "notUndoable":
			return t("curation.outcome.notUndoable", { message });
		default:
			return t("curation.outcome.failed", { message });
	}
}

function tableLabel(table: string): string {
	const label = key("curation.table.", table);
	return label in en ? t(label) : table;
}

function ActionRow({ action, dry }: { action: CurationAction; dry: boolean }) {
	const undo = useUndoAction();
	const title = action.books[0]?.title ?? "";
	return (
		<li
			aria-label={`${t(key("curation.action.", action.kind))} ${title}`}
			className="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 dark:border-slate-800"
		>
			<div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
				<span className="font-medium">
					{t(key("curation.action.", action.kind))}
				</span>
				{action.reason && (
					<span className="text-slate-600 dark:text-slate-400">{action.reason}</span>
				)}
				{action.trashedFiles > 0 && (
					<span className="text-slate-600 dark:text-slate-400">
						{t("curation.trashed", { count: action.trashedFiles })}
					</span>
				)}
				<span className="ml-auto text-slate-600 dark:text-slate-400">
					{t(key("curation.state.", action.state === "done" ? "done" : "undone"))}
				</span>
			</div>
			{action.books.map((b) => (
				<div key={b.id} className="flex flex-col gap-1">
					<Link
						to="/books/$bookId"
						params={{ bookId: b.id }}
						className="font-medium text-brand-strong underline underline-offset-4 dark:text-brand"
					>
						{b.title}
					</Link>
					{dry ? null : b.changes.length === 0 ? (
						<p className="text-slate-600 dark:text-slate-400">{t("curation.noDetail")}</p>
					) : (
						<table className="w-full text-left">
							<tbody>
								{b.changes.map((c) => (
									<tr key={c.field} className="align-top">
										<th scope="row" className="w-32 pr-3 font-normal text-slate-600 dark:text-slate-400">
											{t(key("curation.field.", c.field))}
										</th>
										<td className="break-words pr-3">
											<span className="sr-only">{t("curation.before")}: </span>
											{c.before || <em>{t("curation.nothing")}</em>}
										</td>
										<td className="break-words">
											<span className="sr-only">{t("curation.after")}: </span>
											{c.after || <em>{t("curation.nothing")}</em>}
										</td>
									</tr>
								))}
							</tbody>
						</table>
					)}
				</div>
			))}
			{action.note && action.state === "done" && !undo.data && (
				<p role="status" className="text-amber-800 dark:text-amber-300">
					{action.note}
				</p>
			)}
			{undo.data && undo.data.state !== "undone" && (
				<p role="status" className="text-amber-800 dark:text-amber-300">
					{outcomeText(undo.data)}
				</p>
			)}
			<FormError error={undo.error} />
			{action.state === "done" && !dry && (
				<button
					type="button"
					disabled={undo.isPending}
					aria-label={t("curation.undoNamed", { title })}
					onClick={() => undo.mutate(action.id)}
					className={`${button()} self-start`}
				>
					{t("curation.undo")}
				</button>
			)}
		</li>
	);
}
