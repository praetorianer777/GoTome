import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { type CurrentUser, can } from "@/auth/session";
import { type BookSort, booksQuery, facetsQuery } from "@/books/api";
import { Grid, GridSkeleton, MoreBooks, Table } from "@/books/book-list";
import { BulkBar } from "@/books/bulk-bar";
import { FilterPanel } from "@/books/filter-panel";
import {
	FACET_FIELDS,
	type FilterPicks,
	filterTree,
	pickCount,
	type Rule,
} from "@/books/filters";
import { FormError } from "@/components/form";
import { button, control, EmptyState } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";

export type BookView = "grid" | "list";

/** How a list of books is shown, as the address carries it. */
export interface BookSearch extends FilterPicks {
	sort?: BookSort;
	view?: BookView;
}

export interface SortChoice {
	value: BookSort;
	label: MessageKey;
}

const VIEWS: { value: BookView; label: MessageKey }[] = [
	{ value: "grid", label: "library.view.grid" },
	{ value: "list", label: "library.view.list" },
];

const BOOKS_POLL_MS = 3000;

/**
 * A list of books with its sorting, views, filters and selection: the
 * library, or the part of it a page is about (`base`).
 */
export function BookBrowser({
	user,
	search,
	onSearch,
	sorts,
	defaultSort,
	base,
	library,
	busy = false,
	enabled = true,
	tools,
	above,
	empty,
}: {
	user: CurrentUser;
	search: BookSearch;
	onSearch: (change: Partial<BookSearch>) => void;
	sorts: SortChoice[];
	defaultSort: BookSort;
	/** The rule every book of the list matches, beside the filters picked. */
	base?: Rule;
	library?: string;
	/** Books are arriving, so the list is asked again as they do. */
	busy?: boolean;
	enabled?: boolean;
	/** More controls at the start of the toolbar. */
	tools?: ReactNode;
	/** What stands over the list, beside the filters. */
	above?: ReactNode;
	/** What the list says while it holds nothing and nothing is picked. */
	empty?: ReactNode;
}) {
	const sort = search.sort ?? defaultSort;
	const view = search.view ?? "grid";
	const picks: FilterPicks = Object.fromEntries(
		FACET_FIELDS.map((field) => [field, search[field]]),
	);
	const filter = filterTree(picks, base);
	const filtered = pickCount(picks);
	const [filtersOpen, setFiltersOpen] = useState(false);

	const books = useInfiniteQuery({
		...booksQuery(
			{
				library,
				filter,
				sort,
				order: sort === "added" ? "desc" : "asc",
			},
			busy ? BOOKS_POLL_MS : false,
		),
		enabled,
	});
	const facets = useQuery({
		...facetsQuery({ library, filter }, busy ? BOOKS_POLL_MS : false),
		enabled,
	});
	const shown = books.data?.pages.flatMap((page) => page.books) ?? [];
	const canSelect = can(user, "personal:manage");
	const [selecting, setSelecting] = useState(false);
	const [chosen, setChosen] = useState<ReadonlySet<string>>(new Set());
	const [allMatching, setAllMatching] = useState(false);
	// A selection belongs to the list it was made in.
	const listKey = `${library ?? ""} ${filter ?? ""}`;
	const [chosenIn, setChosenIn] = useState(listKey);
	if (chosenIn !== listKey) {
		setChosenIn(listKey);
		setChosen(new Set());
		setAllMatching(false);
	}
	const toggle = (id: string) => {
		// Unticking one of every book that matches leaves those shown.
		const next = new Set(allMatching ? shown.map((b) => b.id) : chosen);
		if (!next.delete(id)) {
			next.add(id);
		}
		setChosen(next);
		setAllMatching(false);
	};
	const clearSelection = () => {
		setChosen(new Set());
		setAllMatching(false);
	};
	const selection = selecting
		? { isSelected: (id: string) => allMatching || chosen.has(id), toggle }
		: undefined;
	// Every field is named, so that one taken out of the picks leaves the
	// address too.
	const setPicks = (next: FilterPicks) =>
		onSearch(
			Object.fromEntries(FACET_FIELDS.map((field) => [field, next[field]])),
		);
	const showSeries = sort === "series";

	return (
		<>
			<div className="flex flex-wrap items-center gap-x-4 gap-y-3 border-b border-slate-200 pb-4 text-sm dark:border-slate-800">
				{tools}
				<label className="flex items-center gap-2">
					<span className="text-slate-600 dark:text-slate-400">{t("library.sort")}</span>
					<select
						value={sort}
						onChange={(e) => onSearch({ sort: e.target.value as BookSort })}
						className={control}
					>
						{sorts.map((s) => (
							<option key={s.value} value={s.value}>
								{t(s.label)}
							</option>
						))}
					</select>
				</label>
				<fieldset className="flex items-center">
					<legend className="sr-only">{t("library.view")}</legend>
					{VIEWS.map((v) => (
						<button
							key={v.value}
							type="button"
							aria-pressed={view === v.value}
							onClick={() => onSearch({ view: v.value })}
							className={`${button("toggle")} first:rounded-r-none last:-ml-px last:rounded-l-none`}
						>
							{t(v.label)}
						</button>
					))}
				</fieldset>
				{canSelect && (
					<button
						type="button"
						aria-pressed={selecting}
						onClick={() => {
							setSelecting(!selecting);
							clearSelection();
						}}
						className={`${button("toggle")} sm:ml-auto`}
					>
						{t(selecting ? "bulk.selectDone" : "bulk.select")}
					</button>
				)}
				<button
					type="button"
					aria-expanded={filtersOpen}
					aria-controls="filters"
					onClick={() => setFiltersOpen(!filtersOpen)}
					className={`${button()} lg:hidden`}
				>
					{filtered > 0
						? t("filter.toggleActive", { count: filtered })
						: t("filter.toggle")}
				</button>
			</div>

			<div className="flex flex-col gap-6 lg:flex-row lg:items-start">
				<aside
					id="filters"
					aria-label={t("filter.title")}
					className={`${filtersOpen ? "" : "hidden"} shrink-0 lg:sticky lg:top-20 lg:block lg:max-h-[calc(100dvh-6rem)] lg:w-56 lg:overflow-y-auto`}
				>
					<FilterPanel
						facets={facets.data ?? []}
						picks={picks}
						onChange={setPicks}
					/>
				</aside>
				<div className="flex min-w-0 flex-1 flex-col gap-6">
					{above}

					<section
						aria-label={t("library.books")}
						className="flex flex-col gap-4"
					>
						{selecting && (
							<BulkBar
								count={chosen.size}
								canEdit={can(user, "metadata:edit")}
								all={allMatching}
								selection={
									allMatching ? { library, filter } : { books: [...chosen] }
								}
								onSelectAll={() => setAllMatching(true)}
								onClear={clearSelection}
							/>
						)}
						<FormError error={books.error} />
						{books.isPending && <GridSkeleton />}
						{books.isSuccess &&
							shown.length === 0 &&
							(filtered > 0 ? (
								<EmptyState
									title={t("library.filtered.title")}
									action={
										<button
											type="button"
											onClick={() => setPicks({})}
											className={button()}
										>
											{t("filter.clear")}
										</button>
									}
								>
									{t("library.filtered.body")}
								</EmptyState>
							) : (
								empty
							))}
						{shown.length > 0 &&
							(view === "grid" ? (
								<Grid books={shown} selection={selection} showSeries={showSeries} />
							) : (
								<Table books={shown} selection={selection} showSeries={showSeries} />
							))}
						{books.hasNextPage && (
							<MoreBooks
								loading={books.isFetchingNextPage}
								onMore={() => {
									if (!books.isFetchingNextPage) {
										books.fetchNextPage();
									}
								}}
							/>
						)}
					</section>
				</div>
			</div>
		</>
	);
}
