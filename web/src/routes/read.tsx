import { useQuery } from "@tanstack/react-query";
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { Suspense, lazy } from "react";
import { bookQuery } from "@/books/api";
import { FormError } from "@/components/form";
import { t } from "@/i18n";

// Each reader brings its own library, PDF.js or foliate-js, which loads
// with the first file of its kind opened and not with the app.
const PdfReader = lazy(() =>
	import("@/routes/read-pdf").then((m) => ({ default: m.PdfReader })),
);
const EbookReader = lazy(() =>
	import("@/routes/read-ebook").then((m) => ({ default: m.EbookReader })),
);

/** The formats the ebook reader opens. */
export const EBOOK_FORMATS = ["epub", "mobi", "azw3", "azw"];

/** Whether a file of the format opens in a reader in the browser. */
export function readable(format: string): boolean {
	return format === "pdf" || EBOOK_FORMATS.includes(format);
}

/**
 * Where a reader opens instead of the saved place, as the address carries
 * it: a page of a PDF, or a passage, by a word of it and its text, which the
 * reader looks for; in a PDF on the pages from page to to.
 */
export interface ReaderSearch {
	page?: number;
	to?: number;
	find?: string;
	near?: string;
}

function pageNumber(value: unknown): number | undefined {
	const n = Number(value);
	return Number.isInteger(n) && n > 0 ? n : undefined;
}

export function readerSearch(search: Record<string, unknown>): ReaderSearch {
	return {
		page: pageNumber(search.page),
		to: pageNumber(search.to),
		find: typeof search.find === "string" && search.find ? search.find : undefined,
		near: typeof search.near === "string" ? search.near : undefined,
	};
}

/** The reader for a file of a book, by its format. */
export function ReaderRoute() {
	const { bookId, fileId } = useParams({
		from: "/app/books/$bookId/read/$fileId",
	});
	const { page, to, find, near } = useSearch({
		from: "/app/books/$bookId/read/$fileId",
	});
	const book = useQuery(bookQuery(bookId));
	if (!book.data) {
		return (
			<>
				{book.isPending && (
					<p className="text-slate-500">{t("reader.opening")}</p>
				)}
				<FormError error={book.error} />
			</>
		);
	}
	const file = book.data.files.find((f) => f.id === fileId);
	const opening = <p className="text-slate-500">{t("reader.opening")}</p>;
	if (file?.format === "pdf") {
		return (
			<Suspense fallback={opening}>
				<PdfReader
					bookId={bookId}
					fileId={fileId}
					at={page}
					find={find ? { word: find, passage: near ?? find, to } : undefined}
				/>
			</Suspense>
		);
	}
	if (file && EBOOK_FORMATS.includes(file.format)) {
		return (
			<Suspense fallback={opening}>
				<EbookReader
					bookId={bookId}
					fileId={fileId}
					fileName={file.name}
					title={book.data.title}
					find={find ? { word: find, passage: near ?? find } : undefined}
				/>
			</Suspense>
		);
	}
	return (
		<div className="flex flex-col gap-3">
			<p className="text-slate-600 dark:text-slate-400">
				{t("reader.cannotRead")}
			</p>
			<Link
				to="/books/$bookId"
				params={{ bookId }}
				className="text-brand-strong underline underline-offset-4 dark:text-brand"
			>
				{t("reader.back")}
			</Link>
		</div>
	);
}
