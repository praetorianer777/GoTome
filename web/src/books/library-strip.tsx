import { queryOptions, useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { api } from "@/api/client";
import type { BookSummary } from "@/books/api";
import { Cover } from "@/books/cover";
import { filterTree } from "@/books/filters";
import { MediumBadge } from "@/books/medium";
import { type ProgressState, progressQuery } from "@/books/progress";
import { Icon } from "@/components/shell";
import { card } from "@/components/ui";
import { t } from "@/i18n";
import { usePlayer } from "@/player/player";

const CONTINUE_MAX = 3;
const NEWEST_MAX = 12;
const FOLD_KEY = "gotome.strip.folded";

function storedFold(): boolean | undefined {
	try {
		const value = localStorage.getItem(FOLD_KEY);
		return value === null ? undefined : value === "1";
	} catch {
		return undefined;
	}
}

/** Folded at first on a phone, where the grid should stay in the first screen. */
function foldedAtFirst(): boolean {
	return window.matchMedia?.("(max-width: 639px)").matches ?? false;
}

function readingQuery(library?: string) {
	return queryOptions({
		queryKey: ["books", "strip", "reading", library ?? ""],
		queryFn: async () =>
			(
				await api.GET("/books", {
					params: {
						query: {
							library,
							filter: filterTree({ status: ["reading"] }),
							sort: "added",
							order: "desc",
							limit: 12,
						},
					},
				})
			).data?.books ?? [],
	});
}

function newestQuery(library?: string) {
	return queryOptions({
		queryKey: ["books", "strip", "new", library ?? ""],
		queryFn: async () =>
			(
				await api.GET("/books", {
					params: {
						query: { library, sort: "added", order: "desc", limit: NEWEST_MAX },
					},
				})
			).data?.books ?? [],
	});
}

/** When the person last moved in the book, in either medium. */
function lastRead(p?: ProgressState): number {
	return Math.max(
		p?.ebook ? Date.parse(p.ebook.updatedAt) : 0,
		p?.audio ? Date.parse(p.audio.updatedAt) : 0,
	);
}

/**
 * What a person comes to the library for: the books they are in the middle
 * of, and what is new. Only the parts that have something; folded to a line
 * that is remembered per browser.
 */
export function LibraryStrip({ library }: { library?: string }) {
	const reading = useQuery(readingQuery(library));
	const newest = useQuery(newestQuery(library));
	const progresses = useQueries({
		queries: (reading.data ?? []).map((b) => progressQuery(b.id)),
	});
	const [folded, setFolded] = useState(() => storedFold() ?? foldedAtFirst());

	const going = (reading.data ?? [])
		.map((book, i) => ({ book, progress: progresses[i]?.data }))
		.sort((a, b) => lastRead(b.progress) - lastRead(a.progress))
		.slice(0, CONTINUE_MAX);
	const fresh = newest.data ?? [];
	if (going.length === 0 && fresh.length === 0) return null;

	const toggle = () => {
		setFolded(!folded);
		try {
			localStorage.setItem(FOLD_KEY, folded ? "0" : "1");
		} catch {
			// The choice then lasts for the visit.
		}
	};

	return (
		<section
			aria-label={t("strip.title")}
			className={`${card} flex flex-col gap-4 p-3`}
		>
			<button
				type="button"
				aria-expanded={!folded}
				aria-controls="library-strip"
				onClick={toggle}
				className="flex min-h-6 items-center gap-2 rounded-lg text-left text-sm font-medium"
			>
				<Icon
					name="chevron"
					className={`size-4 transition-transform motion-reduce:transition-none ${folded ? "-rotate-90" : ""}`}
				/>
				{t("strip.title")}
			</button>
			<div id="library-strip" hidden={folded} className="flex flex-col gap-5">
				{going.length > 0 && (
					<section aria-labelledby="strip-continue" className="flex flex-col gap-2">
						<h2 id="strip-continue" className="text-lg font-medium">
							{t("strip.continue")}
						</h2>
						<ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
							{going.map(({ book, progress }) => (
								<li key={book.id}>
									<Going book={book} progress={progress} />
								</li>
							))}
						</ul>
					</section>
				)}
				{fresh.length > 0 && (
					<section aria-labelledby="strip-new" className="flex min-w-0 flex-col gap-2">
						<div className="flex flex-wrap items-baseline justify-between gap-x-4">
							<h2 id="strip-new" className="text-lg font-medium">
								{t("strip.new")}
							</h2>
							<Link
								to="/"
								search={{ sort: "added", library }}
								className="text-sm text-brand-strong underline underline-offset-4 dark:text-brand"
							>
								{t("strip.all")}
							</Link>
						</div>
						<ul className="flex gap-3 overflow-x-auto pb-2">
							{fresh.map((book) => (
								<li key={book.id} className="relative w-28 shrink-0">
									<MediumBadge formats={book.formats} className="top-1 right-1 scale-90" />
									<Link
										to="/books/$bookId"
										params={{ bookId: book.id }}
										className="flex flex-col gap-1"
									>
										<Cover book={book} size="small" />
										<span className="line-clamp-2 text-sm font-medium">
											{book.title}
										</span>
									</Link>
								</li>
							))}
						</ul>
					</section>
				)}
			</div>
		</section>
	);
}

/** A book in progress: its cover, how far, and one link to where the person was. */
function Going({
	book,
	progress,
}: {
	book: BookSummary;
	progress?: ProgressState;
}) {
	const player = usePlayer();
	const audio =
		progress?.audio &&
		(!progress.ebook ||
			Date.parse(progress.audio.updatedAt) >= Date.parse(progress.ebook.updatedAt));
	const here = audio ? progress?.audio : progress?.ebook;
	const percent = Math.round((here?.fraction ?? 0) * 100);
	const body = (
		<>
			<span className="w-14 shrink-0">
				<Cover book={book} size="small" className="shadow-candle" />
			</span>
			<span className="flex min-w-0 flex-1 flex-col gap-1">
				<span className="line-clamp-2 font-medium">{book.title}</span>
				{book.authors.length > 0 && (
					<span className="truncate text-sm text-slate-600 dark:text-slate-400">
						{book.authors.join(", ")}
					</span>
				)}
				<progress
					max={100}
					value={percent}
					aria-hidden="true"
					className="h-1.5 w-full"
				/>
				<span className="text-xs text-slate-600 dark:text-slate-400">
					{t("strip.progress", { percent })}
				</span>
			</span>
		</>
	);
	const cls = "flex gap-3 rounded-xl p-2 hover:bg-slate-100 dark:hover:bg-slate-800";
	if (audio) {
		return (
			<Link
				to="/books/$bookId/listen"
				params={{ bookId: book.id }}
				onClick={() => player.open(book.id)}
				className={cls}
			>
				{body}
			</Link>
		);
	}
	if (here?.fileId) {
		return (
			<Link
				to="/books/$bookId/read/$fileId"
				params={{ bookId: book.id, fileId: here.fileId }}
				className={cls}
			>
				{body}
			</Link>
		);
	}
	return (
		<Link to="/books/$bookId" params={{ bookId: book.id }} className={cls}>
			{body}
		</Link>
	);
}
