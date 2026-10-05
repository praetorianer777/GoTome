import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { type CurrentUser, can } from "@/auth/session";
import { type BookDetail, type BookSummary, bookQuery } from "@/books/api";
import { Cover } from "@/books/cover";
import { FormError } from "@/components/form";
import {
	type DuplicatePair,
	type DuplicateParams,
	EVIDENCE_KINDS,
	type Evidence,
	type EvidenceKind,
	type PairState,
	duplicatesQuery,
	overlapOf,
	useSetPairState,
} from "@/duplicates/api";
import { type MessageKey, t } from "@/i18n";
import { formatDate, formatLanguage, formatSize } from "@/lib/format";
import { librariesQuery } from "@/libraries/api";

/** What the duplicates page shows, as the address carries it. */
export interface DuplicatesSearch {
	state?: PairState;
	kind?: EvidenceKind;
	least?: number;
	library?: string;
}

const STATES: { value: PairState; label: MessageKey }[] = [
	{ value: "open", label: "duplicates.state.open" },
	{ value: "kept_both", label: "duplicates.state.keptBoth" },
];

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
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 aria-pressed:bg-slate-900 aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:bg-slate-100 dark:aria-pressed:text-slate-900";

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
	const params: DuplicateParams = {
		state: search.state ?? "open",
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
			<div>
				<h1 className="text-2xl font-semibold">{t("duplicates.title")}</h1>
				<p className="mt-1 max-w-2xl text-slate-600 dark:text-slate-400">
					{t("duplicates.intro")}
				</p>
			</div>
			<div className="flex flex-wrap items-end gap-x-6 gap-y-3 text-sm">
				<fieldset className="flex items-center gap-2">
					<legend className="sr-only">{t("duplicates.state")}</legend>
					{STATES.map((s) => (
						<button
							key={s.value}
							type="button"
							aria-pressed={params.state === s.value}
							onClick={() =>
								onSearch({ state: s.value === "open" ? undefined : s.value })
							}
							className={button}
						>
							{t(s.label)}
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
				{pairs.isPending && <p className="text-slate-500">{t("loading")}</p>}
				{pairs.isSuccess && shown.length === 0 && (
					<p role="status" className="text-slate-600 dark:text-slate-400">
						{t(
							params.state === "open"
								? "duplicates.none"
								: "duplicates.noneKept",
						)}
					</p>
				)}
				{shown.length > 0 && (
					<ul className="flex flex-col gap-4">
						{shown.map((pair) => (
							<Pair key={pair.id} pair={pair} canAct={canAct} />
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

function Pair({ pair, canAct }: { pair: DuplicatePair; canAct: boolean }) {
	const [comparing, setComparing] = useState(false);
	const setState = useSetPairState();
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
				{pair.books.map((book) => (
					<BookCard key={book.id} book={book} />
				))}
			</div>
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
			<div className="flex flex-wrap gap-2">
				<button
					type="button"
					aria-expanded={comparing}
					onClick={() => setComparing(!comparing)}
					className={button}
				>
					{t(comparing ? "duplicates.compareClose" : "duplicates.compare")}
				</button>
				{canAct && (
					<button
						type="button"
						disabled={setState.isPending}
						onClick={() =>
							setState.mutate({
								id: pair.id,
								state: pair.state === "kept_both" ? "open" : "kept_both",
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
				)}
			</div>
			<FormError error={setState.error} />
			{comparing && <Compare pair={pair} />}
		</li>
	);
}

function BookCard({ book }: { book: BookSummary }) {
	return (
		<Link
			to="/books/$bookId"
			params={{ bookId: book.id }}
			className="flex gap-3 rounded-md p-1 hover:bg-slate-50 dark:hover:bg-slate-900"
		>
			<span className="w-12 shrink-0">
				<Cover book={book} size="small" />
			</span>
			<span className="flex min-w-0 flex-col">
				<span className="font-medium">{book.title}</span>
				{book.authors.length > 0 && (
					<span className="text-sm text-slate-600 dark:text-slate-400">
						{book.authors.join(", ")}
					</span>
				)}
				<span className="text-sm uppercase text-slate-600 dark:text-slate-400">
					{book.formats.join(", ")}
				</span>
			</span>
		</Link>
	);
}

/** The two books side by side: their details, then their files. */
function Compare({ pair }: { pair: DuplicatePair }) {
	const a = useQuery(bookQuery(pair.books[0]?.id ?? ""));
	const b = useQuery(bookQuery(pair.books[1]?.id ?? ""));
	if (!a.data || !b.data) {
		return (
			<>
				{(a.isPending || b.isPending) && (
					<p className="text-slate-500">{t("loading")}</p>
				)}
				<FormError error={a.error ?? b.error} />
			</>
		);
	}
	const rows: { label: MessageKey; value: (book: BookDetail) => string }[] = [
		{
			label: "duplicates.field.title",
			value: (x) => [x.title, x.subtitle].filter(Boolean).join(": "),
		},
		{
			label: "duplicates.field.authors",
			value: (x) =>
				x.contributors
					.filter((c) => c.role === "author")
					.map((c) => c.name)
					.join(", "),
		},
		{
			label: "duplicates.field.series",
			value: (x) =>
				x.series
					? `${x.series}${x.seriesIndex != null ? ` ${x.seriesIndex}` : ""}`
					: "",
		},
		{ label: "duplicates.field.published", value: (x) => x.published ?? "" },
		{ label: "duplicates.field.publisher", value: (x) => x.publisher ?? "" },
		{
			label: "duplicates.field.language",
			value: (x) => (x.language ? formatLanguage(x.language) : ""),
		},
		{
			label: "duplicates.field.pages",
			value: (x) => (x.pageCount ? String(x.pageCount) : ""),
		},
		{
			label: "duplicates.field.identifiers",
			value: (x) =>
				x.identifiers
					.map((i) => `${i.type.toUpperCase()} ${i.value}`)
					.join(", "),
		},
		{
			label: "duplicates.field.files",
			value: (x) =>
				x.files
					.map(
						(f) =>
							`${f.format.toUpperCase()} ${f.name} (${formatSize(f.size)})`,
					)
					.join("; "),
		},
		{ label: "duplicates.field.added", value: (x) => formatDate(x.addedAt) },
	];
	const books = [a.data, b.data];
	return (
		<div className="overflow-x-auto">
			<table className="w-full text-sm">
				<caption className="sr-only">{t("duplicates.compare")}</caption>
				<thead>
					<tr>
						<td />
						{books.map((x) => (
							<th
								key={x.id}
								scope="col"
								className="px-2 py-1 text-left font-medium"
							>
								{x.title}
							</th>
						))}
					</tr>
				</thead>
				<tbody className="divide-y divide-slate-200 dark:divide-slate-800">
					{rows.map((row) => {
						const values = books.map(row.value);
						const differ = values[0] !== values[1];
						return (
							<tr
								key={row.label}
								className={differ ? "bg-amber-50 dark:bg-amber-950" : undefined}
							>
								<th
									scope="row"
									className="px-2 py-1 text-left font-normal text-slate-600 dark:text-slate-400"
								>
									{t(row.label)}
								</th>
								{values.map((v, i) => (
									<td key={books[i]?.id} className="break-all px-2 py-1">
										{v || "—"}
									</td>
								))}
							</tr>
						);
					})}
				</tbody>
			</table>
		</div>
	);
}
