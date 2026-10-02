import { useQueryClient } from "@tanstack/react-query";
import {
	createContext,
	type ReactNode,
	useContext,
	useEffect,
	useRef,
	useState,
} from "react";
import { type BookDetail, bookQuery, coverUrl, downloadUrl } from "@/books/api";
import {
	knowProgress,
	type Progress,
	progressQuery,
	saveProgress,
} from "@/books/progress";
import {
	chapterAt,
	partAt,
	type Timeline,
	timelineQuery,
} from "@/player/timeline";

/** When the player stops by itself: never, at a time, or when a chapter ends. */
export type Sleep =
	| { kind: "off" }
	| { kind: "until"; at: number }
	| { kind: "chapter"; chapter: number };

export interface PlayerState {
	bookId?: string;
	book?: BookDetail;
	timeline?: Timeline;
	loading: boolean;
	error?: Error;
	/** The formats of the book's parts this browser says it cannot play. */
	unsupported: string[];
	positionMs: number;
	playing: boolean;
	rate: number;
	sleep: Sleep;
	/** A further place another device saved, which saving here met. */
	further?: Progress;
}

export interface Player {
	state: PlayerState;
	/** Loads a book at its saved place, without playing it yet. */
	open(bookId: string): void;
	play(): void;
	pause(): void;
	toggle(): void;
	/** Goes to a time of the whole book, whichever part it is in. */
	seek(ms: number): void;
	skip(deltaMs: number): void;
	setRate(rate: number): void;
	setSleep(sleep: Sleep): void;
	goFurther(): void;
	stayHere(): void;
	close(): void;
}

// Every field named, so that spreading it over a state clears that state.
const INITIAL: PlayerState = {
	bookId: undefined,
	book: undefined,
	timeline: undefined,
	error: undefined,
	further: undefined,
	loading: false,
	unsupported: [],
	positionMs: 0,
	playing: false,
	rate: 1,
	sleep: { kind: "off" },
};

/** How often the place is saved while playing. */
const SAVE_EVERY_MS = 15_000;

const PlayerContext = createContext<Player | null>(null);

export function usePlayer(): Player {
	const player = useContext(PlayerContext);
	if (!player) {
		throw new Error("usePlayer needs a PlayerProvider");
	}
	return player;
}

/**
 * The one audio player of the app. It lives above the pages, so that a book
 * goes on playing while the person looks elsewhere. A book in several files
 * plays as one recording: the player keeps the time of the whole and loads
 * the part a time falls in.
 */
