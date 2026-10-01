import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import {
	type Job,
	type JobGroup,
	jobIsActive,
	jobsQuery,
	useCancelJob,
	useRetryJob,
} from "@/jobs/api";

const GROUPS: { value: JobGroup | undefined; label: MessageKey }[] = [
	{ value: undefined, label: "jobs.filter.all" },
	{ value: "running", label: "jobs.filter.running" },
	{ value: "waiting", label: "jobs.filter.waiting" },
	{ value: "failed", label: "jobs.filter.failed" },
	{ value: "done", label: "jobs.filter.done" },
];

const STATES: Record<string, MessageKey> = {
	available: "jobs.state.waiting",
	pending: "jobs.state.waiting",
	scheduled: "jobs.state.scheduled",
	retryable: "jobs.state.retrying",
	running: "jobs.state.running",
	completed: "jobs.state.done",
	cancelled: "jobs.state.cancelled",
	discarded: "jobs.state.failed",
};

const when = new Intl.DateTimeFormat(undefined, {
	dateStyle: "short",
	timeStyle: "medium",
});

/** What a job does, in words. */
function describe(job: Job): string {
	switch (job.kind) {
		case "ingest.scan_library":
			return t("jobs.kind.scan", {
				library: job.libraryName || t("jobs.goneLibrary"),
			});
		case "ingest.extract_file":
			return t("jobs.kind.read", { file: job.filePath || t("jobs.goneFile") });
		case "ingest.write_metadata":
			return t("jobs.kind.write", { file: job.filePath || t("jobs.goneFile") });
		case "ingest.scan_all_libraries":
			return t("jobs.kind.scanAll");
		case "auth.sweep_sessions":
			return t("jobs.kind.sweep");
	}
	return job.kind;
}

export function Jobs() {
	const [group, setGroup] = useState<JobGroup>();
	const jobs = useQuery(jobsQuery(group));
	const retry = useRetryJob();
	const cancel = useCancelJob();

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-semibold">{t("jobs.title")}</h1>
				<p className="mt-1 max-w-2xl text-slate-600 dark:text-slate-400">
					{t("jobs.intro")}
				</p>
			</div>
			<fieldset className="flex flex-wrap items-center gap-2 text-sm">
				<legend className="sr-only">{t("jobs.filter")}</legend>
				{GROUPS.map((g) => (
					<button
						key={g.label}
						type="button"
						aria-pressed={group === g.value}
						onClick={() => setGroup(g.value)}
						className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 aria-pressed:bg-slate-900 aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:bg-slate-100 dark:aria-pressed:text-slate-900"
					>
						{t(g.label)}
					</button>
				))}
			</fieldset>
			<FormError error={jobs.error ?? retry.error ?? cancel.error} />
			{jobs.isPending && <p className="text-slate-500">{t("loading")}</p>}
			{jobs.data?.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("jobs.none")}</p>
			)}
			{jobs.data && jobs.data.length > 0 && (
				<ul
					aria-label={t("jobs.list")}
					className="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800"
				>
					{jobs.data.map((job) => (
						<li
							key={job.id}
							aria-label={describe(job)}
							className="flex flex-col gap-1 px-4 py-3 text-sm"
						>
							<div className="flex flex-wrap items-center gap-x-4 gap-y-1">
								<span className="min-w-0 flex-1 break-all font-medium">
									{job.bookId ? (
										<Link
											to="/books/$bookId"
											params={{ bookId: job.bookId }}
											className="hover:underline"
										>
											{describe(job)}
										</Link>
									) : (
										describe(job)
									)}
								</span>
								<span
									className={
										job.state === "discarded" || job.state === "cancelled"
											? "text-red-700 dark:text-red-400"
											: "text-slate-600 dark:text-slate-400"
									}
								>
									{t(STATES[job.state] ?? "jobs.state.waiting")}
								</span>
								<span className="text-slate-500">
									{t("jobs.attempts", {
										attempt: job.attempt,
										max: job.maxAttempts,
									})}
								</span>
								<span className="text-slate-500 tabular-nums">
									{when.format(
										new Date(
											job.finalizedAt ?? job.attemptedAt ?? job.createdAt,
										),
									)}
								</span>
								{jobIsActive(job) ? (
									<button
										type="button"
										disabled={cancel.isPending}
										onClick={() => cancel.mutate(job.id)}
										aria-label={t("jobs.cancelNamed", { job: describe(job) })}
										className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
									>
										{t("jobs.cancel")}
									</button>
								) : (
									<button
										type="button"
										disabled={retry.isPending}
										onClick={() => retry.mutate(job.id)}
										aria-label={t("jobs.retryNamed", { job: describe(job) })}
										className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
									>
										{t("jobs.retry")}
									</button>
								)}
							</div>
							{job.lastError && (
								<p className="break-all text-red-700 dark:text-red-400">
									{job.lastError}
								</p>
							)}
						</li>
					))}
				</ul>
			)}
		</div>
	);
}
