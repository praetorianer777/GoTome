import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type ReactNode, useId, useState } from "react";
import { type CurrentUser, can } from "@/auth/session";
import {
	type BookDetail,
	type BookFile,
	type BookSummary,
	bookQuery,
} from "@/books/api";
import { Cover } from "@/books/cover";
import { AuthorLinks } from "@/books/name-links";
import { type ProgressState, progressQuery } from "@/books/progress";
import { FormError } from "@/components/form";
import {
	type DuplicatePair,
	type DuplicateParams,
	EVIDENCE_KINDS,
	type Evidence,
	type EvidenceKind,
	type PairState,
	type Relation,
	duplicatesQuery,
	overlapOf,
	useMergeBooks,
	useReplacePair,
	useSetPairState,
} from "@/duplicates/api";
import {
	Loading,
	PageHeader,
	Skeleton,
	button as uiButton,
} from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { MergeForm } from "@/duplicates/merge-form";
import { formatDate, formatDuration, formatSize } from "@/lib/format";
import { type Library, librariesQuery } from "@/libraries/api";

/** What the duplicates page shows, as the address carries it. */
export interface DuplicatesSearch {
	state?: PairState;
	/** Open pairs of which one book holds the other's text. */
	contained?: true;
	kind?: EvidenceKind;
	least?: number;
	library?: string;
}

type View = "open" | "contained" | "kept_both";

const VIEWS: { value: View; label: MessageKey; none: MessageKey }[] = [
	{ value: "open", label: "duplicates.state.open", none: "duplicates.none" },
	{
		value: "contained",
		label: "duplicates.state.contained",
		none: "duplicates.noneContained",
	},
	{
		value: "kept_both",
		label: "duplicates.state.keptBoth",
		none: "duplicates.noneKept",
	},
];

const SEARCH_OF: Record<View, Partial<DuplicatesSearch>> = {
	open: { state: undefined, contained: undefined },
	contained: { state: undefined, contained: true },
	kept_both: { state: "kept_both", contained: undefined },
};

const KIND_LABELS: Record<EvidenceKind, MessageKey> = {
	sha256: "duplicates.kind.sha256",
	content: "duplicates.kind.content",
	isbn: "duplicates.kind.isbn",
	title_author: "duplicates.kind.titleAuthor",
	overlap: "duplicates.kind.overlap",
};

const LEAST = [50, 75, 90, 100];

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
const button =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 aria-pressed:border-brand-strong aria-pressed:bg-brand-strong aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:border-brand dark:aria-pressed:bg-brand dark:aria-pressed:text-ink";

const percent = (score: number) => Math.round(score * 100);

