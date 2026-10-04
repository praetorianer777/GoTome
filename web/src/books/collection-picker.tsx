import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	collectionsQuery,
	useAddToCollection,
	useRemoveFromCollection,
} from "@/books/collections";
import { FormError } from "@/components/form";
import { t } from "@/i18n";

/** Which of the person's collections hold the book, theirs to change. */
export function CollectionPicker({ bookId }: { bookId: string }) {
	const collections = useQuery(collectionsQuery(bookId));
	const add = useAddToCollection();
	const remove = useRemoveFromCollection();
	const mine = collections.data?.filter((c) => c.mine) ?? [];
	const busy = add.isPending || remove.isPending;
	return (
		<details className="rounded-lg border border-slate-200 p-3 text-sm dark:border-slate-800">
			<summary className="cursor-pointer">
				{t("collections.ofBook", {
					count: mine.filter((c) => c.hasBook).length,
				})}
			</summary>
			<div className="mt-2 flex flex-col gap-2">
				{collections.isSuccess && mine.length === 0 && (
					<p className="text-slate-600 dark:text-slate-400">
						{t("collections.none")}
					</p>
				)}
				<ul className="flex flex-col gap-1">
					{mine.map((c) => (
						<li key={c.id}>
							<label className="flex items-center gap-2">
								<input
									type="checkbox"
									checked={Boolean(c.hasBook)}
									disabled={busy}
									onChange={(e) =>
										e.target.checked
											? add.mutate({ id: c.id, books: [bookId] })
											: remove.mutate({ id: c.id, book: bookId })
									}
								/>
								{c.name}
							</label>
						</li>
					))}
				</ul>
				<Link
					to="/collections"
					className="self-start text-brand-strong hover:underline dark:text-sky-400"
				>
					{t("collections.manage")}
				</Link>
				<FormError error={add.error ?? remove.error} />
			</div>
		</details>
	);
}
