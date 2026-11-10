import { useQueries } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { bookQuery } from "@/books/api";
import { t } from "@/i18n";
import { useQueue } from "@/player/queue";

const small =
	"rounded-md border border-slate-300 px-2 py-0.5 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-600 dark:hover:bg-slate-800";

/**
 * The audiobooks that play after this one, in order, to be moved or taken
 * out. Nothing is shown while there are none.
 */
export function UpNext() {
	const queue = useQueue();
	const books = useQueries({
		queries: queue.books.map((id) => bookQuery(id)),
	});
	if (queue.books.length === 0) return null;
	return (
		<section aria-labelledby="up-next" className="flex flex-col gap-2">
			<div className="flex items-center justify-between gap-4">
				<h2 id="up-next" className="text-lg font-medium">
					{t("queue.title")}
				</h2>
				<button type="button" className={small} onClick={() => queue.clear()}>
					{t("queue.clear")}
				</button>
			</div>
			<ol className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
				{queue.books.map((id, i) => {
					const book = books[i]?.data;
					const title = book?.title ?? "…";
					return (
						<li key={id} className="flex items-center gap-2 px-3 py-2">
							<Link
								to="/books/$bookId"
								params={{ bookId: id }}
								className="min-w-0 flex-1 truncate hover:underline"
							>
								{title}
							</Link>
							<button
								type="button"
								className={small}
								aria-label={t("queue.up", { title })}
								disabled={i === 0}
								onClick={() => queue.move(id, -1)}
							>
								↑
							</button>
							<button
								type="button"
								className={small}
								aria-label={t("queue.down", { title })}
								disabled={i === queue.books.length - 1}
								onClick={() => queue.move(id, 1)}
							>
								↓
							</button>
							<button
								type="button"
								className={small}
								aria-label={t("queue.removeBook", { title })}
								onClick={() => queue.remove(id)}
							>
								×
							</button>
						</li>
					);
				})}
			</ol>
		</section>
	);
}

/** Queues a book from its page: to play next, or last, or not at all. */
export function QueueButtons({
	bookId,
	className,
}: {
	bookId: string;
	className: string;
}) {
	const queue = useQueue();
	if (queue.has(bookId)) {
		return (
			<button type="button" className={className} onClick={() => queue.remove(bookId)}>
				{t("queue.remove")}
			</button>
		);
	}
	return (
		<>
			<button type="button" className={className} onClick={() => queue.add([bookId], "next")}>
				{t("queue.next")}
			</button>
			<button type="button" className={className} onClick={() => queue.add([bookId], "last")}>
				{t("queue.add")}
			</button>
		</>
	);
}
