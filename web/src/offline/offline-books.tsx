import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { button } from "@/components/ui";
import { t } from "@/i18n";
import { formatSize } from "@/lib/format";
import { clearOffline, keptQuery, removeKept, storageEstimate } from "./kept";
import { queuedPositions } from "./queue";
import { offlineSupported, workerSupported } from "./support";

/** The books kept in this browser, the space they take, and a way to remove them. */
export function OfflineBooks() {
	const queryClient = useQueryClient();
	const kept = useQuery(keptQuery);
	const estimate = useQuery({ queryKey: ["offline", "estimate"], queryFn: storageEstimate });
	const waiting = useQuery({
		queryKey: ["offline", "queue"],
		queryFn: async () => (await queuedPositions()).length,
	});
	const changed = () => queryClient.invalidateQueries({ queryKey: ["offline"] });
	const remove = useMutation({ mutationFn: removeKept, onSettled: changed });
	const removeAll = useMutation({ mutationFn: clearOffline, onSettled: changed });
	const files = kept.data ?? [];
	const total = files.reduce((sum, f) => sum + f.size, 0);
	return (
		<section aria-labelledby="offline" className="flex flex-col gap-2">
			<h2 id="offline" className="text-lg font-medium">
				{t("offline.title")}
			</h2>
			{!workerSupported() ? (
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{offlineSupported() ? t("offline.noWorker") : t("offline.unsupported")}
				</p>
			) : files.length === 0 ? (
				<p className="text-sm text-slate-600 dark:text-slate-400">{t("offline.none")}</p>
			) : (
				<>
					<ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 bg-white dark:divide-slate-800 dark:border-slate-800 dark:bg-slate-950">
						{files.map((file) => (
							<li
								key={file.fileId}
								className="flex flex-wrap items-center gap-x-4 gap-y-2 px-3 py-2 text-sm"
							>
								<span className="min-w-0 flex-1 wrap-anywhere">
									<Link
										to="/books/$bookId"
										params={{ bookId: file.bookId }}
										className="font-medium underline"
									>
										{file.title}
									</Link>
									<span className="block text-slate-500 dark:text-slate-400">
										{[file.authors, file.name].filter(Boolean).join(" · ")}
									</span>
								</span>
								<span className="text-slate-600 dark:text-slate-400">{formatSize(file.size)}</span>
								<button
									type="button"
									disabled={remove.isPending}
									onClick={() => remove.mutate(file.fileId)}
									aria-label={t("offline.removeNamed", { name: file.name })}
									className={button("danger")}
								>
									{t("offline.remove")}
								</button>
							</li>
						))}
					</ul>
					<p className="text-sm text-slate-600 dark:text-slate-400">
						{t("offline.total", { size: formatSize(total) })}
						{estimate.data &&
							` ${t("offline.browser", {
								used: formatSize(estimate.data.used),
								quota: formatSize(estimate.data.quota),
							})}`}
					</p>
					<div>
						<button
							type="button"
							disabled={removeAll.isPending}
							onClick={() => removeAll.mutate()}
							className={button("danger")}
						>
							{t("offline.removeAll")}
						</button>
					</div>
				</>
			)}
			{(waiting.data ?? 0) > 0 && (
				<p role="status" className="text-sm text-slate-600 dark:text-slate-400">
					{t("offline.waiting", { count: waiting.data ?? 0 })}
				</p>
			)}
		</section>
	);
}
