import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { ApiError } from "@/api/client";
import { type BookDetail, bookQuery } from "@/books/api";
import { Cover } from "@/books/cover";
import {
	type Candidate,
	type CandidateApply,
	type FieldSuggestion,
	type Lockable,
	candidateCoverUrl,
	candidatesQuery,
	useApplyCandidate,
} from "@/books/edit";
import { FormError } from "@/components/form";
import { PageHeader, Loading } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { formatLanguage } from "@/lib/format";
import { typing } from "@/lib/keys";

export function FindDetails({ id }: { id: string }) {
	const book = useQuery(bookQuery(id));
	if (book.error instanceof ApiError && book.error.status === 404) {
		return (
			<p className="text-slate-600 dark:text-slate-400">{t("book.notFound")}</p>
		);
	}
	if (!book.data) {
		return (
			<div className="flex flex-col gap-4">
				{book.isPending && <Loading />}
				<FormError error={book.error} />
			</div>
		);
	}
	return <FindPage book={book.data} />;
}

function names(people: { name: string }[]): string {
	return people.map((p) => p.name).join(", ");
}

function seriesText(series?: string, index?: number): string {
	if (!series) return "";
	return index != null ? `${series} #${index}` : series;
}

/**
 * How a field stands: the same on both sides, different (the candidate has
 * a value to take), or one without a value to compare.
 */
type Standing = "same" | "differs" | "theirsEmpty" | "bothEmpty";

/** One field of the book beside the candidate's. */
interface Change {
	field: Lockable;
	label: MessageKey;
	mine: ReactNode;
	theirs: ReactNode;
	standing: Standing;
	/** What taking the field sends; only a field that differs has one. */
	take?: CandidateApply;
	/** The book has a value here that taking would replace. */
	replaces: boolean;
}

const REASONS: Record<FieldSuggestion["reason"], MessageKey> = {
	empty: "find.reason.empty",
	filename: "find.reason.filename",
	sameEdition: "find.reason.sameEdition",
	adds: "find.reason.adds",
	locked: "find.reason.locked",
	manual: "find.reason.manual",
	language: "find.reason.language",
	file: "find.reason.file",
	other: "find.reason.other",
};

const blank = () => (
	<span className="italic text-slate-500 dark:text-slate-400">
		{t("find.blank")}
	</span>
);

/** Every field of the book beside the candidate's, and how each stands. */
function changes(book: BookDetail, c: Candidate): Change[] {
	const out: Change[] = [];
	const text = (
		field: Lockable,
		label: MessageKey,
		mine: string,
		theirs: string,
		take: CandidateApply,
		shown = (s: string) => s,
	) => {
		const standing: Standing =
			theirs === ""
				? mine === ""
					? "bothEmpty"
					: "theirsEmpty"
				: theirs === mine
					? "same"
					: "differs";
		out.push({
			field,
			label,
			mine: mine ? shown(mine) : blank(),
			theirs: theirs ? shown(theirs) : blank(),
			standing,
			take: standing === "differs" ? take : undefined,
			replaces: standing === "differs" && mine !== "",
		});
	};
	text("title", "edit.field.title", book.title, c.title, {
		provider: c.provider,
		title: c.title,
	});
	text(
		"subtitle",
		"edit.field.subtitle",
		book.subtitle ?? "",
		c.subtitle ?? "",
		{ provider: c.provider, subtitle: c.subtitle },
	);
	text(
		"contributors",
		"edit.field.contributors",
		names(book.contributors),
		names(c.contributors),
		{
			provider: c.provider,
			contributors: c.contributors,
		},
	);
	text(
		"series",
		"edit.field.series",
		seriesText(book.series, book.seriesIndex),
		seriesText(c.series, c.seriesIndex),
		{
			provider: c.provider,
			series: { name: c.series ?? "", index: c.seriesIndex },
		},
	);
	text(
		"publisher",
		"edit.field.publisher",
		book.publisher ?? "",
		c.publisher ?? "",
		{ provider: c.provider, publisher: c.publisher },
	);
	text(
		"published",
		"edit.field.published",
		book.published ?? "",
		c.published ?? "",
		{ provider: c.provider, published: c.published },
	);
	text(
		"language",
		"edit.field.language",
		book.language ?? "",
		c.language ?? "",
		{ provider: c.provider, language: c.language },
		formatLanguage,
	);
	text(
		"pageCount",
		"edit.field.pageCount",
		book.pageCount ? String(book.pageCount) : "",
		c.pageCount ? String(c.pageCount) : "",
		{
			provider: c.provider,
			pageCount: c.pageCount,
		},
	);
	text("tags", "edit.field.tags", book.tags.join(", "), c.tags.join(", "), {
		provider: c.provider,
		tags: c.tags,
	});
	// Identifiers are added to the book's own, never put in their place.
	const have = new Set(book.identifiers.map((i) => `${i.type}:${i.value}`));
	const added = c.identifiers.filter((i) => !have.has(`${i.type}:${i.value}`));
	const own = book.identifiers
		.filter((i) => !i.fromFile)
		.map((i) => ({ type: i.type, value: i.value }));
	const written = (list: { type: string; value: string }[]) =>
		list.map((i) => `${i.type.toUpperCase()} ${i.value}`).join(", ");
	out.push({
		field: "identifiers",
		label: "edit.field.identifiers",
		mine: book.identifiers.length > 0 ? written(book.identifiers) : blank(),
		theirs: c.identifiers.length > 0 ? written(c.identifiers) : blank(),
		standing:
			c.identifiers.length === 0
				? book.identifiers.length === 0
					? "bothEmpty"
					: "theirsEmpty"
				: added.length > 0
					? "differs"
					: "same",
		take:
			added.length > 0
				? { provider: c.provider, identifiers: [...own, ...added] }
				: undefined,
		replaces: false,
	});
	text(
		"description",
		"edit.field.description",
		book.description ?? "",
		c.description ?? "",
		{
			provider: c.provider,
			description: c.description,
		},
		(s) => (s.length > 400 ? `${s.slice(0, 400)}…` : s),
	);
	out.unshift({
		field: "cover",
		label: "edit.field.cover",
		mine: (
			<div className="w-16">
				<Cover book={book} size="small" />
			</div>
		),
		theirs: c.coverToken ? (
			<img
				src={candidateCoverUrl(c.coverToken)}
				alt=""
				className="w-16 rounded-md bg-slate-200 shadow-sm dark:bg-slate-800"
			/>
		) : (
			blank()
		),
		standing: c.coverToken
			? "differs"
			: book.coverKey
				? "theirsEmpty"
				: "bothEmpty",
		take: c.coverToken
			? { provider: c.provider, coverToken: c.coverToken }
			: undefined,
		replaces: Boolean(c.coverToken && book.coverKey),
	});
	return out;
}

