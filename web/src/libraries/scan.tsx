import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import {
	type Library,
	type Scan,
	scanIsActive,
	useScanLibrary,
} from "@/libraries/api";
import { Link } from "@tanstack/react-router";
import { useRereadLibrary } from "@/jobs/api";

const when = new Intl.DateTimeFormat(undefined, {
	dateStyle: "medium",
	timeStyle: "short",
});

const CHANGES: [keyof Scan, MessageKey][] = [
	["filesAdded", "scan.added"],
	["filesChanged", "scan.changed"],
	["filesMoved", "scan.moved"],
	["filesRestored", "scan.restored"],
	["filesMissing", "scan.missing"],
	["filesSkipped", "scan.skipped"],
];

function describe(scan: Scan | undefined): string {
	if (!scan) {
		return t("scan.never");
	}
	if (scan.state === "queued") {
		return t("scan.queued");
	}
	if (scan.state === "running") {
		return t("scan.running");
	}
	const at = when.format(new Date(scan.finishedAt ?? scan.requestedAt));
	if (scan.state === "failed") {
		return t("scan.failed", { when: at, error: scan.error ?? "" });
	}
	const changes = CHANGES.filter(([field]) => Number(scan[field]) > 0).map(
		([field, key]) => t(key, { count: Number(scan[field]) }),
	);
	return t("scan.done", {
		when: at,
		count: scan.filesSeen,
		changes: changes.length > 0 ? changes.join(", ") : t("scan.nothingNew"),
	});
}

/** What the last scan of a library found, and the button that starts one. */
export function ScanStatus({
	library,
	canScan,
}: {
	library: Library;
	canScan: boolean;
}) {
	const scan = useScanLibrary();
	const reread = useRereadLibrary();
	const active = scanIsActive(library.lastScan);
	const failed = library.lastScan?.state === "failed";

	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
				<p
					role="status"
					className={
						failed
							? "text-red-700 dark:text-red-400"
							: "text-slate-600 dark:text-slate-400"
					}
				>
					{describe(library.lastScan)}
					{library.filesPending > 0 &&
						!scanIsActive(library.lastScan) &&
						` ${t("scan.reading", { count: library.filesPending })}`}
				</p>
				{canScan && (
					<button
						type="button"
						disabled={active || scan.isPending}
						onClick={() => scan.mutate(library.id)}
						className="rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
					>
						{t("scan.start")}
					</button>
				)}
			</div>
			{library.filesFailed > 0 && (
				<div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
					<p className="text-red-700 dark:text-red-400">
						{t("scan.failedFiles", { count: library.filesFailed })}
					</p>
					{canScan && (
						<>
							<button
								type="button"
								disabled={reread.isPending}
								onClick={() => reread.mutate({ id: library.id, failedOnly: true })}
								className="rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("scan.rereadFailed")}
							</button>
							<Link to="/jobs" className="text-brand-strong underline underline-offset-4 dark:text-brand">
								{t("scan.toJobs")}
							</Link>
						</>
					)}
				</div>
			)}
			<FormError error={scan.error ?? reread.error} />
		</div>
	);
}
