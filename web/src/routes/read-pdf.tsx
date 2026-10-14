import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type FormEvent, type RefObject, useEffect, useRef, useState } from "react";
import { bookQuery, downloadUrl } from "@/books/api";
import {
	knowProgress,
	type Progress,
	progressQuery,
	useSaveProgress,
} from "@/books/progress";
import { KeyHelp } from "@/components/dialog";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { type KeyBinding, useKeys, usePanelFocus } from "@/lib/keys";
import { closestPage } from "@/reader/passage";
import { type OutlineItem, openPdf, type PdfDocument } from "@/reader/pdf";
import "@/reader/text-layer.css";

type Zoom = "width" | number;

const ZOOMS = [0.5, 0.75, 1, 1.25, 1.5, 2, 3];

function zoomedIn(zoom: Zoom): Zoom {
	const at = zoom === "width" ? 1 : zoom;
	return ZOOMS.find((z) => z > at) ?? ZOOMS.at(-1) ?? 1;
}

function zoomedOut(zoom: Zoom): Zoom {
	const at = zoom === "width" ? 1 : zoom;
	return [...ZOOMS].reverse().find((z) => z < at) ?? ZOOMS[0] ?? 1;
}

type Panel = "contents" | "search";

/** How many places a search in the file lists. */
const SEARCH_LIMIT = 100;
/** How long the page must stay before it is saved as the place. */
const SAVE_AFTER_MS = 600;

const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-600 dark:hover:bg-slate-800";

/**
 * The page a saved position names, if it is one in this file, or the page
 * as far into this one as a place saved in a file since replaced was.
 */
function pageIn(
	progress: Progress | null | undefined,
	fileId: string,
	pages?: number,
): number | undefined {
	const locator = progress?.locator ?? "";
	if (locator.startsWith("fraction:") && pages) {
		return Math.max(1, Math.round(Number(locator.slice("fraction:".length)) * pages));
	}
	const match = /^page:(\d+)$/.exec(locator);
	if (!match || progress?.fileId !== fileId) {
		return undefined;
	}
	return Number(match[1]);
}

