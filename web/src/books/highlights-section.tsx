import { useQuery } from "@tanstack/react-query";
import { bookAnnotationsQuery } from "@/books/annotations";
import { ExportLinks, HighlightItem } from "@/books/highlight-tools";
import { t } from "@/i18n";

/** A book's highlights and notes on its page; nothing while there are none. */
export function HighlightsSection({ bookId }: { bookId: string }) {
	const found = useQuery(bookAnnotationsQuery(bookId));
	const list = found.data ?? [];
	if (list.length === 0) return null;
	return (
		<section aria-labelledby="book-highlights" className="flex min-w-0 flex-col gap-2">
			<div className="flex flex-wrap items-center justify-between gap-2">
				<h2 id="book-highlights" className="text-sm font-medium">
					{t("highlights.title")}
				</h2>
				<ExportLinks book={bookId} />
			</div>
			<ul
				aria-labelledby="book-highlights"
				className="flex min-w-0 flex-col divide-y divide-slate-200 dark:divide-slate-800"
			>
				{list.map((a) => (
					<HighlightItem key={a.id} annotation={a} />
				))}
			</ul>
		</section>
	);
}
