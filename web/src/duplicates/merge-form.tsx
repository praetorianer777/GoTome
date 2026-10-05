import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { type BookDetail, bookQuery } from "@/books/api";
import { FormError } from "@/components/form";
import { type DuplicatePair, useMergeBooks } from "@/duplicates/api";
import { type MessageKey, t } from "@/i18n";
import { formatLanguage } from "@/lib/format";

/** The fields a merge may take from the other book, and how each reads. */
const FIELDS: {
	field: string;
	label: MessageKey;
	value: (b: BookDetail) => string;
}[] = [
	{ field: "title", label: "duplicates.field.title", value: (b) => b.title },
	{
		field: "subtitle",
		label: "merge.field.subtitle",
		value: (b) => b.subtitle ?? "",
	},
	{
		field: "contributors",
		label: "duplicates.field.authors",
		value: (b) => b.contributors.map((c) => c.name).join(", "),
	},
	{
		field: "series",
		label: "duplicates.field.series",
		value: (b) =>
			b.series
				? `${b.series}${b.seriesIndex != null ? ` ${b.seriesIndex}` : ""}`
				: "",
	},
	{
		field: "published",
		label: "duplicates.field.published",
		value: (b) => b.published ?? "",
	},
	{
		field: "publisher",
		label: "duplicates.field.publisher",
		value: (b) => b.publisher ?? "",
	},
	{
		field: "language",
		label: "duplicates.field.language",
		value: (b) => (b.language ? formatLanguage(b.language) : ""),
	},
	{
		field: "pageCount",
		label: "duplicates.field.pages",
		value: (b) => (b.pageCount ? String(b.pageCount) : ""),
	},
	{ field: "tags", label: "merge.field.tags", value: (b) => b.tags.join(", ") },
	{
		field: "description",
		label: "merge.field.description",
		value: (b) => b.description ?? "",
	},
	{
		field: "cover",
		label: "merge.field.cover",
		value: (b) => (b.coverKey ? t("merge.hasCover") : ""),
	},
];

/**
 * Makes the pair's two books one: a person picks the book that stays and,
 * where the two differ, whose value each field keeps. The other book's
 * files, shelves and everyone's reading go to the one that stays.
 */
export function MergeForm({
	pair,
	onDone,
}: {
	pair: DuplicatePair;
	onDone: () => void;
}) {
	const a = useQuery(bookQuery(pair.books[0]?.id ?? ""));
	const b = useQuery(bookQuery(pair.books[1]?.id ?? ""));
	const [keep, setKeep] = useState(0);
	// Per field, the book whose value is taken; unset is the kept book's.
	const [chosen, setChosen] = useState<Record<string, number>>({});
	const merge = useMergeBooks();
	if (!a.data || !b.data) {
		return <FormError error={a.error ?? b.error} />;
	}
	const books = [a.data, b.data];
	const kept = books[keep] as BookDetail;
	const other = books[1 - keep] as BookDetail;
	const differing = FIELDS.filter((f) => f.value(kept) !== f.value(other));
	// A field the kept book has nothing for is taken from the other anyway.
	const from = (field: (typeof FIELDS)[number]) =>
		chosen[field.field] ?? (field.value(kept) === "" ? 1 - keep : keep);

	return (
		<form
			aria-label={t("merge.title")}
			className="flex flex-col gap-3 rounded-md border border-slate-200 p-3 text-sm dark:border-slate-700"
			onSubmit={(e) => {
				e.preventDefault();
				const take = differing
					.filter((f) => from(f) !== keep)
					.map((f) => f.field);
				merge.mutate(
					{ into: kept.id, from: other.id, take },
					{ onSuccess: onDone },
				);
			}}
		>
			<fieldset className="flex flex-col gap-1">
				<legend className="mb-1 font-medium">{t("merge.keep")}</legend>
				{books.map((book, i) => (
					<label key={book.id} className="flex items-center gap-2">
						<input
							type="radio"
							name={`keep-${pair.id}`}
							checked={keep === i}
							onChange={() => {
								setKeep(i);
								setChosen({});
							}}
						/>
						{t("merge.keepBook", {
							title: book.title,
							files: book.files.length,
						})}
					</label>
				))}
			</fieldset>
			{differing.length > 0 && (
				<fieldset className="flex flex-col gap-2">
					<legend className="mb-1 font-medium">{t("merge.values")}</legend>
					{differing.map((f) => (
						<div
							key={f.field}
							role="radiogroup"
							aria-label={t(f.label)}
							className="flex flex-col gap-1"
						>
							<span className="text-slate-600 dark:text-slate-400">
								{t(f.label)}
							</span>
							{books.map((book, i) => (
								<label key={book.id} className="flex items-baseline gap-2">
									<input
										type="radio"
										name={`${pair.id}-${f.field}`}
										checked={from(f) === i}
										onChange={() => setChosen({ ...chosen, [f.field]: i })}
									/>
									<span className="line-clamp-2 break-words">
										{f.value(book) || t("merge.none")}
									</span>
								</label>
							))}
						</div>
					))}
				</fieldset>
			)}
			<p className="text-slate-600 dark:text-slate-400">
				{t("merge.moves", { title: other.title })}
			</p>
			<FormError error={merge.error} />
			<button
				type="submit"
				disabled={merge.isPending}
				className="self-start rounded-md bg-brand-strong px-3 py-1 font-medium text-white hover:bg-sky-800 disabled:opacity-60"
			>
				{t("merge.submit", { title: kept.title })}
			</button>
		</form>
	);
}