/** A PDF of a book, a page at a time, picking up where the person left off. */
export function PdfReader({
	bookId,
	fileId,
	at,
	find,
}: {
	bookId: string;
	fileId: string;
	/** A page to open at instead of the saved place. */
	at?: number;
	/** A passage to look for on the pages from at to to, to open at its page. */
	find?: { word: string; passage: string; to?: number };
}) {
	const book = useQuery(bookQuery(bookId));
	const progress = useQuery(progressQuery(bookId));
	const queryClient = useQueryClient();
	const save = useSaveProgress(bookId, "ebook");
	const { mutate: savePlace } = save;
	const [doc, setDoc] = useState<PdfDocument>();
	const [failed, setFailed] = useState<Error | null>(null);
	const [page, setPage] = useState<number>();
	const [zoom, setZoom] = useState<Zoom>("width");
	const [panel, setPanel] = useState<Panel>();
	const [help, setHelp] = useState(false);
	const toggles = useRef<Partial<Record<Panel, HTMLButtonElement | null>>>({});
	const panelBox = usePanelFocus(panel);
	const pageInput = useRef<HTMLInputElement>(null);
	const [further, setFurther] = useState<Progress>();
	// The page last saved or found saved, which needs no saving again.
	const kept = useRef<number | undefined>(undefined);

	useEffect(() => {
		let open = true;
		let opened: PdfDocument | undefined;
		openPdf(downloadUrl({ id: fileId })).then(
			(d) => {
				if (open) {
					opened = d;
					setDoc(d);
				} else {
					d.close();
				}
			},
			(err: unknown) => {
				if (open)
					setFailed(err instanceof Error ? err : new Error(String(err)));
			},
		);
		return () => {
			open = false;
			opened?.close();
		};
	}, [fileId]);

	// A passage's pages are read for its words; the page that holds them is
	// where the reader opens.
	const [found, setFound] = useState<number>();
	const wanted = useRef(find);
	useEffect(() => {
		const passage = wanted.current;
		if (!doc || !passage || at === undefined) return;
		let open = true;
		const last = Math.min(Math.max(passage.to ?? at, at), doc.pages);
		(async () => {
			const pages = [];
			for (let n = at; n <= last; n++) {
				pages.push({ page: n, text: await doc.text(n) });
			}
			return closestPage(pages, passage.word, passage.passage) ?? at;
		})().then(
			(n) => open && setFound(n),
			() => open && setFound(at),
		);
		return () => {
			open = false;
		};
	}, [doc, at]);
	const target = find && at !== undefined ? found : at;

	// Once the file and the saved place are both known, the reader starts
	// there, or at the page asked for, which is not saved until the person
	// moves on from it.
	if (
		doc &&
		!progress.isPending &&
		page === undefined &&
		(target !== undefined || at === undefined)
	) {
		const resumed = pageIn(progress.data?.ebook, fileId, doc.pages);
		const start = Math.min(Math.max(target ?? resumed ?? 1, 1), doc.pages);
		kept.current = target === undefined ? resumed : start;
		setPage(start);
	}

	const go = (n: number) => {
		if (doc) setPage(Math.min(Math.max(n, 1), doc.pages));
	};

	useEffect(() => {
		if (!doc || page === undefined || page === kept.current) {
			return;
		}
		const timer = setTimeout(() => {
			kept.current = page;
			savePlace(
				{ fileId, locator: `page:${page}`, page, fraction: page / doc.pages },
				{
					onSuccess: (answer) => {
						setFurther(
							answer && !answer.saved && answer.progress
								? answer.progress
								: undefined,
						);
					},
				},
			);
		}, SAVE_AFTER_MS);
		return () => clearTimeout(timer);
	}, [doc, page, fileId, savePlace]);

	const show = (which: Panel | undefined) => {
		// Closing gives focus back to the button that opened the panel.
		if (!which && panel) toggles.current[panel]?.focus();
		setPanel(which);
	};
	const toggle = (which: Panel) => show(panel === which ? undefined : which);
	const now = page ?? 1;
	const bindings: KeyBinding[] = doc
		? [
				{ keys: ["ArrowRight", "PageDown", " "], label: "keys.pageNext", run: () => go(now + 1) },
				{ keys: ["ArrowLeft", "PageUp", "Shift+ "], label: "keys.pagePrevious", run: () => go(now - 1) },
				{ keys: ["Home"], label: "keys.first", run: () => go(1) },
				{ keys: ["End"], label: "keys.last", run: () => go(doc.pages) },
				{
					keys: ["g"],
					label: "keys.goToPage",
					run: () => {
						pageInput.current?.focus();
						pageInput.current?.select();
					},
				},
				{ keys: ["c"], label: "keys.contents", run: () => toggle("contents") },
				{ keys: ["f"], label: "keys.search", run: () => toggle("search") },
				{ keys: ["+", "="], label: "keys.zoomIn", run: () => setZoom(zoomedIn(zoom)) },
				{ keys: ["-"], label: "keys.zoomOut", run: () => setZoom(zoomedOut(zoom)) },
				{ keys: ["0"], label: "keys.fitWidth", run: () => setZoom("width") },
				{ keys: ["Escape"], label: "keys.closePanel", run: () => show(undefined) },
				{ keys: ["?"], label: "keys.help", run: () => setHelp(true) },
			]
		: [];
	useKeys(bindings);

	const title = book.data?.title ?? "";
	const furtherPage = pageIn(further, fileId);

	return (
		<div className="flex flex-col gap-3">
			<div className="flex flex-wrap items-center gap-2 text-sm">
				<Link
					to="/books/$bookId"
					params={{ bookId }}
					className="text-brand-strong underline underline-offset-4 dark:text-brand"
				>
					{t("reader.back")}
				</Link>
				<h1 className="mr-auto min-w-0 truncate font-medium">{title}</h1>
				<button
					ref={(el) => {
						toggles.current.contents = el;
					}}
					type="button"
					className={button}
					aria-expanded={panel === "contents"}
					aria-controls="reader-panel"
					aria-keyshortcuts="C"
					disabled={!doc}
					onClick={() => toggle("contents")}
				>
					{t("reader.outline")}
				</button>
				<button
					ref={(el) => {
						toggles.current.search = el;
					}}
					type="button"
					className={button}
					aria-expanded={panel === "search"}
					aria-controls="reader-panel"
					aria-keyshortcuts="F"
					disabled={!doc}
					onClick={() => toggle("search")}
				>
					{t("reader.search")}
				</button>
				<button
					type="button"
					className={button}
					aria-keyshortcuts="Shift+?"
					disabled={!doc}
					onClick={() => setHelp(true)}
				>
					{t("keys.button")}
				</button>
				<ZoomControls zoom={zoom} onZoom={setZoom} disabled={!doc} />
				<PageControls page={page} pages={doc?.pages} onGo={go} input={pageInput} />
			</div>
			{help && <KeyHelp bindings={bindings} onClose={() => setHelp(false)} />}
			<p aria-live="polite" className="sr-only">
				{page !== undefined && title ? t("reader.page", { page, title }) : ""}
			</p>
			{further && (
				<div
					role="status"
					className="flex flex-wrap items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm dark:border-amber-700 dark:bg-amber-950"
				>
					<span className="mr-auto">
						{furtherPage
							? t("reader.further", { page: furtherPage })
							: t("reader.furtherElsewhere", {
									percent: Math.round(further.fraction * 100),
								})}
					</span>
					{furtherPage && (
						<button
							type="button"
							className={button}
							onClick={() => {
								// What this browser now knows is that place.
								knowProgress(queryClient, bookId, "ebook", further);
								kept.current = furtherPage;
								setFurther(undefined);
								go(furtherPage);
							}}
						>
							{t("reader.goThere")}
						</button>
					)}
					<button
						type="button"
						className={button}
						onClick={() => {
							setFurther(undefined);
							if (doc && page) {
								save.mutate({
									fileId,
									locator: `page:${page}`,
									page,
									fraction: page / doc.pages,
									force: true,
								});
							}
						}}
					>
						{t("reader.stayHere")}
					</button>
				</div>
			)}
			<FormError error={failed ?? book.error} />
			{!doc && !failed && (
				<p className="text-slate-500">{t("reader.opening")}</p>
			)}
			<div className="flex gap-4">
				{doc && panel && (
					<div
						id="reader-panel"
						ref={panelBox}
						className="w-64 shrink-0 overflow-auto text-sm"
					>
						{panel === "contents" ? (
							<Outline doc={doc} current={page} onGo={(n) => go(n)} />
						) : (
							<PdfSearch doc={doc} onGo={(n) => go(n)} />
						)}
					</div>
				)}
				{doc && page !== undefined && (
					<Page doc={doc} page={page} zoom={zoom} title={title} />
				)}
			</div>
		</div>
	);
}