/** Pairs of books that look like one, with why, for a person to decide. */
export function Duplicates({
	user,
	search,
	onSearch,
}: {
	user: CurrentUser;
	search: DuplicatesSearch;
	onSearch: (change: Partial<DuplicatesSearch>) => void;
}) {
	const libraries = useQuery(librariesQuery);
	const list = libraries.data ?? [];
	const view: View =
		search.state === "kept_both"
			? "kept_both"
			: search.contained
				? "contained"
				: "open";
	const params: DuplicateParams = {
		state: search.state ?? "open",
		contained:
			view === "kept_both" ? undefined : view === "contained" ? "true" : "false",
		kind: search.kind,
		least: search.least,
		library: list.some((l) => l.id === search.library)
			? search.library
			: undefined,
	};
	const pairs = useInfiniteQuery(duplicatesQuery(params));
	const shown = pairs.data?.pages.flatMap((p) => p.pairs) ?? [];
	const canAct = can(user, "metadata:edit");

	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t("duplicates.title")} intro={t("duplicates.intro")} />
			<div className="flex flex-wrap items-end gap-x-6 gap-y-3 text-sm">
				<fieldset className="flex items-center gap-2">
					<legend className="sr-only">{t("duplicates.state")}</legend>
					{VIEWS.map((v) => (
						<button
							key={v.value}
							type="button"
							aria-pressed={view === v.value}
							onClick={() => onSearch(SEARCH_OF[v.value])}
							className={button}
						>
							{t(v.label)}
						</button>
					))}
				</fieldset>
				<label className="flex max-w-full min-w-0 items-center gap-2">
					<span>{t("duplicates.kind")}</span>
					<select
						value={params.kind ?? ""}
						onChange={(e) =>
							onSearch({
								kind: (e.target.value || undefined) as EvidenceKind | undefined,
							})
						}
						className={`${control} min-w-0 max-w-full`}
					>
						<option value="">{t("duplicates.kind.all")}</option>
						{EVIDENCE_KINDS.map((k) => (
							<option key={k} value={k}>
								{t(KIND_LABELS[k])}
							</option>
						))}
					</select>
				</label>
				<label className="flex max-w-full min-w-0 items-center gap-2">
					<span>{t("duplicates.least")}</span>
					<select
						value={params.least ?? ""}
						onChange={(e) =>
							onSearch({
								least: e.target.value ? Number(e.target.value) : undefined,
							})
						}
						className={`${control} min-w-0 max-w-full`}
					>
						<option value="">{t("duplicates.least.any")}</option>
						{LEAST.map((n) => (
							<option key={n} value={n}>
								{t("duplicates.least.value", { percent: n })}
							</option>
						))}
					</select>
				</label>
				{list.length > 1 && (
					<label className="flex max-w-full min-w-0 items-center gap-2">
						<span>{t("library.choose")}</span>
						<select
							value={params.library ?? ""}
							onChange={(e) =>
								onSearch({ library: e.target.value || undefined })
							}
							className={`${control} min-w-0 max-w-full`}
						>
							<option value="">{t("library.all")}</option>
							{list.map((l) => (
								<option key={l.id} value={l.id}>
									{l.name}
								</option>
							))}
						</select>
					</label>
				)}
			</div>
			<section
				aria-label={t("duplicates.list")}
				className="flex flex-col gap-4"
			>
				<FormError error={pairs.error} />
				{pairs.isPending && <Loading />}
				{pairs.isSuccess && shown.length === 0 && (
					<p role="status" className="text-slate-600 dark:text-slate-400">
						{t(VIEWS.find((v) => v.value === view)?.none ?? "duplicates.none")}
					</p>
				)}
				{shown.length > 0 && (
					<ul className="flex flex-col gap-4">
						{shown.map((pair) => (
							<Pair
									key={pair.id}
									pair={pair}
									canAct={canAct}
									libraries={list}
								/>
						))}
					</ul>
				)}
				{pairs.hasNextPage && (
					<button
						type="button"
						disabled={pairs.isFetchingNextPage}
						onClick={() => pairs.fetchNextPage()}
						className={`${button} self-center px-4 py-2`}
					>
						{pairs.isFetchingNextPage ? t("loading") : t("duplicates.more")}
					</button>
				)}
			</section>
		</div>
	);
}

function byScore(evidence: Evidence[]): Evidence[] {
	return [...evidence].sort((x, y) => y.score - x.score);
}

/** One sentence for what the strongest evidence says about the pair. */
function statement(pair: DuplicatePair): string {
	const [a, b] = pair.books;
	const strongest = byScore(pair.evidence)[0];
	if (!a || !b || !strongest) return "";
	const names = { a: a.title, b: b.title };
	const [na, nb] = pair.narrators ?? [];
	if (na && nb) {
		return t("duplicates.says.narrator", {
			...names,
			narratorsA: na.join(", "),
			narratorsB: nb.join(", "),
		});
	}
	if (pair.formats) {
		return t(
			pair.formats.sameLibrary ? "duplicates.says.formats" : "duplicates.says.formatsApart",
			names,
		);
	}
	switch (strongest.kind) {
		case "sha256":
			return t("duplicates.says.sha256", names);
		case "content":
			return t("duplicates.says.content", names);
		case "isbn":
			return t("duplicates.says.isbn", { ...names, isbn: strongest.detail });
		case "title_author":
			return t("duplicates.says.titleAuthor", names);
		case "overlap": {
			const o = overlapOf(strongest.detail);
			if (!o) return "";
			// One book mostly inside the other says more than their overlap.
			if (Math.max(o.aInB, o.bInA) >= o.jaccard + 0.1) {
				return o.aInB >= o.bInA
					? t("duplicates.says.contained", {
							inner: a.title,
							outer: b.title,
							percent: percent(o.aInB),
						})
					: t("duplicates.says.contained", {
							inner: b.title,
							outer: a.title,
							percent: percent(o.bInA),
						});
			}
			return t("duplicates.says.overlap", {
				...names,
				percent: percent(o.jaccard),
			});
		}
	}
}

