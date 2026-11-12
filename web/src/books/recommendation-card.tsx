import { Link } from "@tanstack/react-router";
import { Cover } from "@/books/cover";
import { MediumBadge } from "@/books/medium";
import { type Recommendation, useDismissRecommendation } from "@/books/recommendations";
import { FormError } from "@/components/form";
import { t } from "@/i18n";

/** A recommended book: its cover, why, and a way to say it is not for them. */
export function RecommendationCard({ rec }: { rec: Recommendation }) {
	const dismiss = useDismissRecommendation();
	const { book } = rec;
	return (
		<div className="relative flex flex-col gap-1">
			<MediumBadge formats={book.formats} className="top-1 right-1 scale-90" />
			<Link to="/books/$bookId" params={{ bookId: book.id }} className="flex flex-col gap-1">
				<Cover book={book} size="small" />
				<span className="line-clamp-2 text-sm font-medium">{book.title}</span>
			</Link>
			{rec.because.length > 0 && (
				<span className="line-clamp-3 text-xs text-slate-600 dark:text-slate-400">
					{t("recommendations.because", { books: rec.because.map((b) => b.title).join(", ") })}
				</span>
			)}
			<button
				type="button"
				disabled={dismiss.isPending}
				aria-label={t("recommendations.notInterestedNamed", { title: book.title })}
				onClick={() => dismiss.mutate(book.id)}
				className="min-h-6 self-start text-xs text-slate-600 underline underline-offset-4 hover:text-slate-900 disabled:opacity-60 dark:text-slate-400 dark:hover:text-white"
			>
				{t("recommendations.notInterested")}
			</button>
			<FormError error={dismiss.error} />
		</div>
	);
}
