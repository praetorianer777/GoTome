import { resolveAssetsWith } from "@/api/client";
import { FakeServer } from "@/fake/server";
import type { BookDetail } from "@/books/api";
import type { DuplicatePair } from "@/duplicates/api";
import type { Timeline } from "@/player/timeline";
import { generatedCover } from "./cover";
import library from "./library.json";

interface Present {
	files: Record<string, number>;
	covers: Record<string, string>;
}

type Book = (typeof library.books)[number];
type DemoFile = { name: string; format: string; kind: string; durationMs?: number };

const DAY = 86_400_000;

async function present(base: string): Promise<Present> {
	try {
		const res = await fetch(`${base}demo/files.json`);
		if (res.ok) return (await res.json()) as Present;
	} catch {
		// Without the list the books show with metadata and generated covers.
	}
	return { files: {}, covers: {} };
}

/** The files of a book that exist: its own, or the one `make sample-books` fetched. */
function filesOf(book: Book, here: Present): { id: string; file: DemoFile; size: number }[] {
	const listed = ((book as { files?: DemoFile[] }).files ?? []).filter((f) => here.files[f.name]);
	if (listed.length > 0) {
		return listed.map((file) => ({ id: file.name, file, size: here.files[file.name]! }));
	}
	const epub = `${book.id}.epub`;
	return here.files[epub]
		? [{ id: epub, file: { name: `${book.title}.epub`, format: "epub", kind: "ebook" }, size: here.files[epub] }]
		: [];
}

function detailOf(book: Book, here: Present): Partial<BookDetail> {
	const extra = book as {
		narrators?: string[];
		translators?: string[];
		seriesIndex?: number;
		series?: string;
		subtitle?: string;
		pageCount?: number;
		rating?: number;
		startedOn?: string;
		finishedOn?: string;
	};
	const files = filesOf(book, here);
	const parts = files.filter((f) => f.file.kind === "audio");
	return {
		id: book.id,
		subtitle: extra.subtitle,
		series: extra.series,
		seriesIndex: extra.seriesIndex,
		language: book.language,
		published: book.published,
		description: book.description,
		pageCount: extra.pageCount,
		tags: book.tags,
		coverKey: book.id,
		addedAt: book.addedAt,
		placeholder: !!(book as { wished?: boolean }).wished,
		durationMs: parts.length > 0 ? parts.reduce((n, f) => n + (f.file.durationMs ?? 0), 0) : undefined,
		contributors: [
			...book.authors.map((name) => ({ name, role: "author" as const })),
			...(extra.narrators ?? []).map((name) => ({ name, role: "narrator" as const })),
			...(extra.translators ?? []).map((name) => ({ name, role: "translator" as const })),
		],
		files: files.map(({ id, file, size }, i) => ({
			id,
			kind: file.kind,
			format: file.format,
			name: file.name,
			size,
			missing: false,
			drm: false,
			extractState: "done",
			hasText: file.kind === "ebook",
			...(file.kind === "audio" ? { part: i, durationMs: file.durationMs } : {}),
		})),
		reading: {
			status: book.status as BookDetail["reading"]["status"],
			rating: extra.rating,
			startedOn: extra.startedOn,
			finishedOn: extra.finishedOn,
		},
	};
}

function timelineOf(book: Book, here: Present): Timeline | undefined {
	const parts = filesOf(book, here).filter((f) => f.file.kind === "audio");
	if (parts.length === 0) return undefined;
	let start = 0;
	const out = parts.map(({ id, file }) => {
		const part = {
			fileId: id,
			format: file.format,
			mediaType: "audio/mpeg",
			name: file.name,
			startMs: start,
			durationMs: file.durationMs ?? 0,
		};
		start += part.durationMs;
		return part;
	});
	const chapters = ((book as { chapters?: { title: string; startMs: number; endMs: number }[] }).chapters ?? []).map(
		(c) => ({ ...c, fileId: out[0]!.fileId }),
	);
	return { parts: out, chapters, durationMs: start, complete: true };
}

function statsOf(now: number) {
	const days = Array.from({ length: 120 }, (_, i) => {
		const day = new Date(now - (119 - i) * DAY).toISOString().slice(0, 10);
		const wave = (i * 7) % 11;
		return {
			date: day,
			readingMinutes: wave > 3 ? 10 + wave * 6 : 0,
			listeningMinutes: i % 9 === 0 ? 25 : 0,
			pages: wave > 3 ? 8 + wave * 5 : 0,
		};
	});
	const sum = (key: "readingMinutes" | "listeningMinutes" | "pages") => days.reduce((n, d) => n + d[key], 0);
	return {
		days,
		history: library.books
			.filter((b) => b.status === "completed")
			.map((b) => ({
				bookId: b.id,
				title: b.title,
				date: (b as { finishedOn?: string }).finishedOn ?? "2026-01-01",
				event: "finished" as const,
			})),
		readingMinutesPerDay: Math.round(sum("readingMinutes") / days.length),
		listeningMinutesPerDay: Math.round(sum("listeningMinutes") / days.length),
		pagesPerDay: Math.round(sum("pages") / days.length),
		totalReadingMinutes: sum("readingMinutes"),
		totalListeningMinutes: sum("listeningMinutes"),
		totalPages: sum("pages"),
		pagesEstimated: true,
		years: [{ year: 2026, books: library.books.filter((b) => b.status === "completed").length, finishes: 8 }],
		undated: 0,
	};
}

