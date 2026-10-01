import { Link, useRouterState } from "@tanstack/react-router";
import { downloadUrl } from "@/books/api";
import { Cover } from "@/books/cover";
import { t } from "@/i18n";
import { chapterStep, type Sleep, usePlayer } from "@/player/player";
import { chapterAt, formatClock } from "@/player/timeline";

const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-600 dark:hover:bg-slate-800";
const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

const SKIP_MS = 30_000;
const RATES = [0.75, 1, 1.25, 1.5, 1.75, 2];
const SLEEP_MINUTES = [15, 30, 45, 60];

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
			className="sticky bottom-0 z-10 border-t border-slate-200 bg-white/95 px-4 py-2 text-sm backdrop-blur dark:border-slate-800 dark:bg-slate-950/95"
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

/** The whole player of one book: chapters, speed, sleep timer. */
export function Listen({ bookId }: { bookId: string }) {
	const player = usePlayer();
	const { state } = player;
	const mine = state.bookId === bookId;
	const timeline = mine ? state.timeline : undefined;
	const at = timeline ? chapterAt(timeline, state.positionMs) : -1;
	const stopped = !timeline || state.unsupported.length > 0;

	return (
		<div className="flex flex-col gap-6">
			<Link
				to="/books/$bookId"
				params={{ bookId }}
				className="self-start text-sm text-brand-strong underline underline-offset-4 dark:text-brand"
			>
				{t("player.backToBook")}
			</Link>
			{!mine && (
				<button
					type="button"
					className="self-start rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800"
					onClick={() => player.open(bookId)}
				>
					{state.bookId ? t("player.switch") : t("player.load")}
				</button>
			)}
			{mine && state.loading && <p className="text-slate-500">{t("loading")}</p>}
			{mine && state.error && (
				<p role="alert" className="text-red-700 dark:text-red-400">
					{t("player.failed", { reason: state.error.message })}
				</p>
			)}
			{mine && state.book && (
				<div className="grid gap-6 sm:grid-cols-[minmax(0,12rem)_1fr]">
					<div className="mx-auto w-40 sm:w-full">
						<Cover book={state.book} size="large" />
					</div>
					<div className="flex flex-col gap-4">
						<h1 className="text-2xl font-semibold">{state.book.title}</h1>
						{state.unsupported.length > 0 && (
							<div role="alert" className="rounded-md border border-red-300 bg-red-50 px-3 py-2 dark:border-red-800 dark:bg-red-950">
								<p>
									{t("player.unsupported", {
										formats: state.unsupported.map((f) => f.toUpperCase()).join(", "),
									})}
								</p>
								<ul className="mt-1 list-inside list-disc">
									{timeline?.parts.map((p) => (
										<li key={p.fileId}>
											<a href={downloadUrl({ id: p.fileId })} download className="underline">
												{t("player.download", { name: p.name })}
											</a>
										</li>
									))}
								</ul>
							</div>
						)}
						{timeline && !timeline.complete && (
							<p className="text-slate-600 dark:text-slate-400">{t("player.incomplete")}</p>
						)}
						{state.further && (
							<div
								role="status"
								className="flex flex-wrap items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm dark:border-amber-700 dark:bg-amber-950"
							>
								<span className="mr-auto">
									{t("player.further", {
										at: formatClock(state.further.positionMs ?? Number(state.further.locator)),
									})}
								</span>
								<button type="button" className={button} onClick={() => player.goFurther()}>
									{t("player.goThere")}
								</button>
								<button type="button" className={button} onClick={() => player.stayHere()}>
									{t("player.stayHere")}
								</button>
							</div>
						)}
						{timeline && (
							<>
								<label className="flex flex-col gap-1">
									<span className="sr-only">{t("player.position")}</span>
									<input
										type="range"
										min={0}
										max={Math.max(timeline.durationMs, 1)}
										step={1000}
										value={Math.min(state.positionMs, timeline.durationMs)}
										aria-valuetext={formatClock(state.positionMs)}
										disabled={stopped}
										onChange={(e) => player.seek(Number(e.target.value))}
									/>
									<span className="flex justify-between text-sm tabular-nums text-slate-600 dark:text-slate-400">
										<span data-testid="position">{formatClock(state.positionMs)}</span>
										<span>{formatClock(timeline.durationMs)}</span>
									</span>
								</label>
								<div className="flex flex-wrap items-center gap-2">
									<button type="button" className={button} disabled={stopped} onClick={() => chapterStep(state, player, -1)}>
										{t("player.previousChapter")}
									</button>
									<button type="button" className={button} aria-label={t("player.back30")} disabled={stopped} onClick={() => player.skip(-SKIP_MS)}>
										−30
									</button>
									<button
										type="button"
										className="rounded-md bg-brand-strong px-4 py-1 font-medium text-white hover:bg-sky-800 disabled:opacity-50"
										disabled={stopped}
										onClick={() => player.toggle()}
									>
										{state.playing ? t("player.pause") : t("player.play")}
									</button>
									<button type="button" className={button} aria-label={t("player.forward30")} disabled={stopped} onClick={() => player.skip(SKIP_MS)}>
										+30
									</button>
									<button type="button" className={button} disabled={stopped} onClick={() => chapterStep(state, player, 1)}>
										{t("player.nextChapter")}
									</button>
								</div>
								<div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
									<label className="flex items-center gap-2">
										<span>{t("player.speed")}</span>
										<select
											value={state.rate}
											onChange={(e) => player.setRate(Number(e.target.value))}
											className={control}
										>
											{RATES.map((r) => (
												<option key={r} value={r}>
													{`${r}×`}
												</option>
											))}
										</select>
									</label>
									<label className="flex items-center gap-2">
										<span>{t("player.sleep")}</span>
										<select
											value={sleepValue(state.sleep)}
											onChange={(e) => player.setSleep(sleepOf(e.target.value, at))}
											className={control}
										>
											<option value="off">{t("player.sleep.off")}</option>
											{SLEEP_MINUTES.map((m) => (
												<option key={m} value={String(m)}>
													{t("player.sleep.minutes", { minutes: m })}
												</option>
											))}
											<option value="chapter">{t("player.sleep.chapter")}</option>
											{state.sleep.kind === "until" && <option value="until">{t("player.sleep.at", { at: new Date(state.sleep.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) })}</option>}
										</select>
									</label>
								</div>
								<section aria-labelledby="chapters" className="flex flex-col gap-2">
									<h2 id="chapters" className="text-lg font-medium">
										{t("player.chapters")}
									</h2>
									<ol className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
										{timeline.chapters.map((c, i) => (
											<li key={`${c.fileId}-${c.startMs}`}>
												<button
													type="button"
													aria-current={i === at ? "true" : undefined}
													disabled={stopped}
													onClick={() => player.seek(c.startMs)}
													className="flex w-full justify-between gap-4 px-3 py-2 text-left hover:bg-slate-50 aria-[current=true]:font-semibold dark:hover:bg-slate-900"
												>
													<span>{c.title}</span>
													<span className="tabular-nums text-slate-500">{formatClock(c.startMs)}</span>
												</button>
											</li>
										))}
									</ol>
								</section>
							</>
						)}
					</div>
				</div>
			)}
		</div>
	);
}

function sleepValue(sleep: Sleep): string {
	return sleep.kind === "off" ? "off" : sleep.kind === "chapter" ? "chapter" : "until";
}

function sleepOf(value: string, chapter: number): Sleep {
	if (value === "off" || value === "until") return { kind: "off" };
	if (value === "chapter") return { kind: "chapter", chapter };
	return { kind: "until", at: Date.now() + Number(value) * 60_000 };
}
