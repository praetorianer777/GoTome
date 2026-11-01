import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type CurrentUser, can } from "@/auth/session";
import { BookBrowser, type BookSearch, type SortChoice } from "@/books/book-browser";
import { GridSkeleton } from "@/books/book-list";
import { FormError } from "@/components/form";
import { button, control, EmptyState, PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { libraryIsBusy, librariesQuery } from "@/libraries/api";
import { ScanStatus } from "@/libraries/scan";

/** What the library page shows, as the address carries it. */
export interface LibrarySearch extends BookSearch {
	library?: string;
}

const SORTS: SortChoice[] = [
	{ value: "title", label: "library.sort.title" },
	{ value: "author", label: "library.sort.author" },
	{ value: "added", label: "library.sort.added" },
];

export function Library({
	user,
	search,
	onSearch,
}: {
	user: CurrentUser;
	search: LibrarySearch;
	onSearch: (change: Partial<LibrarySearch>) => void;
}) {
	const libraries = useQuery(librariesQuery);
	const list = libraries.data ?? [];
	// With one library there is nothing to choose between.
	const selected =
		list.find((l) => l.id === search.library) ??
		(list.length === 1 ? list[0] : undefined);
	const busy = (selected ? [selected] : list).some(libraryIsBusy);

	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t("library.title")} />
			{libraries.isPending && <GridSkeleton />}
			<FormError error={libraries.error} />

			{libraries.data?.length === 0 && (
				<EmptyState
					title={t("library.none.title")}
					action={
						can(user, "storage:manage") && (
							<Link to="/admin/libraries" className={button("primary", "md")}>
								{t("library.none.add")}
							</Link>
						)
					}
				>
					{t(
						can(user, "storage:manage")
							? "library.none.admin"
							: "library.none.other",
					)}
				</EmptyState>
			)}

			{list.length > 0 && (
				<BookBrowser
					user={user}
					search={search}
					onSearch={onSearch}
					sorts={SORTS}
					defaultSort="title"
					library={selected?.id}
					busy={busy}
					tools={
						list.length > 1 && (
							<label className="flex items-center gap-2">
								<span className="text-slate-600 dark:text-slate-400">{t("library.choose")}</span>
								<select
									value={selected?.id ?? ""}
									onChange={(e) =>
										onSearch({ library: e.target.value || undefined })
									}
									className={control}
								>
									<option value="">{t("library.all")}</option>
									{list.map((l) => (
										<option key={l.id} value={l.id}>
											{l.name}
										</option>
									))}
								</select>
							</label>
						)
					}
					above={
						selected && (
							<section
								aria-labelledby="library-name"
								className="flex flex-col gap-2"
							>
								<h2 id="library-name" className="text-lg font-medium">
									{selected.name}
								</h2>
								<ScanStatus
									library={selected}
									canScan={can(user, "index:rebuild")}
								/>
							</section>
						)
					}
					empty={
						busy ? (
							<EmptyState>{t("library.adding")}</EmptyState>
						) : (
							<EmptyState title={t("library.empty.title")}>
								{t("library.empty.body")}
							</EmptyState>
						)
					}
				/>
			)}
		</div>
	);
}
