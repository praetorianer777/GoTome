import { useQuery } from "@tanstack/react-query";
import { candidateCoverUrl } from "@/books/edit";
import { type DiscoverItem, discoverQuery, useDiscoverRun, useDismissDiscovered, useWishDiscovered } from "@/discover/api";
import { FormError } from "@/components/form";
import { button, EmptyState, Loading, PageHeader } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { formatDateTime } from "@/lib/format";

/** The reasons in the order the page lists them: what the data says plainly first. */
const KINDS = ["series_next", "series_gap", "series_more", "author", "genre"] as const;

const HEADINGS: Record<(typeof KINDS)[number], MessageKey> = {
	series_next: "discover.group.series_next",
	series_gap: "discover.group.series_gap",
	series_more: "discover.group.series_more",
	author: "discover.group.author",
	genre: "discover.group.genre",
};

/**
 * Books beyond the libraries that the person's taste points to, each with
 * why, and a way to wish for it or to say it is not for them. Only theirs.
 */
export function Discover() {
	const discovered = useQuery(discoverQuery);
	const run = useDiscoverRun();
	const list = discovered.data;
	const state = list?.state;
	return (
		<div className="flex flex-col gap-6">
			<PageHeader
				title={t("discover.title")}
				intro={t("discover.intro")}
				actions={
					(state === "ready" || state === "running") && (
						<button
							type="button"
							className={button("secondary")}
							disabled={run.isPending || state === "running"}
							onClick={() => run.mutate()}
						>
							{t("discover.again")}
						</button>
					)
				}
			/>
			{discovered.isPending && <Loading />}
			<FormError error={discovered.error ?? run.error} />
			{state === "off" && (
				<EmptyState title={t("discover.off.title")}>{t("discover.off")}</EmptyState>
			)}
			{state === "offline" && (
				<EmptyState title={t("discover.offline.title")}>{t("discover.offline")}</EmptyState>
			)}
			{state === "never" && (
				<EmptyState
					title={t("discover.never.title")}
					action={
						<button
							type="button"
							className={button("primary", "md")}
							disabled={run.isPending}
							onClick={() => run.mutate()}
						>
							{t("discover.start")}
						</button>
					}
				>
					{t("discover.never")}
				</EmptyState>
			)}
			{state === "running" && (
				<p role="status" className="text-slate-600 dark:text-slate-400">
					{t("discover.running")}
				</p>
			)}
			{state === "ready" && list?.finishedAt && (
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{t("discover.finished", { when: formatDateTime(list.finishedAt) })}
				</p>
			)}
			{state === "ready" && list?.items.length === 0 && (
				<EmptyState title={t("discover.none.title")}>{t("discover.none")}</EmptyState>
			)}
			{KINDS.map((kind) => {
				const items = list?.items.filter((i) => i.reason.kind === kind) ?? [];
				if (items.length === 0) return null;
				return (
					<section key={kind} aria-labelledby={`discover-${kind}`} className="flex flex-col gap-3">
						<h2 id={`discover-${kind}`} className="text-lg font-medium">
							{t(HEADINGS[kind])}
						</h2>
						<ul className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
							{items.map((item) => (
								<li key={item.id} className="min-w-0">
									<DiscoverCard item={item} />
								</li>
							))}
						</ul>
					</section>
				);
			})}
		</div>
	);
}

function reasonText(item: DiscoverItem): string {
	const { kind, name, stars } = item.reason;
	switch (kind) {
		case "series_next":
			return t("discover.reason.series_next", { name });
		case "series_gap":
			return t("discover.reason.series_gap", { name });
		case "series_more":
			return t("discover.reason.series_more", { name });
		case "genre":
			return t("discover.reason.genre", { name: t(`genre.${name}` as MessageKey) });
	}
	return stars > 0
		? t("discover.reason.authorStars", { name, stars })
		: t("discover.reason.author", { name });
}

function DiscoverCard({ item }: { item: DiscoverItem }) {
	const wish = useWishDiscovered();
	const dismiss = useDismissDiscovered();
	const busy = wish.isPending || dismiss.isPending;
	return (
		<div className="flex min-w-0 gap-3">
			<div className="h-28 w-20 shrink-0 overflow-hidden rounded bg-slate-200 dark:bg-slate-800">
				{item.coverToken && (
					<img
						src={candidateCoverUrl(item.coverToken)}
						alt=""
						loading="lazy"
						className="h-full w-full object-cover"
					/>
				)}
			</div>
			<div className="flex min-w-0 flex-1 flex-col gap-1 text-sm">
				<span className="line-clamp-2 break-words font-medium">{item.title}</span>
				{item.authors.length > 0 && (
					<span className="line-clamp-1 break-words text-slate-600 dark:text-slate-400">
						{item.authors.join(", ")}
						{item.year ? ` · ${item.year}` : ""}
					</span>
				)}
				{item.series && (
					<span className="line-clamp-1 break-words text-slate-600 dark:text-slate-400">
						{item.seriesIndex != null ? `${item.series} #${item.seriesIndex}` : item.series}
					</span>
				)}
				<span className="break-words">{reasonText(item)}</span>
				{item.reason.because.length > 0 && (
					<span className="line-clamp-2 break-words text-slate-600 dark:text-slate-400">
						{t("discover.because", { books: item.reason.because.join(", ") })}
					</span>
				)}
				<div className="mt-1 flex flex-wrap gap-2">
					<button
						type="button"
						className={button("secondary")}
						disabled={busy}
						aria-label={t("discover.wishNamed", { title: item.title })}
						onClick={() => wish.mutate(item)}
					>
						{t("discover.wish")}
					</button>
					<button
						type="button"
						className="min-h-6 text-slate-600 underline underline-offset-4 hover:text-slate-900 disabled:opacity-60 dark:text-slate-400 dark:hover:text-white"
						disabled={busy}
						aria-label={t("discover.notInterestedNamed", { title: item.title })}
						onClick={() => dismiss.mutate(item.id)}
					>
						{t("discover.notInterested")}
					</button>
				</div>
				<FormError error={wish.error ?? dismiss.error} />
			</div>
		</div>
	);
}