function evidenceText(e: Evidence, pair: DuplicatePair): string {
	switch (e.kind) {
		case "isbn":
			return t("duplicates.evidence.isbn", { isbn: e.detail });
		case "overlap": {
			const o = overlapOf(e.detail);
			const [a, b] = pair.books;
			return o && a && b
				? t("duplicates.evidence.overlap", {
						jaccard: percent(o.jaccard),
						a: a.title,
						b: b.title,
						aInB: percent(o.aInB),
						bInA: percent(o.bInA),
					})
				: t(KIND_LABELS.overlap);
		}
		default:
			return t(KIND_LABELS[e.kind]);
	}
}

function Pair({
	pair,
	canAct,
	libraries,
}: {
	pair: DuplicatePair;
	canAct: boolean;
	libraries: Library[];
}) {
	const [merging, setMerging] = useState(false);
	const oneLibrary = pair.books[0]?.libraryId === pair.books[1]?.libraryId;
	const setState = useSetPairState();
	const [relation, setRelation] = useState<Relation | "">("");
	const a = useQuery(bookQuery(pair.books[0]?.id ?? ""));
	const b = useQuery(bookQuery(pair.books[1]?.id ?? ""));
	const details = [a.data, b.data];
	const recordings = Boolean(pair.narrators);
	const decide = canAct && pair.state === "open" && oneLibrary && !recordings;
	const said = statement(pair);
	return (
		<li
			aria-label={said}
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
				<p className="min-w-0 flex-1 font-medium">{said}</p>
				<span className="text-sm tabular-nums text-slate-600 dark:text-slate-400">
					{t("duplicates.pairScore", { percent: percent(pair.score) })}
				</span>
			</div>
			<div className="grid gap-3 sm:grid-cols-2">
				{pair.books.map((book, i) => {
					const other = pair.books[1 - i];
					return (
						<BookCard
							key={book.id}
							book={book}
							detail={details[i]}
							otherDetail={details[1 - i]}
							other={other}
							library={libraries.find((l) => l.id === book.libraryId)}
							otherLibrary={libraries.find((l) => l.id === other?.libraryId)}
							pairId={pair.id}
							decide={decide}
						/>
					);
				})}
			</div>
			<FormError error={a.error ?? b.error} />
			<ul
				aria-label={t("duplicates.evidence")}
				className="flex flex-col gap-1 text-sm"
			>
				{byScore(pair.evidence).map((e) => (
					<li key={`${e.kind} ${e.detail}`} className="flex gap-2">
						<span className="min-w-0 flex-1">{evidenceText(e, pair)}</span>
						<span className="tabular-nums text-slate-600 dark:text-slate-400">
							{t("duplicates.score", { percent: percent(e.score) })}
						</span>
					</li>
				))}
			</ul>
			{canAct && (
				<div className="flex flex-wrap items-center gap-2">
					{pair.state === "open" && recordings && (
						<button
							type="button"
							disabled={setState.isPending}
							onClick={() =>
								setState.mutate({ id: pair.id, state: "kept_both", relation: "recording" })
							}
							className={button}
						>
							{t("duplicates.linkRecordings")}
						</button>
					)}
					{pair.state === "open" && pair.formats && !pair.formats.sameLibrary && (
						<button
							type="button"
							disabled={setState.isPending}
							onClick={() =>
								setState.mutate({ id: pair.id, state: "kept_both", relation: "format" })
							}
							className={button}
						>
							{t("duplicates.linkFormats")}
						</button>
					)}
					{pair.state === "open" && (
						<label className="flex min-w-0 max-w-full items-center gap-2 text-sm">
							<span>{t("duplicates.relation")}</span>
							<select
								value={relation}
								onChange={(e) => setRelation(e.target.value as Relation | "")}
								className={`${control} min-w-0`}
							>
								<option value="">{t("duplicates.relation.none")}</option>
								<option value="edition">{t("duplicates.relation.edition")}</option>
								<option value="translation">{t("duplicates.relation.translation")}</option>
								<option value="recording">{t("duplicates.relation.recording")}</option>
								<option value="format">{t("duplicates.relation.format")}</option>
								<option value="related">{t("duplicates.relation.related")}</option>
							</select>
						</label>
					)}
					<button
						type="button"
						disabled={setState.isPending}
						onClick={() =>
							setState.mutate({
								id: pair.id,
								state: pair.state === "kept_both" ? "open" : "kept_both",
								relation: pair.state === "open" && relation ? relation : undefined,
							})
						}
						className={button}
					>
						{t(
							pair.state === "kept_both"
								? "duplicates.reopen"
								: "duplicates.keepBoth",
						)}
					</button>
					{decide && (
						<button
							type="button"
							aria-expanded={merging}
							onClick={() => setMerging(!merging)}
							className={button}
						>
							{t(merging ? "duplicates.mergeClose" : "duplicates.merge")}
						</button>
					)}
				</div>
			)}
			{canAct && pair.state === "open" && !oneLibrary && (
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{t("duplicates.otherLibraries")}
				</p>
			)}
			<FormError error={setState.error} />
			{merging && <MergeForm pair={pair} onDone={() => setMerging(false)} />}
		</li>
	);
}

