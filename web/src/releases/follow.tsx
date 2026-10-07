import { useQuery } from "@tanstack/react-query";
import type { components } from "@/api/schema";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import {
	type FollowKind,
	trackerOf,
	trackersQuery,
	useFollow,
	useUnfollow,
} from "./api";

type BookDetail = components["schemas"]["BookDetail"];

/** Buttons that follow the book's authors and its series for new books, or stop. */
export function FollowButtons({ book }: { book: BookDetail }) {
	const trackers = useQuery(trackersQuery);
	const follow = useFollow();
	const unfollow = useUnfollow();
	const subjects: { kind: FollowKind; name: string }[] = book.contributors
		.filter((c) => c.role === "author")
		.map((c) => ({ kind: "author" as const, name: c.name }));
	if (book.series) {
		subjects.push({ kind: "series", name: book.series });
	}
	if (subjects.length === 0 || !trackers.isSuccess) {
		return null;
	}
	const busy = follow.isPending || unfollow.isPending;
	return (
		<div className="flex flex-col gap-1">
			<div className="flex flex-wrap items-center gap-2 text-sm">
				<span className="text-slate-600 dark:text-slate-400">
					{t("book.follow")}
				</span>
				{subjects.map((s) => {
					const tracker = trackerOf(trackers.data, s.kind, s.name);
					const label = t(
						s.kind === "author" ? "book.follow.author" : "book.follow.series",
						{ name: s.name },
					);
					return (
						<button
							key={`${s.kind}:${s.name}`}
							type="button"
							aria-pressed={tracker != null}
							title={t(tracker ? "book.follow.on" : "book.follow.off", {
								name: s.name,
							})}
							disabled={busy}
							onClick={() =>
								tracker ? unfollow.mutate(tracker.id) : follow.mutate(s)
							}
							className="rounded-full border border-slate-300 px-3 py-0.5 hover:bg-slate-100 disabled:opacity-60 aria-pressed:border-sky-700 aria-pressed:bg-sky-50 aria-pressed:text-sky-900 dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:border-sky-400 dark:aria-pressed:bg-sky-950 dark:aria-pressed:text-sky-100"
						>
							{tracker ? `✓ ${label}` : label}
						</button>
					);
				})}
			</div>
			<FormError error={follow.error ?? unfollow.error} />
		</div>
	);
}
