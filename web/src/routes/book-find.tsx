import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type ReactNode, useState } from "react";
import { ApiError } from "@/api/client";
import { type BookDetail, bookQuery } from "@/books/api";
import { Cover } from "@/books/cover";
import {
	type Candidate,
	type CandidateApply,
	type Lockable,
	candidateCoverUrl,
	candidatesQuery,
	useApplyCandidate,
} from "@/books/edit";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { formatLanguage } from "@/lib/format";

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
				{book.isPending && <p className="text-slate-500">{t("loading")}</p>}
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

/** One field a candidate would change. */
interface Change {
	field: Lockable;
	label: MessageKey;
	mine: ReactNode;
	theirs: ReactNode;
	take: CandidateApply;
}

/** What taking the candidate would change, field by field. */
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
		if (theirs !== "" && theirs !== mine) {
			out.push({
				field,
				label,
				mine: mine ? shown(mine) : t("find.empty"),
				theirs: shown(theirs),
				take,
			});
		}
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
	if (added.length > 0) {
		const own = book.identifiers
			.filter((i) => !i.fromFile)
			.map((i) => ({ type: i.type, value: i.value }));
		out.push({
			field: "identifiers",
			label: "edit.field.identifiers",
			mine:
				book.identifiers
					.map((i) => `${i.type.toUpperCase()} ${i.value}`)
					.join(", ") || t("find.empty"),
			theirs: `${t("find.addIdentifiers")}: ${added.map((i) => `${i.type.toUpperCase()} ${i.value}`).join(", ")}`,
			take: { provider: c.provider, identifiers: [...own, ...added] },
		});
	}
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
	if (c.coverToken) {
		out.unshift({
			field: "cover",
			label: "edit.field.cover",
			mine: (
				<div className="w-16">
					<Cover book={book} size="small" />
				</div>
			),
			theirs: (
				<img
					src={candidateCoverUrl(c.coverToken)}
					alt=""
					className="w-16 rounded-md bg-slate-200 shadow-sm dark:bg-slate-800"
				/>
			),
			take: { provider: c.provider, coverToken: c.coverToken },
		});
	}
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
			<div>
				<h1 className="text-2xl font-semibold">
					{t("find.title", { title: book.title })}
				</h1>
				<p className="mt-1 max-w-2xl text-slate-600 dark:text-slate-400">
					{t("find.intro")}
				</p>
			</div>
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
							<Compare
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

function Compare({
	book,
	candidate,
}: {
	book: BookDetail;
	candidate: Candidate;
}) {
	const navigate = useNavigate();
	const apply = useApplyCandidate(book.id);
	const rows = changes(book, candidate);
	const locked = (field: Lockable) => book.fields[field]?.locked ?? false;
	const [take, setTake] = useState(
		() => new Set(rows.filter((r) => !locked(r.field)).map((r) => r.field)),
	);

	if (rows.length === 0) {
		return (
			<p className="text-slate-600 dark:text-slate-400">{t("find.nothing")}</p>
		);
	}
	function submit() {
		const body: CandidateApply = { provider: candidate.provider };
		for (const row of rows) {
			if (take.has(row.field)) {
				Object.assign(body, row.take);
			}
		}
		apply.mutate(body, {
			onSuccess: () =>
				navigate({ to: "/books/$bookId", params: { bookId: book.id } }),
		});
	}
	return (
		<div className="flex flex-col gap-4">
			<table className="w-full text-left text-sm">
				<thead>
					<tr className="border-b border-slate-200 dark:border-slate-800">
						<th className="py-2 pr-2 font-medium">{t("find.field")}</th>
						<th className="py-2 pr-2 font-medium">{t("find.current")}</th>
						<th className="py-2 font-medium">
							{t("find.theirs", { provider: candidate.provider })}
						</th>
					</tr>
				</thead>
				<tbody>
					{rows.map((row) => (
						<tr
							key={row.field}
							className="border-b border-slate-100 align-top dark:border-slate-800/60"
						>
							<th scope="row" className="py-2 pr-2 font-normal">
								<label className="flex items-start gap-2">
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
									<span>
										{t(row.label)}
										{locked(row.field) && (
											<span className="block text-xs text-slate-500">
												{t("find.locked")}
											</span>
										)}
									</span>
								</label>
							</th>
							<td className="break-words py-2 pr-2 text-slate-600 dark:text-slate-400">
								{row.mine}
							</td>
							<td className="break-words py-2">{row.theirs}</td>
						</tr>
					))}
				</tbody>
			</table>
			<FormError error={apply.error} />
			<button
				type="button"
				disabled={apply.isPending || take.size === 0}
				onClick={submit}
				className="self-start rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 disabled:opacity-60"
			>
				{t("find.apply")}
			</button>
		</div>
	);
}