function FindPage({ book }: { book: BookDetail }) {
	const found = useQuery(candidatesQuery(book.id));
	const [picked, setPicked] = useState<Candidate>();

	return (
		<div className="flex flex-col gap-6">
			<Link
				to="/books/$bookId"
				params={{ bookId: book.id }}
				className="self-start text-sm text-brand-strong underline underline-offset-4 dark:text-brand"
			>
				{t("find.back")}
			</Link>
			<PageHeader
				title={t("find.title", { title: book.title })}
				intro={t("find.intro")}
			/>
			{found.isPending && (
				<p className="text-slate-500">{t("find.searching")}</p>
			)}
			<FormError error={found.error} />
			{found.data?.failures.map((f) => (
				<p
					key={f.provider}
					className="text-sm text-amber-800 dark:text-amber-300"
				>
					{t("find.failed", { provider: f.provider, message: f.message })}
				</p>
			))}
			{found.data?.candidates.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("find.none")}</p>
			)}
			{found.data && found.data.candidates.length > 0 && (
				<div className="grid gap-6 lg:grid-cols-[minmax(0,22rem)_1fr]">
					<ul aria-label={t("find.list")} className="flex flex-col gap-2">
						{found.data.candidates.map((c) => (
							<li key={`${c.provider}:${c.id}`}>
								<button
									type="button"
									aria-pressed={picked === c}
									onClick={() => setPicked(c)}
									className="flex w-full gap-3 rounded-lg border border-slate-200 p-2 text-left hover:bg-slate-50 aria-pressed:border-brand-strong aria-pressed:bg-sky-50 dark:border-slate-800 dark:hover:bg-slate-900 dark:aria-pressed:border-brand dark:aria-pressed:bg-slate-900"
								>
									<div className="w-12 shrink-0">
										{c.coverToken ? (
											<img
												src={candidateCoverUrl(c.coverToken)}
												alt=""
												loading="lazy"
												className="w-12 rounded bg-slate-200 dark:bg-slate-800"
											/>
										) : (
											<Cover book={{ id: c.id, title: c.title }} size="small" />
										)}
									</div>
									<span className="flex min-w-0 flex-col text-sm">
										<span className="font-medium">{c.title}</span>
										{c.contributors.length > 0 && (
											<span>{names(c.contributors)}</span>
										)}
										<span className="text-slate-500 dark:text-slate-400">
											{[
												c.published,
												c.publisher,
												t("find.from", { provider: c.provider }),
											]
												.filter(Boolean)
												.join(" · ")}
										</span>
										<span className="text-slate-500 dark:text-slate-400">
											{c.score >= 1
												? t("find.sameISBN")
												: t("find.match", {
														percent: Math.round(c.score * 100),
													})}
										</span>
									</span>
								</button>
							</li>
						))}
					</ul>
					<section aria-label={t("find.compare")}>
						{picked ? (
							<TakeFromCandidate
								key={`${picked.provider}:${picked.id}`}
								book={book}
								candidate={picked}
							/>
						) : (
							<p className="text-slate-600 dark:text-slate-400">
								{t("find.pick")}
							</p>
						)}
					</section>
				</div>
			)}
		</div>
	);
}

/**
 * What taking a candidate would change, a tick per field, and the button
 * that takes the ticked ones. The fields the server suggests are ticked to
 * begin with, and each says why; locked fields cannot be ticked. With keys
 * set, "a" takes too, unless a text field has the focus.
 */
