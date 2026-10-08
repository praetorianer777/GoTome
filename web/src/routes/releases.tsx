import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { candidateCoverUrl } from "@/books/edit";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { formatDate, formatReleaseDate } from "@/lib/format";
import {
	type FollowKind,
	type Release,
	releasesQuery,
	trackersQuery,
	useFollow,
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
			<div className="flex flex-col gap-2">
				<h1 className="text-2xl font-semibold">{t("releases.title")}</h1>
				<p className="max-w-prose text-slate-600 dark:text-slate-400">
					{t("releases.intro")}
				</p>
			</div>
			<Following />
			<ReleaseList when="upcoming" />
			<ReleaseList when="recent" />
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