export function PlayerProvider({ children }: { children: ReactNode }) {
	const queryClient = useQueryClient();
	const audio = useRef<HTMLAudioElement>(null);
	const [state, setState] = useState<PlayerState>(INITIAL);
	// The state as the actions last left it, ahead of what has rendered.
	const current = useRef(state);
	// Which part is in the audio element, and a seek and play that wait for
	// it to load.
	const part = useRef(-1);
	const pending = useRef<{ seconds: number; play: boolean } | null>(null);
	const lastSaved = useRef<number | null>(null);
	// The listeners are set once; these let them reach the helpers the
	// actions are made of.
	const saveRef = useRef<(opts?: { force?: boolean; keepalive?: boolean }) => void>(() => {});
	const loadRef = useRef<(ms: number, play: boolean) => void>(() => {});

	// Made once: it reads and writes only refs and the state setter.
	const updateRef = useRef((change: Partial<PlayerState>) => {
		current.current = { ...current.current, ...change };
		setState(current.current);
	});
	const update = updateRef.current;

	const actions = useRef<Omit<Player, "state">>(
		null as unknown as Omit<Player, "state">,
	);
	if (actions.current === null) {
		const startPlaying = () => {
			audio.current?.play().catch(() => update({ playing: false }));
		};
		const load = (ms: number, play: boolean) => {
			const el = audio.current;
			const timeline = current.current.timeline;
			if (!el || !timeline || timeline.parts.length === 0) return;
			const at = partAt(timeline, ms);
			const p = timeline.parts[at.index];
			if (!p) return;
			update({ positionMs: p.startMs + at.offsetMs });
			if (part.current !== at.index) {
				part.current = at.index;
				pending.current = { seconds: at.offsetMs / 1000, play };
				el.src = downloadUrl({ id: p.fileId });
				el.load();
				// At once, not once the part has loaded: a browser lets a
				// page play while the person's click is being handled, and
				// may refuse it later.
				if (play) startPlaying();
				return;
			}
			el.currentTime = at.offsetMs / 1000;
			if (play) startPlaying();
		};
		const save = (opts: { force?: boolean; keepalive?: boolean } = {}) => {
			const s = current.current;
			const timeline = s.timeline;
			const p = timeline?.parts[part.current];
			if (!s.bookId || !timeline || !p || s.unsupported.length > 0) return;
			const ms = Math.round(s.positionMs);
			if (
				!opts.force &&
				lastSaved.current !== null &&
				Math.abs(lastSaved.current - ms) < 1000
			)
				return;
			lastSaved.current = ms;
			const chapter = timeline.chapters[chapterAt(timeline, ms)];
			const bookId = s.bookId;
			void saveProgress(
				queryClient,
				bookId,
				"audio",
				{
					fileId: p.fileId,
					locator: String(ms),
					positionMs: ms,
					fraction:
						timeline.durationMs > 0 ? Math.min(ms / timeline.durationMs, 1) : 0,
					chapter: chapter?.title,
					force: opts.force,
				},
				opts.keepalive,
			).then(
				(answer) => {
					if (
						answer &&
						!answer.saved &&
						answer.progress &&
						current.current.bookId === bookId
					) {
						update({ further: answer.progress });
					}
				},
				() => {
					// Not saved this time; the next save carries the place.
					lastSaved.current = null;
				},
			);
		};
		actions.current = {
			open(bookId) {
				if (current.current.bookId === bookId) return;
				if (current.current.bookId) actions.current.close();
				update({
					...INITIAL,
					rate: current.current.rate,
					bookId,
					loading: true,
				});
				Promise.all([
					queryClient.fetchQuery(bookQuery(bookId)),
					queryClient.fetchQuery(timelineQuery(bookId)),
					queryClient.fetchQuery(progressQuery(bookId)),
				]).then(
					([book, timeline, progress]) => {
						if (current.current.bookId !== bookId || !timeline) return;
						const el = audio.current;
						const unsupported = [
							...new Set(
								timeline.parts
									.filter((p) => el?.canPlayType(p.mediaType) === "")
									.map((p) => p.format),
							),
						];
						const saved = progress.audio?.positionMs ?? 0;
						const start = saved < timeline.durationMs ? saved : 0;
						lastSaved.current = progress.audio ? saved : null;
						update({
							book,
							timeline,
							loading: false,
							unsupported,
							positionMs: start,
						});
						if (unsupported.length === 0) load(start, false);
					},
					(err: unknown) => {
						if (current.current.bookId !== bookId) return;
						update({
							loading: false,
							error: err instanceof Error ? err : new Error(String(err)),
						});
					},
				);
			},
			play() {
				if (part.current < 0) {
					load(current.current.positionMs, true);
				} else {
					startPlaying();
				}
			},
			pause() {
				audio.current?.pause();
			},
			toggle() {
				if (current.current.playing) actions.current.pause();
				else actions.current.play();
			},
			seek(ms) {
				const timeline = current.current.timeline;
				if (!timeline) return;
				load(
					Math.min(Math.max(ms, 0), timeline.durationMs),
					current.current.playing,
				);
				save();
			},
			skip(deltaMs) {
				actions.current.seek(current.current.positionMs + deltaMs);
			},
			setRate(rate) {
				if (audio.current) audio.current.playbackRate = rate;
				update({ rate });
			},
			setSleep(sleep) {
				update({ sleep });
			},
			goFurther() {
				const s = current.current;
				const further = s.further;
				if (!further || !s.bookId) return;
				knowProgress(queryClient, s.bookId, "audio", further);
				lastSaved.current = further.positionMs ?? Number(further.locator);
				update({ further: undefined });
				load(lastSaved.current, s.playing);
			},
			stayHere() {
				update({ further: undefined });
				save({ force: true });
			},
			close() {
				save();
				const el = audio.current;
				if (el) {
					el.pause();
					el.removeAttribute("src");
					el.load();
				}
				part.current = -1;
				pending.current = null;
				lastSaved.current = null;
				update({ ...INITIAL, rate: current.current.rate });
			},
		};
		saveRef.current = save;
		loadRef.current = load;
	}

	useEffect(() => {
		const el = audio.current;
		if (!el) return;
		const update = updateRef.current;
		const timeline = () => current.current.timeline;
		const onMetadata = () => {
			el.playbackRate = current.current.rate;
			const wait = pending.current;
			pending.current = null;
			if (wait) {
				el.currentTime = wait.seconds;
				// Asked again: WebKit now and then drops a play asked for
				// before the part had loaded.
				if (wait.play && el.paused) {
					el.play().catch(() => update({ playing: false }));
				}
			}
		};
		const onTime = () => {
			const tl = timeline();
			const p = tl?.parts[part.current];
			if (!tl || !p || pending.current) return;
			const ms = p.startMs + el.currentTime * 1000;
			update({ positionMs: ms });
			const sleep = current.current.sleep;
			if (
				(sleep.kind === "until" && Date.now() >= sleep.at) ||
				(sleep.kind === "chapter" && chapterAt(tl, ms) !== sleep.chapter)
			) {
				update({ sleep: { kind: "off" } });
				el.pause();
			}
			if (
				"mediaSession" in navigator &&
				tl.durationMs > 0 &&
				Number.isFinite(el.playbackRate)
			) {
				try {
					navigator.mediaSession.setPositionState({
						duration: tl.durationMs / 1000,
						position: Math.min(ms, tl.durationMs) / 1000,
						playbackRate: el.playbackRate,
					});
				} catch {
					// A browser that refuses the state still plays.
				}
			}
		};
		const onEnded = () => {
			const tl = timeline();
			const next = tl?.parts[part.current + 1];
			if (tl && next) {
				loadRef.current(next.startMs, true);
				return;
			}
			if (tl) update({ positionMs: tl.durationMs, playing: false });
			saveRef.current();
		};
		const onPlay = () => update({ playing: true });
		const onPause = () => {
			update({ playing: false });
			saveRef.current();
		};
		const onError = () => {
			const tl = timeline();
			const p = tl?.parts[part.current];
			if (!p || !el.error) return;
			if (el.error.code === MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED) {
				update({ unsupported: [p.format], playing: false });
			} else {
				update({
					error: new Error(el.error.message || `Could not play ${p.name}.`),
					playing: false,
				});
			}
		};
		el.addEventListener("loadedmetadata", onMetadata);
		el.addEventListener("timeupdate", onTime);
		el.addEventListener("ended", onEnded);
		el.addEventListener("play", onPlay);
		el.addEventListener("pause", onPause);
		el.addEventListener("error", onError);
		const onHide = () => saveRef.current({ keepalive: true });
		window.addEventListener("pagehide", onHide);
		return () => {
			el.removeEventListener("loadedmetadata", onMetadata);
			el.removeEventListener("timeupdate", onTime);
			el.removeEventListener("ended", onEnded);
			el.removeEventListener("play", onPlay);
			el.removeEventListener("pause", onPause);
			el.removeEventListener("error", onError);
			window.removeEventListener("pagehide", onHide);
		};
	}, []);

	useEffect(() => {
		if (!state.playing) return;
		const timer = setInterval(() => saveRef.current(), SAVE_EVERY_MS);
		return () => clearInterval(timer);
	}, [state.playing]);

	const { book } = state;
	useEffect(() => {
		if (!("mediaSession" in navigator) || typeof MediaMetadata === "undefined")
			return;
		const session = navigator.mediaSession;
		if (!book) {
			session.metadata = null;
			return;
		}
		const cover = coverUrl(book, "large");
		session.metadata = new MediaMetadata({
			title: book.title,
			artist: book.contributors
				.filter((c) => c.role === "author")
				.map((c) => c.name)
				.join(", "),
			artwork: cover ? [{ src: cover, type: "image/jpeg" }] : [],
		});
		const a = actions.current;
		const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
			["play", () => a.play()],
			["pause", () => a.pause()],
			["seekbackward", (d) => a.skip(-(d.seekOffset ?? 30) * 1000)],
			["seekforward", (d) => a.skip((d.seekOffset ?? 30) * 1000)],
			["seekto", (d) => d.seekTime != null && a.seek(d.seekTime * 1000)],
			["previoustrack", () => chapterStep(current.current, a, -1)],
			["nexttrack", () => chapterStep(current.current, a, 1)],
		];
		for (const [action, handler] of handlers) {
			try {
				session.setActionHandler(action, handler);
			} catch {
				// An action this browser does not know.
			}
		}
		return () => {
			for (const [action] of handlers) {
				try {
					session.setActionHandler(action, null);
				} catch {
					// As above.
				}
			}
		};
	}, [book]);

	return (
		<PlayerContext.Provider value={{ state, ...actions.current }}>
			{children}
			{/* biome-ignore lint/a11y/useMediaCaption: an audiobook has no captions to give. */}
			<audio
				ref={audio}
				preload="metadata"
				data-part={state.timeline ? part.current : undefined}
			/>
		</PlayerContext.Provider>
	);
}

/** Goes to the start of the next or the previous chapter. Back within the
 * first seconds of a chapter goes to the one before, as a player's back
 * button does. */
export function chapterStep(
	state: PlayerState,
	player: Pick<Player, "seek">,
	step: 1 | -1,
) {
	const tl = state.timeline;
	if (!tl) return;
	const at = chapterAt(tl, state.positionMs);
	const here = tl.chapters[at];
	let target = at + step;
	if (step < 0 && here && state.positionMs - here.startMs > 3000) {
		target = at;
	}
	const chapter = tl.chapters[Math.max(target, 0)];
	if (chapter && target < tl.chapters.length) player.seek(chapter.startMs);
}
