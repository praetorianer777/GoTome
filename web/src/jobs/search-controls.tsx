import { useQuery } from "@tanstack/react-query";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import {
	searchStatusQuery,
	useRebuildIndex,
	useRereadText,
} from "@/jobs/search-index";

const button =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** Has the text of a book, a library, or with neither every book read again for search. */
export function RereadText({
	target,
	label,
}: {
	target: { library?: string; book?: string };
	label: string;
}) {
	const reread = useRereadText();
	return (
		<div className="flex flex-col gap-1 text-sm">
			<button
				type="button"
				disabled={reread.isPending}
				onClick={() => reread.mutate(target)}
				className={`${button} self-start`}
			>
				{label}
			</button>
			{reread.data && (
				<p role="status" className="text-slate-600 dark:text-slate-400">
					{t("searchIndex.rereadQueued", { count: reread.data.files })}
				</p>
			)}
			<FormError error={reread.error} />
		</div>
	);
}

/** How much of the text search knows, and the controls to build it again. */
export function SearchIndexPanel() {
	const status = useQuery(searchStatusQuery());
	const rebuild = useRebuildIndex();
	return (
		<section
			aria-labelledby="search-index"
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<h2 id="search-index" className="text-lg font-medium">
				{t("searchIndex.title")}
			</h2>
			<p className="max-w-2xl text-sm text-slate-600 dark:text-slate-400">
				{t("searchIndex.intro")}
			</p>
			{status.data && (
				<div role="status" className="flex flex-col gap-1 text-sm">
					<p>
						{t("searchIndex.known", {
							indexed: status.data.indexed,
							files: status.data.files,
						})}
					</p>
					{status.data.rebuilding && <p>{t("searchIndex.rebuilding")}</p>}
				</div>
			)}
			<FormError error={status.error ?? rebuild.error} />
			<div className="flex flex-wrap items-start gap-3">
				<RereadText target={{}} label={t("searchIndex.rereadAll")} />
				<div className="flex flex-col gap-1 text-sm">
					<button
						type="button"
						disabled={rebuild.isPending || status.data?.rebuilding}
						onClick={() => rebuild.mutate()}
						className={`${button} self-start`}
					>
						{t("searchIndex.rebuild")}
					</button>
					{rebuild.data && (
						<p role="status" className="text-slate-600 dark:text-slate-400">
							{t(
								rebuild.data.queued
									? "searchIndex.rebuildQueued"
									: "searchIndex.rebuildWaiting",
							)}
						</p>
					)}
				</div>
			</div>
		</section>
	);
}