/** The name a book goes by in a pair, where both may have one title. */
function bookName(book: BookSummary): string {
	return book.formats.length > 0
		? `${book.title} (${book.formats.join(", ").toUpperCase()})`
		: book.title;
}

type Field = {
	label: MessageKey;
	value: (book: BookSummary, detail: BookDetail | undefined, library: Library | undefined) => string;
	/** Known only once the book's details have come. */
	ofDetail?: true;
};

const FIELDS: Field[] = [
	{
		label: "duplicates.field.authors",
		value: (book) => book.authors.join(", "),
	},
	{
		label: "duplicates.field.series",
		value: (book) =>
			book.series
				? `${book.series}${book.seriesIndex != null ? ` ${book.seriesIndex}` : ""}`
				: "",
	},
	{
		label: "duplicates.field.files",
		value: (_, detail) => (detail?.files ?? []).map(fileLine).join("\n"),
		ofDetail: true,
	},
	{
		label: "duplicates.field.ereader",
		value: (_, detail) =>
			[...new Set((detail?.files ?? []).flatMap((f) => f.ereader?.findings ?? []))]
				.map((f) => t(`filter.ereader.${f}`))
				.join(", "),
		ofDetail: true,
	},
	{
		label: "duplicates.field.library",
		value: (_, __, library) => library?.name ?? "",
	},
	{
		label: "duplicates.field.added",
		value: (book) => formatDate(book.addedAt),
	},
];

/** A file as a card lists it: format, size and pages or length. */
function fileLine(f: BookFile): string {
	const parts = [f.format.toUpperCase(), formatSize(f.size)];
	if (f.pageCount) parts.push(t("duplicates.filePages", { count: f.pageCount }));
	else if (f.durationMs) parts.push(formatDuration(f.durationMs));
	return `${parts.join(" · ")} — ${f.name}`;
}

const differs = "rounded bg-amber-50 dark:bg-amber-950";

/**
 * One book of a pair as a card, its values marked where the other book's
 * differ, with what keeping it does.
 */
function BookCard({
	book,
	detail,
	otherDetail,
	other,
	library,
	otherLibrary,
	pairId,
	decide,
}: {
	book: BookSummary;
	detail: BookDetail | undefined;
	otherDetail: BookDetail | undefined;
	other: BookSummary | undefined;
	library: Library | undefined;
	otherLibrary: Library | undefined;
	pairId: string;
	decide: boolean;
}) {
	const titleOf = (x: BookSummary) => [x.title, x.subtitle].filter(Boolean).join(": ");
	const titleDiffers = other !== undefined && titleOf(book) !== titleOf(other);
	return (
		<article
			aria-label={bookName(book)}
			className="flex min-w-0 flex-col gap-3 rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
		>
			<div className="flex gap-3">
				<Link
					to="/books/$bookId"
					params={{ bookId: book.id }}
					tabIndex={-1}
					aria-hidden="true"
					className="w-16 shrink-0"
				>
					<Cover book={book} size="small" />
				</Link>
				<div className={`flex min-w-0 flex-col ${titleDiffers ? differs : ""}`}>
					<Link
						to="/books/$bookId"
						params={{ bookId: book.id }}
						className="font-medium break-words hover:underline"
					>
						{book.title}
					</Link>
					{book.subtitle && (
						<span className="text-sm break-words">{book.subtitle}</span>
					)}
					{titleDiffers && <span className="sr-only">{t("duplicates.differs")}</span>}
				</div>
			</div>
			<dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm">
				{FIELDS.map((field) => {
					const value = field.value(book, detail, library);
					const loading = field.ofDetail === true && detail === undefined;
					const changed =
						other !== undefined &&
						(!field.ofDetail || (detail !== undefined && otherDetail !== undefined)) &&
						value !== field.value(other, otherDetail, otherLibrary);
					return (
						<FieldRow key={field.label} label={field.label} changed={changed}>
							{field.label === "duplicates.field.authors" && book.authors.length > 0 ? (
								<span className="[&_a]:inline-block [&_a]:min-h-6">
									<AuthorLinks book={book} />
								</span>
							) : field.label === "duplicates.field.files" && detail ? (
								<ul className="flex flex-col gap-0.5">
									{detail.files.map((f) => (
										<li key={f.id} className="break-words">
											{fileLine(f)}
										</li>
									))}
								</ul>
							) : loading ? (
								<Skeleton className="h-4 w-24" />
							) : (
								value || "—"
							)}
						</FieldRow>
					);
				})}
				<Reading book={book} detail={detail} otherDetail={otherDetail} />
			</dl>
			{decide && other && (
				<KeepThis pairId={pairId} keep={book} gone={other} library={library} />
			)}
		</article>
	);
}

