import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { annotationsQuery } from "@/books/annotations";
import { ExportLinks, HighlightItem } from "@/books/highlight-tools";
import { FormError } from "@/components/form";
import { button, control, Loading, PageHeader } from "@/components/ui";
import { t } from "@/i18n";

/** Every highlight and note of the person, searchable, to export. */
export function Highlights({
	q,
	onSearch,
}: {
	q?: string;
	onSearch: (q: string | undefined) => void;
}) {
	const [typed, setTyped] = useState(q ?? "");
	useEffect(() => setTyped(q ?? ""), [q]);
	const found = useInfiniteQuery(annotationsQuery(q ?? ""));
	const list = found.data?.pages.flatMap((page) => page.annotations) ?? [];
	return (
		<div className="flex min-w-0 flex-col gap-6">
			<PageHeader
				title={t("highlights.title")}
				intro={t("highlights.intro")}
				actions={<ExportLinks q={q} />}
			/>
			<form
				role="search"
				className="flex max-w-xl gap-2"
				onSubmit={(e) => {
					e.preventDefault();
					onSearch(typed.trim() || undefined);
				}}
			>
				<label className="min-w-0 flex-1">
					<span className="sr-only">{t("highlights.searchLabel")}</span>
					<input
						type="search"
						value={typed}
						onChange={(e) => setTyped(e.target.value)}
						className={`${control} w-full min-w-0`}
					/>
				</label>
				<button type="submit" className={button()}>
					{t("highlights.searchSubmit")}
				</button>
			</form>
			<FormError error={found.error} />
			{found.isPending && <Loading />}
			{found.isSuccess && list.length === 0 && (
				<p role="status" className="text-slate-600 dark:text-slate-400">
					{q ? t("highlights.emptySearch") : t("highlights.empty")}
				</p>
			)}
			{list.length > 0 && (
				<ul
					aria-label={t("highlights.list")}
					className="flex min-w-0 flex-col divide-y divide-slate-200 dark:divide-slate-800"
				>
					{list.map((a) => (
						<HighlightItem key={a.id} annotation={a} withBook />
					))}
				</ul>
			)}
			{found.hasNextPage && (
				<button
					type="button"
					disabled={found.isFetchingNextPage}
					onClick={() => void found.fetchNextPage()}
					className={`${button()} self-start`}
				>
					{t("highlights.more")}
				</button>
			)}
		</div>
	);
}
