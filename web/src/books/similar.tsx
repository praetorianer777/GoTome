import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { similarQuery } from "@/books/api";
import { Cover } from "@/books/cover";
import { t } from "@/i18n";

/** A row of the books most like this one; nothing while there are none. */
export function SimilarBooks({ bookId }: { bookId: string }) {
	const similar = useQuery(similarQuery(bookId));
	const books = similar.data ?? [];
	if (books.length === 0) return null;
	return (
		<section aria-labelledby="book-similar" className="flex min-w-0 flex-col gap-2">
			<h2 id="book-similar" className="text-sm font-medium">
				{t("book.similar")}
			</h2>
			<ul className="flex gap-3 overflow-x-auto pb-2">
				{books.map((book) => (
					<li key={book.id} className="w-24 shrink-0">
						<Link
							to="/books/$bookId"
							params={{ bookId: book.id }}
							className="flex flex-col gap-1"
						>
							<Cover book={book} size="small" />
							<span className="line-clamp-2 text-sm font-medium">
								{book.title}
							</span>
							{book.authors.length > 0 && (
								<span className="truncate text-xs text-slate-600 dark:text-slate-400">
									{book.authors.join(", ")}
								</span>
							)}
						</Link>
					</li>
				))}
			</ul>
		</section>
	);
}
