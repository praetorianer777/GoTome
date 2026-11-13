import { Link, useRouterState } from "@tanstack/react-router";
import { Cover } from "@/books/cover";
import { t } from "@/i18n";
import { usePlayer } from "@/player/player";
import { chapterAt, formatClock } from "@/player/timeline";

export const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-600 dark:hover:bg-slate-800";
export const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

export const SKIP_MS = 30_000;

/**
 * The player along the bottom of every page while a book is loaded, but
 * that book's own player page.
 */
export function MiniPlayer() {
	const player = usePlayer();
	const { state } = player;
	const path = useRouterState({ select: (s) => s.location.pathname });
	if (!state.bookId || !state.book || path === `/books/${state.bookId}/listen`) {
		return null;
	}
	const chapter = state.timeline?.chapters[chapterAt(state.timeline, state.positionMs)];
	return (
		<section
			aria-label={t("player.mini")}
			className="sticky bottom-0 z-10 border-t border-slate-200 bg-white/95 px-4 pt-2 pb-safe-2 text-sm lg:backdrop-blur dark:border-slate-800 dark:bg-slate-950/95"
		>
			<div className="mx-auto flex max-w-6xl items-center gap-3">
				<div className="w-10 shrink-0">
					<Cover book={state.book} size="small" />
				</div>
				<div className="min-w-0 flex-1">
					<Link
						to="/books/$bookId/listen"
						params={{ bookId: state.bookId }}
						className="block truncate font-medium hover:underline"
					>
						{state.book.title}
					</Link>
					<span className="block truncate text-slate-600 dark:text-slate-400">
						{chapter ? `${chapter.title} · ` : ""}
						{formatClock(state.positionMs)}
					</span>
				</div>
				<button
					type="button"
					className={button}
					aria-label={t("player.back30")}
					disabled={!state.timeline || state.unsupported.length > 0}
					onClick={() => player.skip(-SKIP_MS)}
				>
					−30
				</button>
				<button
					type="button"
					className={button}
					disabled={!state.timeline || state.unsupported.length > 0}
					onClick={() => player.toggle()}
				>
					{state.playing ? t("player.pause") : t("player.play")}
				</button>
				<button
					type="button"
					className={button}
					aria-label={t("player.forward30")}
					disabled={!state.timeline || state.unsupported.length > 0}
					onClick={() => player.skip(SKIP_MS)}
				>
					+30
				</button>
				<button type="button" className={button} aria-label={t("player.close")} onClick={() => player.close()}>
					×
				</button>
			</div>
		</section>
	);
}
