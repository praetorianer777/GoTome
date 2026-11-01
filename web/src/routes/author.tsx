import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { type CurrentUser, can } from "@/auth/session";
import { authorQuery } from "@/books/api";
import { BookBrowser, type BookSearch, type SortChoice } from "@/books/book-browser";
import { GridSkeleton } from "@/books/book-list";
import { FormError } from "@/components/form";
import { EmptyState, PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { bookCount } from "@/lib/format";
import { Follow } from "@/releases/follow";

const SORTS: SortChoice[] = [
	{ value: "series", label: "library.sort.series" },
	{ value: "title", label: "library.sort.title" },
	{ value: "added", label: "library.sort.added" },
];

/** An author's books, of those the person sees, series by series. */
export function AuthorPage({
	user,
	authorKey,
	search,
	onSearch,
}: {
	user: CurrentUser;
	authorKey: string;
	search: BookSearch;
	onSearch: (change: Partial<BookSearch>) => void;
}) {
	const author = useQuery(authorQuery(authorKey));
	const a = author.data;
	if (author.error instanceof ApiError && author.error.status === 404) {
		return <EmptyState title={t("author.notFound")} />;
	}
	if (!a) {
		return (
			<>
				{author.isPending && <GridSkeleton />}
				<FormError error={author.error} />
			</>
		);
	}
	const others = a.spellings.filter((name) => name !== a.name);
	return (
		<div className="flex flex-col gap-6">
			<PageHeader
				title={a.name}
				count={bookCount(a.books)}
				intro={
					others.length > 0 &&
					t("author.spellings", { names: others.join(", ") })
				}
				actions={
					can(user, "personal:manage") && (
						<Follow subjects={[{ kind: "author", name: a.name }]} />
					)
				}
			/>
			<BookBrowser
				user={user}
				search={search}
				onSearch={onSearch}
				sorts={SORTS}
				defaultSort="series"
				base={{ field: "person", op: "in", values: [a.key] }}
				empty={<EmptyState title={t("author.notFound")} />}
			/>
		</div>
	);
}
