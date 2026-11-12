import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { candidateCoverUrl } from "@/books/edit";
import { FormError } from "@/components/form";
import { button as uiButton, control as uiControl, PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { formatDate, formatDateTime, formatReleaseDate } from "@/lib/format";
import {
	type Feed,
	type FollowKind,
	feedItemsQuery,
	feedsQuery,
	type Release,
	releasesQuery,
	splitList,
	trackersQuery,
	useAddFeed,
	useFollow,
	useRemoveFeed,
	useRetryFeed,
	useUnfollow,
} from "@/releases/api";

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The authors and series the person follows, and their new books. */
export function Releases() {
	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t("releases.title")} intro={t("releases.intro")} />
			<Following />
			<ReleaseList when="upcoming" />
			<ReleaseList when="recent" />
			<Feeds />
			<FeedItems />
		</div>
	);
}

function Following() {
	const trackers = useQuery(trackersQuery);
	const follow = useFollow();
	const unfollow = useUnfollow();
	const [kind, setKind] = useState<FollowKind>("author");
	const [name, setName] = useState("");
	return (
		<section
			aria-labelledby="following"
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<h2 id="following" className="text-lg font-medium">
				{t("releases.following")}
			</h2>
			<FormError error={trackers.error ?? unfollow.error} />
			{trackers.isSuccess && trackers.data.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">
					{t("releases.followingNone")}
				</p>
			)}
			{trackers.isSuccess && trackers.data.length > 0 && (
				<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
					{trackers.data.map((tr) => (
						<li
							key={tr.id}
							className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2"
						>
							<span className="font-medium">{tr.name}</span>
							<span className="text-sm text-slate-500">
								{t(
									tr.kind === "author"
										? "releases.kind.author"
										: "releases.kind.series",
								)}
								{" · "}
								{tr.polledAt
									? t("releases.askedAt", { when: formatDate(tr.polledAt) })
									: t("releases.notAskedYet")}
							</span>
							<button
								type="button"
								className={`${button} ml-auto text-sm`}
								disabled={unfollow.isPending}
								aria-label={t("releases.unfollow", { name: tr.name })}
								onClick={() => unfollow.mutate(tr.id)}
							>
								×
							</button>
						</li>
					))}
				</ul>
			)}
			<form
				className="flex flex-wrap items-end gap-3 text-sm"
				onSubmit={(e) => {
					e.preventDefault();
					if (name.trim() === "") return;
					follow.mutate(
						{ kind, name: name.trim() },
						{ onSuccess: () => setName("") },
					);
				}}
			>
				<label className="flex flex-col gap-1">
					<span>{t("releases.kind")}</span>
					<select
						value={kind}
						onChange={(e) => setKind(e.target.value as FollowKind)}
						className={control}
					>
						<option value="author">{t("releases.kind.author")}</option>
						<option value="series">{t("releases.kind.series")}</option>
					</select>
				</label>
				<label className="flex flex-col gap-1">
					<span>{t("releases.name")}</span>
					<input
						value={name}
						onChange={(e) => setName(e.target.value)}
						className={control}
					/>
				</label>
				<button type="submit" className={button} disabled={follow.isPending}>
					{t("releases.add")}
				</button>
			</form>
			<FormError error={follow.error} />
		</section>
	);
}

function ReleaseList({ when }: { when: "upcoming" | "recent" }) {
	const releases = useQuery(releasesQuery(when));
	const id = `releases-${when}`;
	return (
		<section aria-labelledby={id} className="flex flex-col gap-3">
			<h2 id={id} className="text-lg font-medium">
				{t(when === "upcoming" ? "releases.upcoming" : "releases.recent")}
			</h2>
			<FormError error={releases.error} />
			{releases.isSuccess && releases.data.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">
					{t(
						when === "upcoming"
							? "releases.upcomingNone"
							: "releases.recentNone",
					)}
				</p>
			)}
			<ul className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{releases.data?.map((r) => (
					<ReleaseCard key={r.id} release={r} />
				))}
			</ul>
		</section>
	);
}

function ReleaseCard({ release: r }: { release: Release }) {
	return (
		<li className="flex gap-3">
			<div className="h-24 w-16 shrink-0 overflow-hidden rounded bg-slate-200 dark:bg-slate-800">
				{r.coverToken && (
					<img
						src={candidateCoverUrl(r.coverToken)}
						alt=""
						loading="lazy"
						className="h-full w-full object-cover"
					/>
				)}
			</div>
			<div className="flex min-w-0 flex-col gap-0.5 text-sm">
				<span className="line-clamp-2 font-medium">{r.title}</span>
				{r.authors.length > 0 && (
					<span className="line-clamp-1 text-slate-600 dark:text-slate-400">
						{r.authors.join(", ")}
					</span>
				)}
				{r.series && (
					<span className="line-clamp-1 text-slate-600 dark:text-slate-400">
						{r.seriesIndex != null ? `${r.series} #${r.seriesIndex}` : r.series}
					</span>
				)}
				<span>
					{r.date && r.precision
						? formatReleaseDate(r.date, r.precision)
						: t("releases.dateUnknown")}
				</span>
				{r.inLibrary && (
					<span className="font-medium text-emerald-700 dark:text-emerald-400">
						{t("releases.inLibrary")}
					</span>
				)}
				<span className="text-slate-500">
					{t("releases.via", { name: r.following.name })} ·{" "}
					{t("releases.source", { source: r.source })}
				</span>
			</div>
		</li>
	);
}

