import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { booksQuery } from "@/books/api";
import { Cover } from "@/books/cover";
import { candidateCoverUrl } from "@/books/edit";
import { type WishSearch, useWish, wishSearchQuery } from "@/books/wishes";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { librariesQuery } from "@/libraries/api";

const WISHED = JSON.stringify({ all: [{ field: "status", op: "in", values: ["wishlist"] }] });

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The books the person wishes for, those the library holds and those it does not yet. */
export function Wishlist() {
	const books = useInfiniteQuery(
		booksQuery({ filter: WISHED, sort: "added", order: "desc", placeholders: "include" }),
	);
	const shown = books.data?.pages.flatMap((p) => p.books) ?? [];
	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("wishlist.title")}</h1>
			<WishFinder />
			<section aria-labelledby="wished" className="flex flex-col gap-3">
				<h2 id="wished" className="text-lg font-medium">
					{t("wishlist.wished")}
				</h2>
				<FormError error={books.error} />
				{books.isSuccess && shown.length === 0 && (
					<p className="text-slate-600 dark:text-slate-400">{t("wishlist.none")}</p>
				)}
				<ul className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
					{shown.map((book) => (
						<li key={book.id}>
							<Link to="/books/$bookId" params={{ bookId: book.id }} className="flex flex-col gap-2">
								<Cover book={book} size="small" />
								<span className="flex flex-col">
									<span className="line-clamp-2 font-medium leading-snug">{book.title}</span>
									{book.authors.length > 0 && (
										<span className="line-clamp-1 text-sm text-slate-600 dark:text-slate-400">
											{book.authors.join(", ")}
										</span>
									)}
									<span className="text-sm text-slate-500">
										{book.placeholder ? t("wishlist.missing") : t("wishlist.held")}
									</span>
								</span>
							</Link>
						</li>
					))}
				</ul>
				{books.hasNextPage && (
					<button
						type="button"
						className={`${button} self-center`}
						disabled={books.isFetchingNextPage}
						onClick={() => books.fetchNextPage()}
					>
						{t("library.more")}
					</button>
				)}
			</section>
		</div>
	);
}

/** Looks books up with the providers, to wish for one the library lacks. */
function WishFinder() {
	const [typed, setTyped] = useState<WishSearch>({});
	const [search, setSearch] = useState<WishSearch>({});
	const found = useQuery(wishSearchQuery(search));
	const libraries = useQuery(librariesQuery);
	const [library, setLibrary] = useState<string>();
	const chosen = library ?? libraries.data?.[0]?.id;
	const wish = useWish();
	const navigate = useNavigate();

	return (
		<section aria-labelledby="wish-for" className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800">
			<h2 id="wish-for" className="text-lg font-medium">
				{t("wishlist.find")}
			</h2>
			<form
				className="flex flex-wrap items-end gap-3 text-sm"
				onSubmit={(e) => {
					e.preventDefault();
					setSearch({
						title: typed.title?.trim() || undefined,
						author: typed.author?.trim() || undefined,
						isbn: typed.isbn?.trim() || undefined,
					});
				}}
			>
				{(["title", "author", "isbn"] as const).map((field) => (
					<label key={field} className="flex flex-col gap-1">
						<span>{t(`wishlist.field.${field}`)}</span>
						<input
							value={typed[field] ?? ""}
							onChange={(e) => setTyped({ ...typed, [field]: e.target.value })}
							className={control}
						/>
					</label>
				))}
				<button type="submit" className={button}>
					{t("wishlist.search")}
				</button>
			</form>
			{found.isFetching && <p className="text-slate-500">{t("wishlist.searching")}</p>}
			<FormError error={found.error} />
			{found.data?.failures.map((f) => (
				<p key={f.provider} className="text-sm text-amber-800 dark:text-amber-300">
					{f.provider}: {f.message}
				</p>
			))}
			{found.isSuccess && found.data.candidates.length === 0 && !found.isFetching && (
				<p className="text-slate-600 dark:text-slate-400">{t("wishlist.nothing")}</p>
			)}
			{found.data && found.data.candidates.length > 0 && (
				<>
					{(libraries.data?.length ?? 0) > 1 && (
						<label className="flex items-center gap-2 text-sm">
							<span>{t("wishlist.library")}</span>
							<select value={chosen} onChange={(e) => setLibrary(e.target.value)} className={control}>
								{libraries.data?.map((l) => (
									<option key={l.id} value={l.id}>
										{l.name}
									</option>
								))}
							</select>
						</label>
					)}
					<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
						{found.data.candidates.map((c) => (
							<li key={`${c.provider}-${c.id}`} className="flex items-start gap-3 py-2">
								{c.coverToken ? (
									<img src={candidateCoverUrl(c.coverToken)} alt="" className="w-12 rounded" />
								) : (
									<span className="w-12" />
								)}
								<div className="min-w-0 flex-1">
									<p className="font-medium">{c.title}</p>
									<p className="text-sm text-slate-600 dark:text-slate-400">
										{[c.contributors.map((p) => p.name).join(", "), c.published, c.publisher]
											.filter(Boolean)
											.join(" · ")}
									</p>
								</div>
								<button
									type="button"
									className={button}
									disabled={!chosen || wish.isPending}
									aria-label={t("wishlist.wishNamed", { title: c.title })}
									onClick={() =>
										chosen &&
										wish.mutate(
											{
												library: chosen,
												provider: c.provider,
												title: c.title,
												subtitle: c.subtitle,
												description: c.description,
												language: c.language,
												published: c.published,
												publisher: c.publisher,
												series: c.series ? { name: c.series, index: c.seriesIndex } : undefined,
												pageCount: c.pageCount,
												contributors: c.contributors,
												tags: c.tags,
												identifiers: c.identifiers,
												coverToken: c.coverToken,
											},
											{
												onSuccess: (book) =>
													book && navigate({ to: "/books/$bookId", params: { bookId: book.id } }),
											},
										)
									}
								>
									{t("wishlist.wish")}
								</button>
							</li>
						))}
					</ul>
				</>
			)}
			<FormError error={wish.error} />
		</section>
	);
}
