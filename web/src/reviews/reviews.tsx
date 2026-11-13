import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { FormError, Switch } from "@/components/form";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";
import {
	bookReviewsQuery,
	sharingQuery,
	useSetReview,
	useSetSharing,
} from "@/reviews/api";

/** Filled and empty stars for a rating of 0 to 5. */
export function Stars({ count }: { count: number }) {
	if (count <= 0) return null;
	return (
		<span
			role="img"
			aria-label={t("reading.stars", { count })}
			className="text-amber-600 dark:text-amber-400"
		>
			{"★".repeat(count)}
			{"☆".repeat(5 - count)}
		</span>
	);
}

/** Reviews others on the server share, their stars, and the person's own review. */
export function BookReviews({ bookId, personal }: { bookId: string; personal: boolean }) {
	const reviews = useQuery(bookReviewsQuery(bookId));
	const data = reviews.data;
	if (!data) return null;
	if (!personal && data.reviews.length === 0 && data.stars === 0) return null;
	return (
		<section aria-labelledby="book-reviews" className="flex min-w-0 flex-col gap-3">
			<h2 id="book-reviews" className="text-sm font-medium">
				{t("reviews.title")}
			</h2>
			{data.stars > 0 && (
				<p className="flex flex-wrap items-center gap-x-2">
					<Stars count={Math.round(data.average)} />
					<span>
						{t("reviews.average", {
							average: data.average.toFixed(1),
							count: data.stars,
						})}
					</span>
				</p>
			)}
			{data.reviews.length > 0 ? (
				<ul className="flex flex-col gap-3">
					{data.reviews.map((r) => (
						<li
							key={r.author}
							className="flex min-w-0 flex-col gap-1 rounded-md border border-slate-200 p-3 dark:border-slate-700"
						>
							<div className="flex flex-wrap items-baseline gap-x-3">
								<Link
									to="/people/$username"
									params={{ username: r.author }}
									className="font-medium underline"
								>
									{r.author}
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
			) : (
				<p className="text-slate-600 dark:text-slate-400">{t("reviews.none")}</p>
			)}
			{personal && (
				<OwnReview
					bookId={bookId}
					saved={data.mine?.body ?? ""}
					savedPrivate={data.mine?.private ?? false}
				/>
			)}
		</section>
	);
}

function OwnReview({
	bookId,
	saved,
	savedPrivate,
}: {
	bookId: string;
	saved: string;
	savedPrivate: boolean;
}) {
	const save = useSetReview(bookId);
	const sharing = useQuery(sharingQuery);
	const [typed, setTyped] = useState<string>();
	const [priv, setPriv] = useState<boolean>();
	const text = typed ?? saved;
	const isPrivate = priv ?? savedPrivate;
	const changed = text !== saved || isPrivate !== savedPrivate;
	const shared = sharing.data?.shareReviews === true && !isPrivate && text.trim() !== "";
	return (
		<form
			className="flex flex-col gap-2"
			onSubmit={(e) => {
				e.preventDefault();
				save.mutate(
					{ body: text, private: isPrivate },
					{
						onSuccess: () => {
							setTyped(undefined);
							setPriv(undefined);
						},
					},
				);
			}}
		>
			<label className="flex flex-col gap-1">
				<span>{t("reviews.mine")}</span>
				<textarea
					value={text}
					rows={4}
					onChange={(e) => setTyped(e.target.value)}
					className="w-full rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100"
				/>
			</label>
			<label className="flex items-center gap-2">
				<input
					type="checkbox"
					checked={isPrivate}
					onChange={(e) => setPriv(e.target.checked)}
				/>
				<span>{t("reviews.private")}</span>
			</label>
			<div className="flex flex-wrap items-center gap-x-3 gap-y-2">
				<button
					type="submit"
					disabled={save.isPending || !changed}
					className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
				>
					{t("reviews.save")}
				</button>
				<span className="text-slate-600 dark:text-slate-400">
					{shared
						? t("reviews.hint.shared")
						: sharing.data?.shareReviews
							? t("reviews.hint.private")
							: t("reviews.hint.off")}
				</span>
			</div>
			<FormError error={save.error} />
		</form>
	);
}

/** The person's choice to share their ratings and reviews with others. */
export function SharingChoice() {
	const sharing = useQuery(sharingQuery);
	const set = useSetSharing();
	if (!sharing.data) return null;
	return (
		<section aria-labelledby="sharing" className="flex max-w-md flex-col gap-3">
			<h2 id="sharing" className="text-lg font-medium">
				{t("reviews.profile")}
			</h2>
			<Switch
				label={t("reviews.share")}
				hint={t("reviews.share.hint")}
				checked={sharing.data.shareReviews}
				onChange={(on) => set.mutate(on)}
			/>
			<FormError error={set.error} />
		</section>
	);
}
