import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	GENRES,
	type Genre,
	type GenreSuggestion,
	genreSuggestionsQuery,
	isGenre,
	useDecideGenre,
} from "@/books/genres";
import { FormError } from "@/components/form";
import { Loading, PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { formatCount } from "@/lib/format";

const button =
	"rounded-md bg-brand-strong px-3 py-2 text-sm font-medium text-white hover:bg-sky-800 disabled:opacity-60 dark:bg-brand dark:text-ink dark:hover:bg-sky-300";
const quietButton =
	"rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";
const pill =
	"rounded-full border border-slate-300 px-3 py-1 hover:bg-slate-100 aria-pressed:border-brand-strong aria-pressed:bg-brand-strong aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:border-brand dark:aria-pressed:bg-brand dark:aria-pressed:text-ink";

/** A genre's name as the reader's language has it. */
export function genreName(genre: string): string {
	return isGenre(genre) ? t(`genre.${genre}`) : genre;
}

/** Genres suggested from the books' vectors, for a person to accept or reject. */
export function Genres({
	genre,
	onGenre,
}: {
	genre?: Genre;
	onGenre: (genre?: Genre) => void;
}) {
	const list = useInfiniteQuery(genreSuggestionsQuery(genre));
	const pages = list.data?.pages ?? [];
	const waiting = pages[0]?.waiting ?? [];
	const suggestions = pages.flatMap((p) => p?.suggestions ?? []);
	const total = waiting.reduce((sum, w) => sum + w.waiting, 0);

	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t("genres.title")} intro={t("genres.intro")} />
			{(waiting.length > 0 || genre) && (
				<fieldset className="flex flex-wrap gap-2 text-sm">
					<legend className="sr-only">{t("genres.pick")}</legend>
					<button
						type="button"
						aria-pressed={!genre}
						className={pill}
						onClick={() => onGenre()}
					>
						{t("genres.all", { count: formatCount(total) })}
					</button>
					{GENRES.filter(
						(g) => g === genre || waiting.some((w) => w.genre === g),
					).map((g) => (
						<button
							key={g}
							type="button"
							aria-pressed={g === genre}
							onClick={() => onGenre(g)}
							className={pill}
						>
							{t("genres.waiting", {
								genre: genreName(g),
								count: formatCount(
									waiting.find((w) => w.genre === g)?.waiting ?? 0,
								),
							})}
						</button>
					))}
				</fieldset>
			)}
			{list.isPending && <Loading />}
			<FormError error={list.error} />
			{list.data && suggestions.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("genres.none")}</p>
			)}
			<ul className="flex flex-col gap-3">
				{suggestions.map((s) => (
					<Suggestion key={`${s.book.id}:${s.genre}`} suggestion={s} />
				))}
			</ul>
			{list.hasNextPage && (
				<button
					type="button"
					className={`${quietButton} self-start`}
					disabled={list.isFetchingNextPage}
					onClick={() => list.fetchNextPage()}
				>
					{list.isFetchingNextPage ? t("loading") : t("genres.more")}
				</button>
			)}
		</div>
	);
}

function Suggestion({ suggestion: s }: { suggestion: GenreSuggestion }) {
	const decide = useDecideGenre();
	const name = genreName(s.genre);
	return (
		<li className="flex flex-col gap-2 rounded-lg border border-slate-200 p-4 dark:border-slate-800">
			<div className="flex flex-col gap-1">
				<Link
					to="/books/$bookId"
					params={{ bookId: s.book.id }}
					className="font-medium hover:underline"
				>
					{s.book.title}
				</Link>
				{s.book.authors.length > 0 && (
					<span className="text-sm text-slate-600 dark:text-slate-400">
						{s.book.authors.join(", ")}
					</span>
				)}
			</div>
			<p>
				<span className="font-medium">{name}</span>{" "}
				<span className="text-sm text-slate-500">
					{t(
						s.method === "neighbours"
							? "genres.byNeighbours"
							: "genres.byVector",
					)}
				</span>
			</p>
			<FormError error={decide.error} />
			<div className="flex flex-wrap gap-2">
				<button
					type="button"
					className={button}
					disabled={decide.isPending}
					onClick={() =>
						decide.mutate({
							bookId: s.book.id,
							genre: s.genre,
							state: "accepted",
						})
					}
				>
					{t("genres.accept", { genre: name })}
				</button>
				<button
					type="button"
					className={quietButton}
					disabled={decide.isPending}
					onClick={() =>
						decide.mutate({
							bookId: s.book.id,
							genre: s.genre,
							state: "rejected",
						})
					}
				>
					{t("genres.reject")}
				</button>
			</div>
		</li>
	);
}
