import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useId, useRef, useState } from "react";
import { Dialog } from "@/components/dialog";
import { FormError } from "@/components/form";
import { button, card, Loading } from "@/components/ui";
import {
	type DuplicateClassCount,
	duplicateClassesQuery,
	useStartDuplicateRun,
} from "@/curation/api";
import { type MessageKey, t } from "@/i18n";

const key = (prefix: string, name: string) => `${prefix}${name}` as MessageKey;

/**
 * The pairs the rules are sure of, per class, with a dry run that says what
 * would be done and a run that does it, to be followed and undone under
 * Changes.
 */
export function AutoDuplicates() {
	const classes = useQuery(duplicateClassesQuery);
	const start = useStartDuplicateRun();
	const [asking, setAsking] = useState<DuplicateClassCount>();
	const [started, setStarted] = useState<{ runId: string; dryRun: boolean }>();
	const list = classes.data ?? [];
	const go = (c: DuplicateClassCount, dryRun: boolean) =>
		start.mutate(
			{ classes: [c.class], dryRun },
			{ onSuccess: (run) => run && setStarted({ runId: run.runId, dryRun }) },
		);
	return (
		<section
			aria-label={t("duplicates.auto.title")}
			className={`${card} flex min-w-0 flex-col gap-3 p-4`}
		>
			<h2 className="font-serif text-lg">{t("duplicates.auto.title")}</h2>
			<p className="text-sm text-slate-600 dark:text-slate-400">
				{t("duplicates.auto.intro")}
			</p>
			{classes.isPending && <Loading rows={2} />}
			<FormError error={classes.error ?? start.error} />
			{list.length > 0 && (
				<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
					{list.map((c) => (
						<li
							key={c.class}
							aria-label={t(key("duplicates.auto.class.", c.class))}
							className="grid grid-cols-1 gap-2 py-2 text-sm sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
						>
							<div className="flex min-w-0 flex-col gap-0.5">
								<span className="font-medium">
									{t(key("duplicates.auto.class.", c.class))}
								</span>
								<span className="text-slate-600 dark:text-slate-400">
									{t(c.merges ? "duplicates.auto.merges" : "duplicates.auto.links")}
									{" · "}
									{t("duplicates.auto.pairs", { count: c.pairs })}
									{c.daily && ` · ${t("duplicates.auto.daily")}`}
								</span>
							</div>
							<div className="flex flex-wrap gap-2">
								<button
									type="button"
									disabled={c.pairs === 0 || start.isPending}
									aria-label={t("duplicates.auto.dryNamed", {
										class: t(key("duplicates.auto.class.", c.class)),
									})}
									onClick={() => go(c, true)}
									className={button()}
								>
									{t("duplicates.auto.dry")}
								</button>
								<button
									type="button"
									disabled={c.pairs === 0 || start.isPending}
									aria-label={t("duplicates.auto.runNamed", {
										class: t(key("duplicates.auto.class.", c.class)),
									})}
									onClick={() => setAsking(c)}
									className={button("primary")}
								>
									{t("duplicates.auto.run")}
								</button>
							</div>
						</li>
					))}
				</ul>
			)}
			{started && (
				<p role="status" className="text-sm">
					{t(started.dryRun ? "duplicates.auto.dryStarted" : "duplicates.auto.started")}{" "}
					<Link to="/curation" search={{ run: started.runId }} className="underline underline-offset-4">
						{t("duplicates.auto.seeChanges")}
					</Link>
				</p>
			)}
			{asking && (
				<Confirm
					item={asking}
					onCancel={() => setAsking(undefined)}
					onConfirm={() => {
						go(asking, false);
						setAsking(undefined);
					}}
				/>
			)}
		</section>
	);
}

function Confirm({
	item,
	onConfirm,
	onCancel,
}: {
	item: DuplicateClassCount;
	onConfirm: () => void;
	onCancel: () => void;
}) {
	const id = useId();
	const cancel = useRef<HTMLButtonElement>(null);
	useEffect(() => cancel.current?.focus(), []);
	const name = t(key("duplicates.auto.class.", item.class));
	return (
		<Dialog
			alert
			title={t("duplicates.auto.confirm.title", { count: item.pairs, class: name })}
			describedBy={`${id}-body`}
			onClose={onCancel}
		>
			<p id={`${id}-body`} className="text-sm text-slate-700 dark:text-slate-300">
				{t(
					item.merges
						? "duplicates.auto.confirm.merges"
						: "duplicates.auto.confirm.links",
					{ count: item.pairs },
				)}
			</p>
			<div className="flex flex-wrap justify-end gap-2">
				<button
					ref={cancel}
					type="button"
					onClick={onCancel}
					className={button("secondary", "md")}
				>
					{t("duplicates.auto.confirm.cancel")}
				</button>
				<button type="button" onClick={onConfirm} className={button("primary", "md")}>
					{t("duplicates.auto.confirm.run", { count: item.pairs })}
				</button>
			</div>
		</Dialog>
	);
}
