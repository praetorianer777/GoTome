import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { formatCount, formatSize } from "@/lib/format";

export type Health = components["schemas"]["Health"];
type Warning = components["schemas"]["Warning"];

/** Whether the machine has room for Postgres, the app and the embedding model. */
export const systemHealthQuery = queryOptions({
	queryKey: ["system", "health"],
	queryFn: async () => (await api.GET("/system/health")).data,
});

/** How an operator runs a statement in the deployment's database. */
function psql(sql: string): string {
	return `docker compose exec db psql -U gotome -d gotome -c "${sql}"`;
}

function warningText(w: Warning, h: Health): string {
	switch (w.kind) {
		case "memory":
			return t("health.warn.memory", {
				need: formatSize(h.memory.needBytes),
				budget: formatSize(h.memory.budgetBytes),
			});
		case "embedding":
			return t("health.warn.embedding", {
				model: h.memory.embeddingModel,
				size: formatSize(h.memory.embeddingBytes),
			});
		case "parallel":
			return t("health.warn.parallel", {
				workers: formatCount(h.postgres.maxParallelWorkers),
				cpus: formatCount(h.memory.cpus),
			});
		default:
			return t("health.warn.disk", {
				free: formatSize(h.disk.freeBytes),
				need: formatSize(h.disk.needFreeBytes),
			});
	}
}

function HealthWarning({
	warning,
	health,
}: {
	warning: Warning;
	health: Health;
}) {
	return (
		<li className="flex flex-col gap-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
			<p>{warningText(warning, health)}</p>
			{warning.commands.length > 0 && (
				<pre className="overflow-x-auto rounded bg-white p-2 text-xs text-slate-900 dark:bg-slate-900 dark:text-slate-100">
					{[...warning.commands.map(psql), "docker compose restart db"].join(
						"\n",
					)}
				</pre>
			)}
		</li>
	);
}

function Row({ label, value }: { label: MessageKey; value: string }) {
	return (
		<div className="flex flex-col">
			<dt className="text-sm text-slate-600 dark:text-slate-400">{t(label)}</dt>
			<dd className="tabular-nums">{value}</dd>
		</div>
	);
}

/**
 * The machine's memory and disk against what Postgres, the app and the
 * embedding model are set to take, with what to do where they do not fit.
 */
export function HealthPanel() {
	const health = useQuery(systemHealthQuery);
	const h = health.data;
	const unknown = t("health.unknown");
	const size = (bytes: number) => (bytes > 0 ? formatSize(bytes) : unknown);
	return (
		<section
			aria-labelledby="overview-health"
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<h2 id="overview-health" className="text-lg font-medium">
				{t("health.title")}
			</h2>
			<FormError error={health.error} />
			{h && (
				<>
					{h.warnings.length > 0 ? (
						<ul className="flex flex-col gap-3">
							{h.warnings.map((w) => (
								<HealthWarning key={w.kind} warning={w} health={h} />
							))}
						</ul>
					) : (
						<p>{t("health.fine")}</p>
					)}
					<dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
						<Row
							label="health.memory"
							value={
								h.memory.machineBytes > 0
									? t(
											h.memory.limited
												? "health.memory.limited"
												: "health.memory.machine",
											{
												size: formatSize(h.memory.machineBytes),
											},
										)
									: unknown
							}
						/>
						<Row label="health.cpus" value={formatCount(h.memory.cpus)} />
						<Row
							label="health.postgres"
							value={formatSize(h.postgres.needBytes)}
						/>
						<Row
							label="health.model"
							value={
								h.memory.embeddingModel
									? `${formatSize(h.memory.embeddingBytes)} (${h.memory.embeddingModel})`
									: t("health.noModel")
							}
						/>
						<Row
							label="health.free"
							value={
								h.disk.totalBytes > 0
									? t("health.freeOf", {
											free: formatSize(h.disk.freeBytes),
											total: formatSize(h.disk.totalBytes),
										})
									: unknown
							}
						/>
						<Row label="health.database" value={size(h.disk.databaseBytes)} />
						<Row label="health.index" value={size(h.disk.indexBytes)} />
					</dl>
					<p className="max-w-3xl text-sm text-slate-600 dark:text-slate-400">
						{t("health.note", {
							path: h.disk.path,
							app: formatSize(h.memory.appBytes),
						})}
					</p>
				</>
			)}
		</section>
	);
}