function PageControls({
	page,
	pages,
	onGo,
	input,
}: {
	page?: number;
	pages?: number;
	onGo: (page: number) => void;
	input: RefObject<HTMLInputElement | null>;
}) {
	const [typed, setTyped] = useState<string>();
	return (
		<div className="flex items-center gap-2">
			<button
				type="button"
				className={button}
				disabled={!page || page <= 1}
				onClick={() => page && onGo(page - 1)}
			>
				{t("reader.previous")}
			</button>
			<form
				className="flex items-center gap-1"
				onSubmit={(e) => {
					e.preventDefault();
					const n = Number(typed);
					if (Number.isInteger(n)) onGo(n);
					setTyped(undefined);
				}}
			>
				<input
					ref={input}
					aria-label={t("reader.pageNumber")}
					aria-keyshortcuts="G"
					inputMode="numeric"
					value={typed ?? String(page ?? "")}
					onChange={(e) => setTyped(e.target.value)}
					onBlur={() => setTyped(undefined)}
					disabled={!pages}
					className="w-14 rounded-md border border-slate-300 bg-white px-2 py-1 text-right dark:border-slate-600 dark:bg-slate-900"
				/>
				<span>{t("reader.of", { pages: pages ?? "…" })}</span>
			</form>
			<button
				type="button"
				className={button}
				disabled={!page || !pages || page >= pages}
				onClick={() => page && onGo(page + 1)}
			>
				{t("reader.next")}
			</button>
		</div>
	);
}

