import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
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

/** The reader for a file of a book, by its format. */
export function ReaderRoute() {
	const { bookId, fileId } = useParams({
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
				<PdfReader bookId={bookId} fileId={fileId} />
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
