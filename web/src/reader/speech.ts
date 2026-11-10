/** One sentence of the page, in the order it is read. */
export interface Sentence {
	text: string;
}

/** What the speech reads from: the open book. */
export interface Source<S extends Sentence = Sentence> {
	/** The sentences from the top of the page to the end of its section. */
	sentences(): S[];
	/** Shows which sentence is being read, and brings it into view; null for none. */
	mark(sentence: S | null): Promise<void>;
	/** Turns to the next section of the book; false when this was the last. */
	nextSection(): Promise<boolean>;
}

export interface Speech {
	/** The voice's URI, or none for the book's language's default. */
	voice?: string;
	rate: number;
}

export const SPEECH_KEY = "gotome.reader.speech";
export const DEFAULT_SPEECH: Speech = { rate: 1 };
export const RATES = [0.75, 1, 1.25, 1.5, 2];

/** The voice and speed this browser last used, which are each browser's own. */
export function storedSpeech(): Speech {
	try {
		const raw = localStorage.getItem(SPEECH_KEY);
		return raw
			? { ...DEFAULT_SPEECH, ...(JSON.parse(raw) as Partial<Speech>) }
			: DEFAULT_SPEECH;
	} catch {
		return DEFAULT_SPEECH;
	}
}

export function storeSpeech(speech: Speech) {
	try {
		localStorage.setItem(SPEECH_KEY, JSON.stringify(speech));
	} catch {
		// Without storage the choice lasts as long as the page.
	}
}

/** Whether this browser can speak at all; whether it has voices is `voices`. */
export function canSpeak(): boolean {
	return typeof window !== "undefined" && "speechSynthesis" in window;
}

/** The voices this browser has, which some list only after the page has loaded. */
export function listVoices(): SpeechSynthesisVoice[] {
	return canSpeak() ? window.speechSynthesis.getVoices() : [];
}

export function onVoices(changed: () => void): () => void {
	if (!canSpeak()) return () => {};
	window.speechSynthesis.addEventListener("voiceschanged", changed);
	return () => window.speechSynthesis.removeEventListener("voiceschanged", changed);
}

const primary = (tag: string) => tag.toLowerCase().split(/[-_]/)[0] ?? "";

/** The chosen voice if this browser still has it, else one of the book's language. */
export function pickVoice(
	voices: SpeechSynthesisVoice[],
	language: string | undefined,
	chosen: string | undefined,
): SpeechSynthesisVoice | undefined {
	const named = voices.find((v) => v.voiceURI === chosen);
	if (named) return named;
	if (!language) return voices.find((v) => v.default) ?? voices[0];
	const exact = voices.find(
		(v) => v.lang.toLowerCase().replace("_", "-") === language.toLowerCase(),
	);
	const same = voices.filter((v) => primary(v.lang) === primary(language));
	return exact ?? same.find((v) => v.localService) ?? same[0] ?? voices.find((v) => v.default);
}

/** A span of a text, from its start to its end, without the space around. */
export interface Span {
	start: number;
	end: number;
}

/**
 * The sentences of a text. Lines are cut at first: a heading has no full
 * stop to end it at.
 */
export function sentenceSpans(text: string, language?: string): Span[] {
	const spans: Span[] = [];
	let line = 0;
	for (const part of text.split("\n")) {
		const base = line;
		line += part.length + 1;
		const pieces: Span[] = [];
		if (typeof Intl.Segmenter === "function") {
			let segmenter: Intl.Segmenter;
			try {
				segmenter = new Intl.Segmenter(language || undefined, {
					granularity: "sentence",
				});
			} catch {
				segmenter = new Intl.Segmenter(undefined, { granularity: "sentence" });
			}
			for (const s of segmenter.segment(part)) {
				pieces.push({ start: s.index, end: s.index + s.segment.length });
			}
		} else {
			for (const m of part.matchAll(/[^.!?]+[.!?]*/g)) {
				pieces.push({ start: m.index, end: m.index + m[0].length });
			}
		}
		for (const p of pieces) {
			const raw = part.slice(p.start, p.end);
			const lead = raw.length - raw.trimStart().length;
			const trimmed = raw.trim();
			if (!/[\p{L}\p{N}]/u.test(trimmed)) continue;
			spans.push({ start: base + p.start + lead, end: base + p.start + lead + trimmed.length });
		}
	}
	return spans;
}

export interface Reading {
	stop(): void;
}

/**
 * Reads the book aloud from the top of the page, a sentence at a time, and
 * on through the sections until the book ends or it is stopped. The voice
 * and the speed are asked for at each sentence, so a change applies at once.
 */
export function readAloud<S extends Sentence>(
	source: Source<S>,
	options: {
		language?: string;
		speech: () => { voice?: SpeechSynthesisVoice; rate: number };
		onEnd: () => void;
	},
): Reading {
	const synth = window.speechSynthesis;
	let stopped = false;
	let release: (() => void) | undefined;
	// Chrome drops an utterance nothing refers to before it has ended, and
	// never tells.
	const alive = new Set<SpeechSynthesisUtterance>();

	const say = (text: string) =>
		new Promise<void>((resolve) => {
			const { voice, rate } = options.speech();
			const utterance = new SpeechSynthesisUtterance(text);
			if (voice) utterance.voice = voice;
			utterance.lang = voice?.lang ?? options.language ?? "";
			utterance.rate = rate;
			const done = () => {
				alive.delete(utterance);
				release = undefined;
				resolve();
			};
			utterance.onend = done;
			utterance.onerror = done;
			release = done;
			alive.add(utterance);
			synth.speak(utterance);
		});

	const run = async () => {
		try {
			while (!stopped) {
				for (const sentence of source.sentences()) {
					if (stopped) return;
					await source.mark(sentence);
					if (stopped) return;
					await say(sentence.text);
				}
				if (stopped || !(await source.nextSection())) break;
			}
		} finally {
			await source.mark(null).catch(() => {});
			if (!stopped) options.onEnd();
		}
	};
	run().catch(() => {});

	return {
		stop() {
			if (stopped) return;
			stopped = true;
			synth.cancel();
			release?.();
		},
	};
}
