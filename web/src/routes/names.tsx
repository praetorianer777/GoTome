import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type NamedEntry, namesQuery } from "@/books/api";
import { MoreBooks } from "@/books/book-list";
import { FormError } from "@/components/form";
import { EmptyState, Loading, PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { bookCount } from "@/lib/format";

/** The letter a name is listed under; what starts with no letter goes first. */
function letterOf(entry: NamedEntry): string {
	const first = entry.sort.normalize("NFKD").charAt(0).toLocaleUpperCase();
	return /\p{L}/u.test(first) ? first : "#";
}

/** Names in their order, cut where the letter changes. */
function byLetter(names: NamedEntry[]): { letter: string; names: NamedEntry[] }[] {
	const groups: { letter: string; names: NamedEntry[] }[] = [];
	for (const name of names) {
		const letter = letterOf(name);
		const last = groups.at(-1);
		if (last?.letter === letter) {
			last.names.push(name);
		} else {
			groups.push({ letter, names: [name] });
		}
	}
	return groups;
}

/** Every author, or every series, of the books the person sees, A to Z. */
export function NamesPage({ kind }: { kind: "authors" | "series" }) {
	const names = useInfiniteQuery(namesQuery(kind === "authors" ? "/authors" : "/series"));
	const shown = names.data?.pages.flatMap((page) => page.names) ?? [];
	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t(kind === "authors" ? "names.authors" : "names.series")} />
			<FormError error={names.error} />
			{names.isPending && <Loading />}
			{names.isSuccess && shown.length === 0 && (
				<EmptyState title={t("library.empty.title")}>{t("library.empty.body")}</EmptyState>
			)}
			{byLetter(shown).map((group) => (
				<section key={group.letter} aria-label={group.letter} className="flex flex-col gap-3">
					<h2 className="text-lg font-medium">{group.letter}</h2>
					<ul className="grid gap-x-6 gap-y-1 sm:grid-cols-2 lg:grid-cols-3">
						{group.names.map((name) => (
							<li key={name.key} className="flex items-baseline justify-between gap-3">
								{kind === "authors" ? (
									<Link
										to="/authors/$authorKey"
										params={{ authorKey: name.key }}
										className="min-w-0 truncate hover:text-brand-strong hover:underline dark:hover:text-brand"
									>
										{name.name}
									</Link>
								) : (
									<Link
										to="/series/$seriesKey"
										params={{ seriesKey: name.key }}
										className="min-w-0 truncate hover:text-brand-strong hover:underline dark:hover:text-brand"
									>
										{name.name}
									</Link>
								)}
								<span className="shrink-0 text-sm text-slate-600 dark:text-slate-400">
									{bookCount(name.books)}
								</span>
							</li>
						))}
					</ul>
				</section>
			))}
			{names.hasNextPage && (
				<MoreBooks
					loading={names.isFetchingNextPage}
					onMore={() => {
						if (!names.isFetchingNextPage) {
							names.fetchNextPage();
						}
					}}
				/>
			)}
		</div>
	);
}