function ZoomControls({
	zoom,
	onZoom,
	disabled,
}: {
	zoom: Zoom;
	onZoom: (zoom: Zoom) => void;
	disabled: boolean;
}) {
	const at = zoom === "width" ? undefined : zoom;
	return (
		<div className="flex items-center gap-1">
			<button
				type="button"
				className={button}
				disabled={disabled || at === ZOOMS[0]}
				aria-keyshortcuts="-"
				onClick={() => onZoom(zoomedOut(zoom))}
			>
				{t("reader.zoomOut")}
			</button>
			<button
				type="button"
				className={button}
				aria-pressed={zoom === "width"}
				aria-keyshortcuts="0"
				disabled={disabled}
				onClick={() => onZoom("width")}
			>
				{zoom === "width"
					? t("reader.fitWidth")
					: t("reader.zoomAt", { percent: Math.round((at ?? 1) * 100) })}
			</button>
			<button
				type="button"
				className={button}
				disabled={disabled || at === ZOOMS.at(-1)}
				aria-keyshortcuts="+"
				onClick={() => onZoom(zoomedIn(zoom))}
			>
				{t("reader.zoomIn")}
			</button>
		</div>
	);
}

function Page({
	doc,
	page,
	zoom,
	title,
}: {
	doc: PdfDocument;
	page: number;
	zoom: Zoom;
	title: string;
}) {
	const frame = useRef<HTMLDivElement>(null);
	const canvas = useRef<HTMLCanvasElement>(null);
	const text = useRef<HTMLDivElement>(null);
	const [width, setWidth] = useState(0);
	const [drawn, setDrawn] = useState<number>();
	const [failed, setFailed] = useState<Error | null>(null);

	useEffect(() => {
		const el = frame.current;
		if (!el) return;
		setWidth(el.clientWidth);
		if (typeof ResizeObserver === "undefined") return;
		// A few pixels are not worth drawing the page again for.
		const observer = new ResizeObserver(() =>
			setWidth((was) => (Math.abs(was - el.clientWidth) < 8 ? was : el.clientWidth)),
		);
		observer.observe(el);
		return () => observer.disconnect();
	}, []);

	useEffect(() => {
		if (!canvas.current || !text.current || width === 0) return;
		const into = { canvas: canvas.current, text: text.current };
		let cancel = () => {};
		let stopped = false;
		(async () => {
			const scale =
				zoom === "width" ? width / (await doc.pageWidth(page)) : zoom;
			if (stopped) return;
			const task = doc.render(page, scale, into.canvas, into.text);
			cancel = task.cancel;
			await task.done;
			if (!stopped) setDrawn(page);
		})().catch((err: unknown) => {
			if (!stopped)
				setFailed(err instanceof Error ? err : new Error(String(err)));
		});
		return () => {
			stopped = true;
			cancel();
		};
	}, [doc, page, zoom, width]);

	return (
		<div ref={frame} className="min-w-0 flex-1 overflow-auto">
			<FormError error={failed} />
			<figure
				aria-label={t("reader.page", { page, title })}
				aria-busy={drawn !== page}
				data-page={drawn}
				className="relative mx-auto w-fit bg-white shadow"
			>
				<canvas ref={canvas} />
				<div ref={text} className="textLayer" />
			</figure>
		</div>
	);
}

interface PageFound {
	page: number;
	pre: string;
	match: string;
	post: string;
}

/** Where the words stand on a page's text, with some of the text around them. */
function placesOn(page: number, text: string, words: string): PageFound[] {
	const found: PageFound[] = [];
	const lower = text.toLocaleLowerCase();
	const needle = words.toLocaleLowerCase();
	for (let at = lower.indexOf(needle); at >= 0; at = lower.indexOf(needle, at + needle.length)) {
		found.push({
			page,
			pre: text.slice(Math.max(0, at - 30), at),
			match: text.slice(at, at + needle.length),
			post: text.slice(at + needle.length, at + needle.length + 30),
		});
	}
	return found;
}