function FieldRow({
	label,
	changed,
	children,
}: {
	label: MessageKey;
	changed: boolean;
	children: ReactNode;
}) {
	return (
		<>
			<dt className="text-slate-600 dark:text-slate-400">{t(label)}</dt>
			<dd className={`min-w-0 ${changed ? differs : ""}`}>
				{children}
				{changed && <span className="sr-only"> ({t("duplicates.differs")})</span>}
			</dd>
		</>
	);
}

/** The viewer's own state with the book, and how far they are in it. */
function readingOf(detail: BookDetail | undefined, progress: ProgressState | undefined): string {
	if (!detail) return "";
	const parts = [t(`status.${detail.reading.status}`)];
	const ebook = progress?.ebook?.fraction;
	const audio = progress?.audio?.fraction;
	if (ebook) parts.push(t("progress.ebook", { percent: Math.round(ebook * 100) }));
	if (audio) parts.push(t("progress.audio", { percent: Math.round(audio * 100) }));
	return parts.join(" · ");
}

function hasProgress(detail: BookDetail | undefined): boolean {
	return detail !== undefined && detail.reading.status !== "unread" && detail.reading.status !== "wishlist";
}

function Reading({
	book,
	detail,
	otherDetail,
}: {
	book: BookSummary;
	detail: BookDetail | undefined;
	otherDetail: BookDetail | undefined;
}) {
	const progress = useQuery({ ...progressQuery(book.id), enabled: hasProgress(detail) });
	const otherProgress = useQuery({
		...progressQuery(otherDetail?.id ?? ""),
		enabled: hasProgress(otherDetail),
	});
	const value = readingOf(detail, progress.data);
	const otherValue = readingOf(otherDetail, otherProgress.data);
	return (
		<FieldRow
			label="duplicates.field.reading"
			changed={detail !== undefined && otherDetail !== undefined && value !== otherValue}
		>
			{detail ? value : <Skeleton className="h-4 w-24" />}
		</FieldRow>
	);
}

/**
 * Keeps this book of the pair. Where GOtome writes to the library, the
 * other book's files go to the trash; where it does not, the other book is
 * merged into this one with its files. Either way everyone's reading and
 * shelves move to this one.
 */
function KeepThis({
	pairId,
	keep,
	gone,
	library,
}: {
	pairId: string;
	keep: BookSummary;
	gone: BookSummary;
	library: Library | undefined;
}) {
	const replace = useReplacePair();
	const merge = useMergeBooks();
	const trashes = library?.writable ?? true;
	const pending = replace.isPending || merge.isPending;
	const said = useId();
	return (
		<div className="mt-auto flex flex-col gap-2 border-t border-slate-200 pt-3 dark:border-slate-800">
			<button
				type="button"
				disabled={pending}
				aria-describedby={said}
				aria-label={t("duplicates.keepThis.named", { book: bookName(keep) })}
				onClick={() =>
					trashes
						? replace.mutate({ id: pairId, keep: keep.id })
						: merge.mutate({ into: keep.id, from: gone.id, take: [] })
				}
				className={`${uiButton("secondary", "md")} font-medium`}
			>
				{t("duplicates.keepThis")}
			</button>
			<p id={said} className="text-sm text-slate-600 dark:text-slate-400">
				{t(trashes ? "duplicates.keepThis.trash" : "duplicates.keepThis.merge", {
					gone: bookName(gone),
				})}
			</p>
			{trashes && library?.unwritableSince && (
				<p className="text-sm text-amber-800 dark:text-amber-200">
					{t("duplicates.keepThis.unwritable", {
						reason: library.unwritableReason ?? "",
					})}
				</p>
			)}
			<FormError error={replace.error ?? merge.error} />
		</div>
	);
}
