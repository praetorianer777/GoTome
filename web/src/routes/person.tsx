import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { FormError } from "@/components/form";
import { EmptyState, Loading } from "@/components/ui";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";
import { personReviewsQuery } from "@/reviews/api";
import { Stars } from "@/reviews/reviews";

/** The reviews a person shares, of the books the viewer sees. */
export function PersonPage({ username }: { username: string }) {
	const person = useQuery(personReviewsQuery(username));
	if (person.error instanceof ApiError && person.error.status === 404) {
		return <EmptyState mood="confused" title={t("person.notFound")} />;
	}
	const data = person.data;
	if (!data) {
		return (
			<>
				{person.isPending && <Loading />}
				<FormError error={person.error} />
			</>
		);
	}
	return (
		<div className="flex flex-col gap-6 wrap-anywhere">
			<h1 className="text-2xl font-semibold">
				{t("person.title", { name: data.username })}
			</h1>
			<ul className="flex max-w-2xl flex-col gap-3">
				{data.reviews.map((r) => (
					<li
						key={r.bookId}
						className="flex min-w-0 flex-col gap-1 rounded-md border border-slate-200 p-3 dark:border-slate-700"
					>
						<div className="flex flex-wrap items-baseline gap-x-3">
							<Link
								to="/books/$bookId"
								params={{ bookId: r.bookId }}
								className="font-medium underline"
							>
								{r.title}
							</Link>
							<Stars count={r.stars} />
							<span className="text-sm text-slate-600 dark:text-slate-400">
								{formatDate(r.updatedAt)}
							</span>
						</div>
						<p className="whitespace-pre-line">{r.body}</p>
					</li>
				))}
			</ul>
		</div>
	);
}