/** Looks for words on every page, a page at a time, and lists where they stand. */
function PdfSearch({ doc, onGo }: { doc: PdfDocument; onGo: (page: number) => void }) {
	const [query, setQuery] = useState("");
	const [found, setFound] = useState<PageFound[]>();
	const [reading, setReading] = useState<number>();
	const run = useRef(0);
	useEffect(
		() => () => {
			run.current++;
		},
		[],
	);
	const submit = async (e: FormEvent) => {
		e.preventDefault();
		const words = query.trim();
		if (!words) return;
		const mine = ++run.current;
		const places: PageFound[] = [];
		setFound(undefined);
		for (let n = 1; n <= doc.pages && places.length < SEARCH_LIMIT; n++) {
			setReading(n);
			try {
				places.push(...placesOn(n, await doc.text(n), words));
			} catch {
				// A page whose text cannot be read holds nothing to find.
			}
			if (run.current !== mine) return;
		}
		setReading(undefined);
		setFound(places.slice(0, SEARCH_LIMIT));
	};
	return (
		<div className="flex flex-col gap-2">
			<form role="search" className="flex gap-2" onSubmit={(e) => void submit(e)}>
				<label className="sr-only" htmlFor="pdf-search">
					{t("reader.searchLabel")}
				</label>
				<input
					id="pdf-search"
					type="search"
					value={query}
					onChange={(e) => setQuery(e.target.value)}
					className="min-w-0 flex-1 rounded-md border border-slate-300 bg-white px-2 py-1 dark:border-slate-600 dark:bg-slate-900"
				/>
				<button type="submit" className={button}>
					{t("reader.searchSubmit")}
				</button>
			</form>
			<p role="status" className="text-slate-600 dark:text-slate-400">
				{reading !== undefined
					? t("reader.searchingPage", { page: reading, pages: doc.pages })
					: found === undefined
						? ""
						: found.length === 0
							? t("reader.searchNone")
							: t("reader.searchFound", { count: found.length })}
			</p>
			{found && found.length > 0 && (
				<ol className="flex flex-col gap-1">
					{found.map((f, i) => (
						<li key={`${f.page}-${i}`}>
							<button type="button" className="text-left hover:underline" onClick={() => onGo(f.page)}>
								<span className="font-medium">{t("fulltext.page", { page: f.page })}:</span>{" "}
								{f.pre}
								<mark>{f.match}</mark>
								{f.post}
							</button>
						</li>
					))}
				</ol>
			)}
		</div>
	);
}

function Outline({
	doc,
	current,
	onGo,
}: {
	doc: PdfDocument;
	current?: number;
	onGo: (page: number) => void;
}) {
	const [items, setItems] = useState<OutlineItem[]>();
	useEffect(() => {
		let open = true;
		doc.outline().then(
			(found) => open && setItems(found),
			() => open && setItems([]),
		);
		return () => {
			open = false;
		};
	}, [doc]);
	return (
		<nav aria-label={t("reader.outline")}>
			{items === undefined && <p className="text-slate-500">{t("loading")}</p>}
			{items?.length === 0 && (
				<p className="text-slate-500">{t("reader.noOutline")}</p>
			)}
			{items && items.length > 0 && (
				<OutlineList items={items} current={current} onGo={onGo} />
			)}
		</nav>
	);
}

function OutlineList({
	items,
	current,
	onGo,
}: {
	items: OutlineItem[];
	current?: number;
	onGo: (page: number) => void;
}) {
	return (
		<ul className="flex flex-col gap-1 pl-3 first:pl-0">
			{items.map((item) => (
				<li key={item.id}>
					{item.page ? (
						<button
							type="button"
							aria-current={item.page === current ? "page" : undefined}
							onClick={() => item.page && onGo(item.page)}
							className="text-left hover:underline aria-[current=page]:font-semibold"
						>
							{item.title}
						</button>
					) : (
						<span>{item.title}</span>
					)}
					{item.items.length > 0 && (
						<OutlineList items={item.items} current={current} onGo={onGo} />
					)}
				</li>
			))}
		</ul>
	);
}