function build(here: Present): FakeServer {
	const server = new FakeServer()
		.withAccount(library.user, "demo", "admin")
		.signedInAs(library.user)
		.withLibrary(library.library, { writable: false });
	const lib = server.libraries[0]!;
	for (const book of library.books) {
		server.withBook(book.title, detailOf(book, here), lib.name);
		const timeline = timelineOf(book, here);
		if (timeline) server.audio[book.id] = timeline;
	}
	const now = Date.now();
	const inProgress = library.books.filter((b) => b.status === "reading" && filesOf(b, here).some((f) => f.file.kind === "ebook"));
	for (const [i, book] of inProgress.entries()) {
		const file = filesOf(book, here).find((f) => f.file.kind === "ebook")!;
		server.progress[book.id] = {
			ebook: {
				clientId: "demo",
				fileId: file.id,
				fraction: 0.4 - i * 0.1,
				locator: `fraction:${0.4 - i * 0.1}`,
				updatedAt: new Date(now - DAY).toISOString(),
			},
			finishes: 0,
		};
	}
	server.similar = library.similar;
	server.collections = library.collections.map((c) => ({
		id: c.id,
		name: c.name,
		description: (c as { description?: string }).description,
		visibility: "shared",
		mine: true,
		ownerId: `id-${library.user}`,
		ownerName: library.user,
		updatedAt: new Date(now).toISOString(),
		books: c.books,
	}));
	server.smartShelves = library.smartShelves.map((s) => ({
		id: s.id,
		name: s.name,
		filter: JSON.stringify(s.filter),
		visibility: "shared",
		mine: true,
		ownerId: `id-${library.user}`,
		ownerName: library.user,
		updatedAt: new Date(now).toISOString(),
	}));
	for (const p of library.pairs) server.withPair(p.id, p.a, p.b, p.evidence as DuplicatePair["evidence"]);
	server.trackers = library.following.map((f, i) => ({
		id: `t-${i}`,
		kind: f.kind as "author" | "series",
		name: f.name,
		createdAt: new Date(now - 30 * DAY).toISOString(),
	}));
	const release = (r: { id: string; title: string; authors: string[]; source: string; date: string; precision: string; follower: string }) => ({
		id: r.id,
		title: r.title,
		authors: r.authors,
		series: (r as { series?: string }).series,
		seriesIndex: (r as { seriesIndex?: number }).seriesIndex,
		source: r.source,
		date: r.date,
		precision: r.precision as "day" | "month",
		inLibrary: false,
		following: {
			kind: ((r as { series?: string }).series ? "series" : "author") as "series" | "author",
			name: r.follower,
		},
	});
	server.releases = {
		upcoming: library.releases.upcoming.map(release),
		recent: library.releases.recent.map(release),
	};
	server.passages = library.search.map((s) => ({
		bookId: s.book,
		hit: {
			fileId: filesOf(library.books.find((b) => b.id === s.book)!, here)[0]?.id ?? "",
			format: "epub",
			position: 1,
			chapter: s.chapter,
			offset: 0,
			snippet: [{ text: s.snippet }],
		},
	}));
	server.stats = statsOf(now);
	server.figures = {
		...server.figures,
		books: library.books.filter((b) => !(b as { wished?: boolean }).wished).length,
		files: Object.keys(here.files).length,
		bytes: Object.values(here.files).reduce((n, size) => n + size, 0),
		formats: [{ name: "epub", count: Object.keys(here.files).length, bytes: Object.values(here.files).reduce((n, s) => n + s, 0) }],
		languages: [
			{ name: "en", count: library.books.filter((b) => b.language === "en").length },
			{ name: "de", count: library.books.filter((b) => b.language === "de").length },
		],
	};
	return server;
}

/** Puts a fake server where the real one would be, before the app starts. */
export async function bootDemo(): Promise<void> {
	const base = import.meta.env.BASE_URL;
	const here = await present(base);
	const server = build(here);
	const real = globalThis.fetch.bind(globalThis);
	globalThis.fetch = (input, init) => {
		const request = input instanceof Request ? input : new Request(new URL(String(input), location.href), init);
		return new URL(request.url).pathname.startsWith("/api/v1/") ? server.handle(request) : real(request);
	};
	resolveAssetsWith((path) => {
		const cover = /^\/books\/([^/]+)\/covers\//.exec(path);
		if (cover) {
			const id = cover[1]!;
			const file = here.covers[id];
			const book = library.books.find((b) => b.id === id);
			return file ? `${base}demo/covers/${file}` : generatedCover(book?.title ?? "", book?.authors[0] ?? "");
		}
		const download = /^\/files\/([^/]+)\/download/.exec(path);
		return download ? `${base}demo/files/${download[1]}` : `${base}demo/missing`;
	});
}
