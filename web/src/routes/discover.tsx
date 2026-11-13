import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useEffect, useState } from "react";
import { AuthorLink } from "@/books/name-links";
import { Dialog } from "@/components/dialog";
import { candidateCoverUrl } from "@/books/edit";
import { type DiscoverItem, type DiscoverProgress, discoverQuery, useDiscoverRun, useDismissDiscovered, useWishDiscovered } from "@/discover/api";
import { FormError } from "@/components/form";
import { button, EmptyState, Loading, PageHeader } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { formatDateTime, formatLanguage, formatTime } from "@/lib/format";

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
export function Discover({
	open,
	onOpen,
}: {
	/** The candidate whose details are open, kept in the address. */
	open?: string;
	onOpen: (id: string | undefined) => void;
}) {
	const discovered = useQuery(discoverQuery);
	const run = useDiscoverRun();
	const list = discovered.data;
	const state = list?.state;
	const opened = open ? list?.items.find((i) => i.id === open) : undefined;
	useEffect(() => {
		if (open && list && !opened) onOpen(undefined);
	}, [open, list, opened, onOpen]);
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
			{state === "running" && <Progress progress={list?.progress} />}
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
									<DiscoverCard item={item} onOpen={() => onOpen(item.id)} />
								</li>
							))}
						</ul>
					</section>
				);
			})}
			{opened && (
				<DiscoverPanel
					item={opened}
					onClose={() => {
						onOpen(undefined);
						// Safari does not focus a button it was clicked, so the dialog cannot return there.
						requestAnimationFrame(() =>
							document.querySelector<HTMLElement>(`[data-discover-open="${opened.id}"]`)?.focus(),
						);
					}}
				/>
			)}
		</div>
	);
}

/** How far the person's own run is: asked of planned, and whether it works or waits. */
function Progress({ progress }: { progress: DiscoverProgress | undefined }) {
	if (!progress) {
		return (
			<p role="status" className="text-slate-600 dark:text-slate-400">
				{t("discover.running")}
			</p>
		);
	}
	const { authors, series, genres } = progress;
	const planned = authors.planned + series.planned + genres.planned;
	const asked = authors.asked + series.asked + genres.asked;
	const label = planned > 0 ? t("discover.progress.asked", { asked, planned }) : t("discover.progress.preparing");
	return (
		<div role="status" className="flex flex-col gap-2 text-slate-600 dark:text-slate-400">
			<p className="font-medium text-slate-900 dark:text-slate-100">{label}</p>
			<div
				role="progressbar"
				aria-label={t("discover.progress.label")}
				aria-valuemin={0}
				aria-valuemax={planned > 0 ? planned : undefined}
				aria-valuenow={planned > 0 ? asked : undefined}
				aria-valuetext={label}
				className="h-2 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-800"
			>
				<div
					className={`h-full rounded-full bg-slate-700 dark:bg-slate-300 ${planned > 0 ? "" : "w-1/4 animate-pulse"}`}
					style={planned > 0 ? { width: `${Math.round((asked / planned) * 100)}%` } : undefined}
				/>
			</div>
			{planned > 0 && (
				<p className="text-sm">
					{t("discover.progress.parts", {
						authors: authors.asked,
						authorsPlanned: authors.planned,
						series: series.asked,
						seriesPlanned: series.planned,
						genres: genres.asked,
						genresPlanned: genres.planned,
					})}
				</p>
			)}
			<p className="text-sm">
				{progress.activity === "working" ? t("discover.progress.working") : waitingText(progress.nextPassAt)}
				{progress.startedAt && ` ${t("discover.progress.started", { when: formatDateTime(progress.startedAt) })}`}
			</p>
			{progress.backingOff.length > 0 && (
				<p className="text-sm">
					{t("discover.progress.backingOff", {
						providers: progress.backingOff.map((b) => b.provider).join(", "),
						when: formatTime(progress.backingOff.reduce((a, b) => (a.until > b.until ? a : b)).until),
					})}
				</p>
			)}
		</div>
	);
}

