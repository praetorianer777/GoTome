import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import type { AutoRunStarted } from "./auto";

const quietButton =
	"rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";
const solidButton =
	"rounded-md bg-brand-strong px-3 py-2 text-sm font-medium text-white hover:bg-sky-800 disabled:opacity-60 dark:bg-brand dark:text-ink dark:hover:bg-sky-300";

/**
 * Buttons that queue a run of a rule, or a dry run of it, which records what
 * the rule would do and changes nothing. A run asks first, naming how many it
 * takes; both then link to the run on the Changes page.
 */
export function AutoRun({
	count,
	name,
	disabled,
	start,
	pending,
	error,
	runLabel,
	runDisabled,
}: {
	/** How many the rule takes, as the confirmation says. */
	count: number;
	/** What the buttons are about, for their accessible names. */
	name: string;
	disabled?: boolean;
	start: (dryRun: boolean) => Promise<AutoRunStarted | undefined>;
	pending: boolean;
	error?: unknown;
	/** What the run button says when it is not just "Run". */
	runLabel?: string;
	/** Keeps the run from being asked for; a dry run stays open. */
	runDisabled?: boolean;
}) {
	const [sure, setSure] = useState(false);
	const [started, setStarted] = useState<AutoRunStarted>();
	const go = (dryRun: boolean) => {
		setSure(false);
		start(dryRun).then(setStarted, () => {});
	};
	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-wrap items-center gap-2">
				{sure ? (
					<>
						<span>{t("auto.sure", { count })}</span>
						<button
							type="button"
							className={solidButton}
							disabled={pending}
							onClick={() => go(false)}
						>
							{t("auto.yes", { count })}
						</button>
						<button type="button" className={quietButton} onClick={() => setSure(false)}>
							{t("auto.no")}
						</button>
					</>
				) : (
					<>
						<button
							type="button"
							className={quietButton}
							disabled={disabled || pending}
							aria-label={t("auto.dryRunNamed", { name })}
							onClick={() => go(true)}
						>
							{t("auto.dryRun")}
						</button>
						<button
							type="button"
							className={quietButton}
							disabled={disabled || runDisabled || pending}
							aria-label={t("auto.runNamed", { name, label: runLabel ?? t("auto.run") })}
							onClick={() => setSure(true)}
						>
							{runLabel ?? t("auto.run")}
						</button>
					</>
				)}
			</div>
			<FormError error={error} />
			{started && (
				<p role="status" className="text-sm">
					{t(started.dryRun ? "auto.dryRunQueued" : "auto.runQueued")}{" "}
					<Link
						to="/curation"
						search={{ run: started.runId }}
						className="underline underline-offset-4"
					>
						{t("auto.seeRun")}
					</Link>
				</p>
			)}
		</div>
	);
}