function Feeds() {
	const feeds = useQuery(feedsQuery);
	const add = useAddFeed();
	const remove = useRemoveFeed();
	const retry = useRetryFeed();
	const [url, setUrl] = useState("");
	const [keywords, setKeywords] = useState("");
	const [authors, setAuthors] = useState("");
	return (
		<section
			aria-labelledby="feeds"
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<h2 id="feeds" className="text-lg font-medium">
				{t("feeds.title")}
			</h2>
			<p className="text-sm text-slate-600 dark:text-slate-400">{t("feeds.intro")}</p>
			<FormError error={feeds.error ?? remove.error ?? retry.error} />
			{feeds.isSuccess && feeds.data.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("feeds.none")}</p>
			)}
			{feeds.isSuccess && feeds.data.length > 0 && (
				<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
					{feeds.data.map((f) => (
						<li key={f.id} className="flex flex-wrap items-start gap-x-3 gap-y-1 py-2">
							<div className="flex min-w-0 flex-1 flex-col gap-0.5 text-sm">
								<span className="font-medium">{f.title ?? f.url}</span>
								{f.title && (
									<span className="break-all text-slate-500">{f.url}</span>
								)}
								<span className="text-slate-600 dark:text-slate-400">{feedFilters(f)}</span>
								<FeedState feed={f} />
							</div>
							{!f.nextFetchAt && (
								<button
									type="button"
									className={uiButton()}
									disabled={retry.isPending}
									onClick={() => retry.mutate(f.id)}
								>
									{t("feeds.retry")}
								</button>
							)}
							<button
								type="button"
								className={uiButton()}
								disabled={remove.isPending}
								aria-label={t("feeds.remove", { name: f.title ?? f.url })}
								onClick={() => remove.mutate(f.id)}
							>
								×
							</button>
						</li>
					))}
				</ul>
			)}
			<form
				className="flex flex-wrap items-end gap-3 text-sm"
				onSubmit={(e) => {
					e.preventDefault();
					if (url.trim() === "") return;
					add.mutate(
						{ url: url.trim(), keywords: splitList(keywords, ","), authors: splitList(authors, ";") },
						{
							onSuccess: () => {
								setUrl("");
								setKeywords("");
								setAuthors("");
							},
						},
					);
				}}
			>
				<label className="flex min-w-64 flex-1 flex-col gap-1">
					<span>{t("feeds.url")}</span>
					<input
						type="url"
						required
						value={url}
						onChange={(e) => setUrl(e.target.value)}
						placeholder="https://"
						className={uiControl}
					/>
				</label>
				<div className="flex flex-col gap-1">
					<label className="flex flex-col gap-1">
						<span>{t("feeds.keywords")}</span>
						<input
							value={keywords}
							onChange={(e) => setKeywords(e.target.value)}
							aria-describedby="feeds-keywords-hint"
							className={uiControl}
						/>
					</label>
					<span id="feeds-keywords-hint" className="text-xs text-slate-500">
						{t("feeds.keywords.hint")}
					</span>
				</div>
				<div className="flex flex-col gap-1">
					<label className="flex flex-col gap-1">
						<span>{t("feeds.authors")}</span>
						<input
							value={authors}
							onChange={(e) => setAuthors(e.target.value)}
							aria-describedby="feeds-authors-hint"
							className={uiControl}
						/>
					</label>
					<span id="feeds-authors-hint" className="text-xs text-slate-500">
						{t("feeds.authors.hint")}
					</span>
				</div>
				<button type="submit" className={uiButton("primary")} disabled={add.isPending}>
					{t("feeds.add")}
				</button>
			</form>
			<FormError error={add.error} />
		</section>
	);
}

function feedFilters(f: Feed): string {
	const parts: string[] = [];
	if (f.keywords.length > 0) parts.push(t("feeds.withKeywords", { words: f.keywords.join(", ") }));
	if (f.authors.length > 0) parts.push(t("feeds.withAuthors", { names: f.authors.join("; ") }));
	return parts.length > 0 ? parts.join(" · ") : t("feeds.everyItem");
}

/** When the feed was read, or why it could not be and what happens next. */
function FeedState({ feed: f }: { feed: Feed }) {
	if (f.lastError) {
		return (
			<span className="text-red-700 dark:text-red-300">
				{f.nextFetchAt
					? t("feeds.failing", { error: f.lastError, when: formatDateTime(f.nextFetchAt) })
					: t("feeds.stopped", { error: f.lastError, count: f.failures })}
			</span>
		);
	}
	return (
		<span className="text-slate-500">
			{f.checkedAt ? t("feeds.checkedAt", { when: formatDateTime(f.checkedAt) }) : t("feeds.notFetchedYet")}
		</span>
	);
}

function FeedItems() {
	const items = useQuery(feedItemsQuery);
	return (
		<section aria-labelledby="feed-items" className="flex flex-col gap-3">
			<h2 id="feed-items" className="text-lg font-medium">
				{t("feeds.items")}
			</h2>
			<FormError error={items.error} />
			{items.isSuccess && items.data.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("feeds.itemsNone")}</p>
			)}
			{items.isSuccess && items.data.length > 0 && (
				<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
					{items.data.map((it) => (
						<li key={`${it.feedId} ${it.link ?? it.title}`} className="flex flex-col gap-0.5 py-2 text-sm">
							{it.link ? (
								<a
									href={it.link}
									target="_blank"
									rel="noopener noreferrer"
									className="font-medium text-brand-strong hover:underline dark:text-brand"
								>
									{it.title}
								</a>
							) : (
								<span className="font-medium">{it.title}</span>
							)}
							<span className="text-slate-600 dark:text-slate-400">
								{[it.authors.join(", "), it.feed, formatDate(it.published ?? it.seenAt)]
									.filter((s) => s !== "")
									.join(" · ")}
							</span>
						</li>
					))}
				</ul>
			)}
		</section>
	);
}
