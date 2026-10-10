import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import { can, currentUserQuery } from "@/auth/session";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { settingsQuery } from "@/settings/api";

/** How often the progress is asked for again while books are embedded. */
const POLL_MS = 15_000;

/** How far the books the person sees are embedded, and whether it goes on. */
export const embeddingStatusQuery = queryOptions({
	queryKey: ["embedding", "status"],
	queryFn: async () => (await api.GET("/embedding/status")).data,
	refetchInterval: (query) => {
		const s = query.state.data;
		return s?.enabled && (s.metadata < s.books || s.content < s.withText) ? POLL_MS : false;
	},
});

/** Pauses embedding, or starts it again where it stopped. */
function usePauseEmbedding() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (on: boolean) =>
			(await api.PATCH("/settings", { body: { values: { "embedding.enabled": on ? "on" : "off" } } })).data
				?.settings ?? [],
		onSuccess: (settings) => {
			queryClient.setQueryData(settingsQuery.queryKey, settings);
			queryClient.invalidateQueries({ queryKey: ["embedding"] });
		},
	});
}

const button =
	"rounded-md border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The progress of embedding, with a button to pause or start it for whoever may change settings. */
export function EmbeddingPanel() {
	const status = useQuery(embeddingStatusQuery);
	const user = useQuery(currentUserQuery);
	const toggle = usePauseEmbedding();
	const s = status.data;
	return (
		<section
			aria-labelledby="embedding"
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<h2 id="embedding" className="text-lg font-medium">
				{t("embedding.title")}
			</h2>
			<p className="max-w-2xl text-sm text-slate-600 dark:text-slate-400">{t("embedding.intro")}</p>
			{s && (
				<div role="status" className="flex flex-col gap-1 text-sm">
					<p>
						{t("embedding.progress", {
							metadata: s.metadata,
							books: s.books,
							content: s.content,
							withText: s.withText,
						})}
					</p>
					<p className="text-slate-600 dark:text-slate-400">
						{t(s.enabled ? "embedding.running" : "embedding.paused", { model: s.model })}
					</p>
				</div>
			)}
			<FormError error={status.error ?? toggle.error} />
			{s && can(user.data ?? null, "settings:manage") && (
				<button
					type="button"
					className={`${button} self-start`}
					disabled={toggle.isPending}
					onClick={() => toggle.mutate(!s.enabled)}
				>
					{t(s.enabled ? "embedding.pause" : "embedding.start")}
				</button>
			)}
		</section>
	);
}
