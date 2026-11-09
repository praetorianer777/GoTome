import { useQuery } from "@tanstack/react-query";
import { RecommendationCard } from "@/books/recommendation-card";
import { recommendationsQuery } from "@/books/recommendations";
import { FormError } from "@/components/form";
import { Loading } from "@/components/ui";
import { t } from "@/i18n";

const MORE = 48;

/**
 * Unread books of the libraries the person sees that are near what they
 * liked, each with the books that led to it. Only theirs.
 */
export function Recommendations() {
	const recommended = useQuery(recommendationsQuery(MORE));
	const books = recommended.data?.books ?? [];
	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("recommendations.title")}</h1>
			{recommended.isPending && <Loading />}
			<FormError error={recommended.error} />
			{recommended.isSuccess && !recommended.data.hasProfile && (
				<p className="text-slate-600 dark:text-slate-400">{t("recommendations.noProfile")}</p>
			)}
			{recommended.isSuccess && recommended.data.hasProfile && books.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("recommendations.none")}</p>
			)}
			<ul className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
				{books.map((rec) => (
					<li key={rec.book.id}>
						<RecommendationCard rec={rec} />
					</li>
				))}
			</ul>
		</div>
	);
}