function waitingText(next: string | undefined): string {
	if (!next) return t("discover.progress.waiting");
	const minutes = Math.ceil((new Date(next).getTime() - Date.now()) / 60_000);
	if (minutes < 1) return t("discover.progress.waitingSoon");
	return t("discover.progress.waitingAbout", { minutes });
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

/** A cover, or a placeholder of the same size where there is none or the source has none to give. */
function Cover({ token, className }: { token: string | undefined; className: string }) {
	const [failed, setFailed] = useState(false);
	return (
		<div className={`shrink-0 overflow-hidden rounded bg-slate-200 dark:bg-slate-800 ${className}`}>
			{token && !failed && (
				<img
					src={candidateCoverUrl(token)}
					alt=""
					loading="lazy"
					className="h-full w-full object-cover"
					onError={() => setFailed(true)}
				/>
			)}
		</div>
	);
}

function DiscoverCard({ item, onOpen }: { item: DiscoverItem; onOpen: () => void }) {
	const wish = useWishDiscovered();
	const dismiss = useDismissDiscovered();
	const busy = wish.isPending || dismiss.isPending;
	return (
		<div className="flex min-w-0 gap-3">
			<Cover token={item.coverToken} className="h-28 w-20" />
			<div className="flex min-w-0 flex-1 flex-col gap-1 text-sm">
				<button
					type="button"
					data-discover-open={item.id}
					aria-haspopup="dialog"
					onClick={onOpen}
					className="line-clamp-2 break-words text-left font-medium hover:underline"
				>
					{item.title}
				</button>
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

function sourceName(provider: string): string {
	const key = `settingChoice.metadata.providers.${provider}`;
	return ["openlibrary", "crossref", "hardcover", "dnb", "google"].includes(provider) ? t(key as MessageKey) : provider;
}

/** What the source said of a book, at the side (a sheet from the bottom on a phone), with the two choices the card has. */
function DiscoverPanel({ item, onClose }: { item: DiscoverItem; onClose: () => void }) {
	const wish = useWishDiscovered();
	const dismiss = useDismissDiscovered();
	const busy = wish.isPending || dismiss.isPending;
	const isbns = item.identifiers.filter((i) => i.type === "isbn").map((i) => i.value);
	const rows: [string, ReactNode][] = [];
	if (item.series) {
		rows.push([t("discover.panel.series"), item.seriesIndex != null ? `${item.series} ${t("series.number", { index: item.seriesIndex })}` : item.series]);
	}
	if (item.published || item.year) rows.push([t("book.published"), item.published || String(item.year)]);
	if (item.publisher) rows.push([t("book.publisher"), item.publisher]);
	if (item.pageCount) rows.push([t("book.pages"), String(item.pageCount)]);
	if (item.language) rows.push([t("book.language"), formatLanguage(item.language)]);
	if (isbns.length > 0) rows.push([t("discover.panel.isbn"), isbns.join(", ")]);
	if (item.tags.length > 0) rows.push([t("discover.panel.categories"), item.tags.join(", ")]);
	rows.push([t("discover.panel.source"), sourceName(item.provider)]);
	return (
		<Dialog title={item.title} onClose={onClose} side>
			<div className="flex min-w-0 flex-col gap-4">
				<Cover token={item.coverToken} className="h-64 w-44 self-center" />
				{item.subtitle && <p className="break-words text-slate-600 dark:text-slate-400">{item.subtitle}</p>}
				{item.authors.length > 0 && (
					<p className="break-words">
						{item.authors.map((name, i) => {
							const key = item.people.find((p) => p.name === name)?.key;
							return (
								<span key={name}>
									{i > 0 && ", "}
									{key ? <AuthorLink name={name} authorKey={key} /> : name}
								</span>
							);
						})}
					</p>
				)}
				<dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
					{rows.map(([label, value]) => (
						<div key={label} className="contents">
							<dt className="text-slate-600 dark:text-slate-400">{label}</dt>
							<dd className="min-w-0 break-words">{value}</dd>
						</div>
					))}
				</dl>
				<div className="flex min-w-0 flex-col gap-1 text-sm">
					<h3 className="font-medium">{t("discover.panel.why")}</h3>
					<p className="break-words">{reasonText(item)}</p>
					{item.reason.because.length > 0 && (
						<p className="break-words text-slate-600 dark:text-slate-400">
							{t("discover.because", { books: item.reason.because.join(", ") })}
						</p>
					)}
				</div>
				<div className="flex min-w-0 flex-col gap-1 text-sm">
					<h3 className="font-medium">{t("book.description")}</h3>
					{item.description ? (
						<p className="whitespace-pre-line break-words">{item.description}</p>
					) : (
						<p className="text-slate-600 dark:text-slate-400">{t("discover.panel.noDescription")}</p>
					)}
				</div>
				<div className="flex flex-wrap gap-2">
					<button
						type="button"
						className={button("primary")}
						disabled={busy}
						onClick={() => wish.mutate(item, { onSuccess: onClose })}
					>
						{t("discover.wish")}
					</button>
					<button
						type="button"
						className={button("secondary")}
						disabled={busy}
						onClick={() => dismiss.mutate(item.id, { onSuccess: onClose })}
					>
						{t("discover.notInterested")}
					</button>
				</div>
				<FormError error={wish.error ?? dismiss.error} />
			</div>
		</Dialog>
	);
}
