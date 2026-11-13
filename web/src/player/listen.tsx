import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { downloadUrl } from "@/books/api";
import { Cover } from "@/books/cover";
import { KeyHelp } from "@/components/dialog";
import { t } from "@/i18n";
import { ContinueOffers } from "@/books/continue-offer";
import { formatTime } from "@/lib/format";
import { type KeyBinding, useKeys } from "@/lib/keys";
import { BOOSTS } from "@/player/prefs";
import { canBoost, chapterStep, type Sleep, usePlayer } from "@/player/player";
import { chapterAt, formatClock } from "@/player/timeline";
import { UpNext } from "@/player/up-next";
import { button, control, SKIP_MS } from "@/player/controls";

const RATES = [0.75, 1, 1.25, 1.5, 1.75, 2];
const SLEEP_MINUTES = [15, 30, 45, 60];

/** The whole player of one book: chapters, speed, sleep timer. */
export function Listen({ bookId }: { bookId: string }) {
	const player = usePlayer();
	const { state } = player;
	const mine = state.bookId === bookId;
	const timeline = mine ? state.timeline : undefined;
	const at = timeline ? chapterAt(timeline, state.positionMs) : -1;
	const stopped = !timeline || state.unsupported.length > 0;
	const [help, setHelp] = useState(false);
	const rate = (step: number) => {
		const next = RATES[RATES.indexOf(state.rate) + step];
		if (next) player.setRate(next);
	};
	const bindings: KeyBinding[] =
		timeline && !stopped
			? [
					{ keys: [" ", "k"], label: "keys.playPause", run: () => player.toggle() },
					{ keys: ["ArrowLeft", "j"], label: "keys.back30", run: () => player.skip(-SKIP_MS) },
					{ keys: ["ArrowRight", "l"], label: "keys.forward30", run: () => player.skip(SKIP_MS) },
					{
						keys: ["Shift+ArrowLeft", "p"],
						label: "keys.previousChapter",
						run: () => chapterStep(state, player, -1),
					},
					{
						keys: ["Shift+ArrowRight", "n"],
						label: "keys.nextChapter",
						run: () => chapterStep(state, player, 1),
					},
					{ keys: ["Home"], label: "keys.first", run: () => player.seek(0) },
					{
						keys: ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"],
						display: ["0–9"],
						label: "keys.tenth",
						run: (e) => player.seek((timeline.durationMs * Number(e.key)) / 10),
					},
					{ keys: ["+", "="], label: "keys.faster", run: () => rate(1) },
					{ keys: ["-"], label: "keys.slower", run: () => rate(-1) },
					{ keys: ["?"], label: "keys.help", run: () => setHelp(true) },
				]
			: [];
	useKeys(bindings);

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
					className="self-start rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 dark:bg-brand dark:text-ink dark:hover:bg-sky-300"
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
						<Cover book={state.book} size="large" named />
					</div>
					<div className="flex flex-col gap-4">
						<h1 className="text-2xl font-semibold">{state.book.title}</h1>
						{timeline && (
							<p aria-live="polite" className="sr-only">
								{at >= 0 && timeline.chapters[at]
									? t("player.chapterNow", { title: timeline.chapters[at].title })
									: ""}{" "}
								{state.sleep.kind === "off"
									? ""
									: t("player.sleep.set", {
											when:
												state.sleep.kind === "chapter"
													? t("player.sleep.chapter")
													: t("player.sleep.at", { at: formatTime(state.sleep.at) }),
										})}
							</p>
						)}
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
						<ContinueOffers bookId={bookId} except="audio" bar />
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
									<button type="button" className={button} disabled={stopped} aria-keyshortcuts="P" onClick={() => chapterStep(state, player, -1)}>
										{t("player.previousChapter")}
									</button>
									<button type="button" className={button} aria-label={t("player.back30")} disabled={stopped} onClick={() => player.skip(-SKIP_MS)}>
										−30
									</button>
									<button
										type="button"
										className="rounded-md bg-brand-strong px-4 py-1 font-medium text-white hover:bg-sky-800 dark:bg-brand dark:text-ink dark:hover:bg-sky-300 disabled:opacity-50"
										disabled={stopped}
										onClick={() => player.toggle()}
									>
										{state.playing ? t("player.pause") : t("player.play")}
									</button>
									<button type="button" className={button} aria-label={t("player.forward30")} disabled={stopped} onClick={() => player.skip(SKIP_MS)}>
										+30
									</button>
									<button type="button" className={button} disabled={stopped} aria-keyshortcuts="N" onClick={() => chapterStep(state, player, 1)}>
										{t("player.nextChapter")}
									</button>
									<button type="button" className={button} disabled={stopped} aria-keyshortcuts="Shift+?" onClick={() => setHelp(true)}>
										{t("keys.button")}
									</button>
								</div>
								{help && <KeyHelp bindings={bindings} onClose={() => setHelp(false)} />}
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
											{state.sleep.kind === "until" && <option value="until">{t("player.sleep.at", { at: formatTime(state.sleep.at) })}</option>}
										</select>
									</label>
									{(state.sleep.kind !== "off" || state.resumable) && (
										<button type="button" className={button} onClick={() => player.extendSleep()}>
											{t("player.sleep.extend")}
										</button>
									)}
									{canBoost() && (
										<label className="flex items-center gap-2">
											<span>{t("player.boost")}</span>
											<select
												value={state.boost}
												onChange={(e) => player.setBoost(Number(e.target.value))}
												className={control}
											>
												{BOOSTS.map((b) => (
													<option key={b} value={b}>
														{b === 1 ? t("player.boost.off") : `${b}×`}
													</option>
												))}
											</select>
										</label>
									)}
								</div>
								<div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
									<label className="flex items-center gap-2">
										<input
											type="checkbox"
											checked={state.night.enabled}
											onChange={(e) => player.setNight({ ...state.night, enabled: e.target.checked })}
										/>
										<span>{t("player.night")}</span>
									</label>
									{state.night.enabled && (
										<>
											<label className="flex items-center gap-2">
												<span>{t("player.night.from")}</span>
												<input
													type="time"
													value={state.night.from}
													onChange={(e) => e.target.value && player.setNight({ ...state.night, from: e.target.value })}
													className={control}
												/>
											</label>
											<label className="flex items-center gap-2">
												<span>{t("player.night.to")}</span>
												<input
													type="time"
													value={state.night.to}
													onChange={(e) => e.target.value && player.setNight({ ...state.night, to: e.target.value })}
													className={control}
												/>
											</label>
											<label className="flex items-center gap-2">
												<span>{t("player.night.minutes")}</span>
												<select
													value={state.night.minutes}
													onChange={(e) => player.setNight({ ...state.night, minutes: Number(e.target.value) })}
													className={control}
												>
													{SLEEP_MINUTES.map((m) => (
														<option key={m} value={m}>
															{t("player.sleep.minutes", { minutes: m })}
														</option>
													))}
												</select>
											</label>
										</>
									)}
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
			<UpNext />
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
