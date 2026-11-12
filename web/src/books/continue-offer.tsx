import { queryOptions, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { button } from "@/components/ui";
import { t } from "@/i18n";
import { usePlayer } from "@/player/player";
import { formatClock } from "@/player/timeline";

export type Continuation = components["schemas"]["Continuation"];

/**
 * Where the person could go on in the other medium of a book: the
 * audiobook when the text is further along, the text when the audiobook is.
 * Asked again each time a page shows, as the places move while it is read
 * or heard.
 */
export function continuationQuery(bookId: string) {
	return queryOptions({
		queryKey: ["continuation", bookId],
		staleTime: 0,
		queryFn: async (): Promise<Continuation> =>
			(
				await api.GET("/books/{bookId}/continuation", {
					params: { path: { bookId } },
				})
			).data ?? {},
	});
}

/**
 * Links to continue in the other medium, for whichever one is behind (the
 * medium of the page they are on is left out with `except`). They only
 * open the other medium there; nothing is saved until it is used.
 */
export function ContinueOffers({
	bookId,
	except,
	bar = false,
}: {
	bookId: string;
	except?: "ebook" | "audio";
	/** Shown as a notice bar, as in the readers, rather than as buttons. */
	bar?: boolean;
}) {
	const player = usePlayer();
	const { data } = useQuery(continuationQuery(bookId));
	const audio = except === "audio" ? undefined : data?.audio;
	const ebook = except === "ebook" ? undefined : data?.ebook;
	if (!audio && !ebook) return null;
	const style = bar ? button("secondary", "sm") : button("secondary", "md");
	const links = (
		<>
			{audio?.positionMs !== undefined && (
				<Link
					to="/books/$bookId/listen"
					params={{ bookId }}
					className={style}
					onClick={() => player.open(bookId, { at: audio.positionMs })}
				>
					{t("continue.audio", { at: formatClock(audio.positionMs) })}
				</Link>
			)}
			{ebook?.fileId && (
				<Link
					to="/books/$bookId/read/$fileId"
					params={{ bookId, fileId: ebook.fileId }}
					search={{ fraction: ebook.fraction }}
					className={style}
				>
					{ebook.chapter
						? t("continue.ebookChapter", { chapter: ebook.chapter })
						: t("continue.ebookPercent", { percent: Math.round(ebook.fraction * 100) })}
				</Link>
			)}
		</>
	);
	if (!bar) return links;
	return (
		<div
			role="status"
			className="flex flex-wrap items-center gap-2 rounded-md border border-sky-300 bg-sky-50 px-3 py-2 text-sm dark:border-sky-700 dark:bg-sky-950"
		>
			<span className="mr-auto">
				{t(audio ? "continue.furtherAudio" : "continue.furtherText")}
			</span>
			{links}
		</div>
	);
}