export function Compare({
	book,
	candidate,
	busy,
	error,
	onTake,
	keys = false,
	children,
}: {
	book: BookDetail;
	candidate: Candidate;
	busy: boolean;
	error: unknown;
	onTake: (body: CandidateApply) => void;
	keys?: boolean;
	children?: ReactNode;
}) {
	const rows = changes(book, candidate);
	const takeable = rows.some((r) => r.take);
	const locked = (field: Lockable) => book.fields[field]?.locked ?? false;
	const suggestion = (field: Lockable) => candidate.fields?.[field];
	const [take, setTake] = useState(
		() =>
			new Set(
				rows
					.filter(
						(r) => r.take && !locked(r.field) && suggestion(r.field)?.suggested,
					)
					.map((r) => r.field),
			),
	);
	function submit() {
		const body: CandidateApply = { provider: candidate.provider };
		for (const row of rows) {
			if (row.take && take.has(row.field)) {
				Object.assign(body, row.take);
			}
		}
		onTake(body);
	}
	const latest = useRef(submit);
	latest.current = submit;
	useEffect(() => {
		if (!keys) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "a" && !typing(e) && !e.ctrlKey && !e.metaKey && !e.altKey) {
				e.preventDefault();
				latest.current();
			}
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, [keys]);

	const standings: Record<Standing, MessageKey> = {
		same: "find.same",
		differs: "find.differs",
		theirsEmpty: "find.theirsEmpty",
		bothEmpty: "find.bothEmpty",
	};
	return (
		<div className="flex flex-col gap-4">
			{!takeable && (
				<p className="text-slate-600 dark:text-slate-400">{t("find.nothing")}</p>
			)}
			<table className="block w-full text-left text-sm sm:table">
				<thead className="hidden sm:table-header-group">
					<tr className="border-b border-slate-200 dark:border-slate-800">
						<th className="py-2 pr-2 font-medium">{t("find.field")}</th>
						<th className="py-2 pr-2 font-medium">{t("find.current")}</th>
						<th className="py-2 font-medium">
							{t("find.theirs", { provider: candidate.provider })}
						</th>
					</tr>
				</thead>
				<tbody className="block sm:table-row-group">
					{rows.map((row) => {
						const reason = locked(row.field)
							? "locked"
							: row.take
								? suggestion(row.field)?.reason
								: undefined;
						return (
							<tr
								key={row.field}
								data-standing={row.standing}
								className={`block border-b border-slate-100 py-2 align-top sm:table-row sm:py-0 dark:border-slate-800/60 ${row.standing === "differs" ? "bg-amber-50 dark:bg-amber-950" : ""}`}
							>
								<th scope="row" className="block py-1 pr-2 font-normal sm:table-cell sm:py-2">
									<label className="flex items-start gap-2">
										{row.take ? (
											<input
												type="checkbox"
												checked={take.has(row.field)}
												disabled={locked(row.field)}
												onChange={(e) => {
													const next = new Set(take);
													if (e.target.checked) next.add(row.field);
													else next.delete(row.field);
													setTake(next);
												}}
												aria-label={t("find.take", { field: t(row.label) })}
												className="mt-0.5"
											/>
										) : (
											<span aria-hidden="true" className="w-4 shrink-0" />
										)}
										<span>
											<span className={row.standing === "differs" ? "font-medium" : ""}>
												{t(row.label)}
											</span>
											<span className="block text-xs text-slate-500 dark:text-slate-400">
												{t(standings[row.standing])}
											</span>
											{reason && (
												<span className="block text-xs text-slate-500 dark:text-slate-400">
													{t(REASONS[reason])}
												</span>
											)}
										</span>
									</label>
								</th>
								<td className="block break-words py-1 pr-2 text-slate-600 sm:table-cell sm:py-2 dark:text-slate-400">
									<span className="block text-xs font-medium sm:hidden">
										{t("find.current")}
									</span>
									{row.mine}
								</td>
								<td className="block break-words py-1 sm:table-cell sm:py-2">
									<span className="block text-xs font-medium text-slate-600 sm:hidden dark:text-slate-400">
										{t("find.theirs", { provider: candidate.provider })}
									</span>
									{row.theirs}
								</td>
							</tr>
						);
					})}
				</tbody>
			</table>
			<FormError error={error} />
			<div className="flex flex-wrap items-center gap-2">
				<button
					type="button"
					disabled={busy || take.size === 0}
					onClick={submit}
					className="rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 dark:bg-brand dark:text-ink dark:hover:bg-sky-300 disabled:opacity-60"
				>
					{t("find.apply")}
				</button>
				{children}
			</div>
		</div>
	);
}

function TakeFromCandidate({ book, candidate }: { book: BookDetail; candidate: Candidate }) {
	const navigate = useNavigate();
	const apply = useApplyCandidate(book.id);
	return (
		<Compare
			book={book}
			candidate={candidate}
			busy={apply.isPending}
			error={apply.error}
			onTake={(body) =>
				apply.mutate(body, {
					onSuccess: () => navigate({ to: "/books/$bookId", params: { bookId: book.id } }),
				})
			}
		/>
	);
}
