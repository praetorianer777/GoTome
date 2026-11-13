import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { bookQuery } from "@/books/api";
import { BookPanelLayout, FoldedText } from "@/books/book-panel";
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
import { formatCount, formatPercent } from "@/lib/format";

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

/** Genres suggested for books, for a person to accept or reject. */
export function Genres({
	genre,
	onGenre,
	book,
	onBook,
}: {
	genre?: Genre;
	onGenre: (genre?: Genre) => void;
	/** The book open beside the list, as the address has it. */
	book?: string;
	onBook: (id: string | undefined, replace?: boolean) => void;
}) {
	const list = useInfiniteQuery(genreSuggestionsQuery(genre));
	const pages = list.data?.pages ?? [];
	const waiting = pages[0]?.waiting ?? [];
	const suggestions = pages.flatMap((p) => p?.suggestions ?? []);
	const total = waiting.reduce((sum, w) => sum + w.waiting, 0);

	const ofOpen = book ? suggestions.filter((s) => s.book.id === book) : [];
	return (
		<BookPanelLayout
			bookId={book}
			onClose={() => onBook(undefined, true)}
			panel={
				ofOpen.length > 0 && (
					<ul className="flex flex-col gap-3" aria-label={t("genres.suggested")}>
						{ofOpen.map((s) => (
							<li
								key={s.genre}
								className="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 dark:border-slate-800"
							>
								<Decision suggestion={s} />
							</li>
						))}
					</ul>
				)
			}
		>
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
					<Suggestion
						key={`${s.book.id}:${s.genre}`}
						suggestion={s}
						onOpen={() => onBook(s.book.id)}
					/>
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
		</BookPanelLayout>
	);
}

function Suggestion({
	suggestion: s,
	onOpen,
}: {
	suggestion: GenreSuggestion;
	onOpen: () => void;
}) {
	const detail = useQuery({ ...bookQuery(s.book.id), retry: false });
	return (
		<li className="flex flex-col gap-2 rounded-lg border border-slate-200 p-4 dark:border-slate-800">
			<div className="flex flex-col gap-1">
				<button
					type="button"
					onClick={onOpen}
					className="self-start text-left font-medium hover:underline"
				>
					{s.book.title}
				</button>
				{s.book.authors.length > 0 && (
					<span className="text-sm text-slate-600 dark:text-slate-400">
						{s.book.authors.join(", ")}
					</span>
				)}
			</div>
			{detail.data?.description && <FoldedText text={detail.data.description} />}
			{detail.data && detail.data.tags.length > 0 && (
				<p className="text-xs text-slate-500 dark:text-slate-400">
					{detail.data.tags.join(" · ")}
				</p>
			)}
			<Decision suggestion={s} />
		</li>
	);
}

/** What made the ensemble suggest a genre: each input's say, and the words. */
function Reasons({
	reasons,
}: {
	reasons: NonNullable<GenreSuggestion["reasons"]>;
}) {
	const inputs = reasons.inputs
		.map((i) =>
			t(`genres.input.${i.input}`, { score: formatPercent(i.score) }),
		)
		.join(" · ");
	return (
		<div className="text-xs text-slate-500 dark:text-slate-400">
			{inputs && <p>{t("genres.why.inputs", { inputs })}</p>}
			{reasons.words.length > 0 && (
				<p>{t("genres.why.words", { words: reasons.words.join(", ") })}</p>
			)}
		</div>
	);
}

/** The suggested genre, why it is suggested, and the buttons that decide it. */
function Decision({ suggestion: s }: { suggestion: GenreSuggestion }) {
	const decide = useDecideGenre();
	const name = genreName(s.genre);
	return (
		<>
			<p>
				<span className="font-medium">{name}</span>{" "}
				<span className="text-sm text-slate-500">
					{s.method === "ensemble"
						? t("genres.byEnsemble", { score: formatPercent(s.score) })
						: t(
								s.method === "neighbours"
									? "genres.byNeighbours"
									: "genres.byVector",
							)}
				</span>
			</p>
			{s.reasons && <Reasons reasons={s.reasons} />}
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
		</>
	);
}
