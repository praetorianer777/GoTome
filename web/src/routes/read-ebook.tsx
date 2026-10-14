import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { downloadUrl } from "@/books/api";
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
import {
	type Ebook,
	type Flow,
	type Look,
	type Place,
	type Theme,
	type TocEntry,
	openEbook,
} from "@/reader/ebook";
import type { Found } from "@/reader/passage";

/** How long a place must stay before it is saved. */
const SAVE_AFTER_MS = 600;
const LOOK_KEY = "gotome.reader.look";
const DEFAULT_LOOK: Look = { flow: "paginated", theme: "light", fontSize: 100 };
const FONT_SIZES = [80, 90, 100, 110, 120, 140, 160, 200];

const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-600 dark:hover:bg-slate-800";
const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

/** The look this browser last used, which is each browser's own. */
function storedLook(): Look {
	try {
		const raw = localStorage.getItem(LOOK_KEY);
		return raw
			? { ...DEFAULT_LOOK, ...(JSON.parse(raw) as Partial<Look>) }
			: DEFAULT_LOOK;
	} catch {
		return DEFAULT_LOOK;
	}
}

function storeLook(look: Look) {
	try {
		localStorage.setItem(LOOK_KEY, JSON.stringify(look));
	} catch {
		// Without storage the look lasts as long as the page.
	}
}

/** The CFI a saved position names, if it is one in this file. */
function cfiIn(
	progress: Progress | null | undefined,
	fileId: string,
): string | undefined {
	return progress?.fileId === fileId && progress.locator.startsWith("epubcfi(")
		? progress.locator
		: undefined;
}

/**
 * Where to open: the saved CFI in this file, or how far into the book a
 * place saved in a file since replaced was ("fraction:0.42"), or one
 * KOReader saved ("koreader:" and its XPointer), which only it can read.
 */
function startIn(
	progress: Progress | null | undefined,
	fileId: string,
): string | undefined {
	if (progress?.locator.startsWith("koreader:")) {
		return `fraction:${progress.fraction}`;
	}
	return progress?.locator.startsWith("fraction:")
		? progress.locator
		: cfiIn(progress, fileId);
}

/** An EPUB, MOBI or AZW3 file of a book, picking up where the person left off. */
export function EbookReader({
	bookId,
	fileId,
	fileName,
	title,
	find,
}: {
	bookId: string;
	fileId: string;
	fileName: string;
	title: string;
	/** A passage to open at instead of the saved place: a word of it to look for, and its text. */
	find?: { word: string; passage: string };
}) {
	const progress = useQuery(progressQuery(bookId));
	const queryClient = useQueryClient();
	const save = useSaveProgress(bookId, "ebook");
	const { mutate: savePlace } = save;
	const pages = useRef<HTMLDivElement>(null);
	const [ebook, setEbook] = useState<Ebook>();
	const [failed, setFailed] = useState<Error | null>(null);
	const [place, setPlace] = useState<Place>();
	const [look, setLook] = useState<Look>(storedLook);
	const [panel, setPanel] = useState<Panel>();
	const [help, setHelp] = useState(false);
	const toggles = useRef<Partial<Record<Panel, HTMLButtonElement | null>>>({});
	const panelBox = usePanelFocus(panel);
	const [further, setFurther] = useState<Progress>();
	// The place last saved or found saved, which needs no saving again.
	const kept = useRef<string | undefined>(undefined);
	const lookNow = useRef(look);
	lookNow.current = look;
	const finding = useRef(Boolean(find));
	const latest = useRef<Place | undefined>(undefined);
	const findNow = useRef(find);

	const show = (which: Panel | undefined) => {
		// Closing gives focus back to the button that opened the panel.
		if (!which && panel) toggles.current[panel]?.focus();
		setPanel(which);
	};
	const toggle = (which: Panel) => show(panel === which ? undefined : which);
	const resize = (step: number) => {
		const at = FONT_SIZES.indexOf(look.fontSize);
		const next = FONT_SIZES[Math.min(Math.max(at + step, 0), FONT_SIZES.length - 1)];
		if (next) setLook({ ...look, fontSize: next });
	};
	const bindings: KeyBinding[] = ebook
		? [
				{ keys: ["ArrowLeft"], label: "keys.pageLeft", run: () => void ebook.goLeft() },
				{ keys: ["ArrowRight"], label: "keys.pageRight", run: () => void ebook.goRight() },
				{ keys: ["PageDown", " "], label: "keys.pageNext", run: () => void ebook.next() },
				{ keys: ["PageUp", "Shift+ "], label: "keys.pagePrevious", run: () => void ebook.prev() },
				{ keys: ["Home"], label: "keys.first", run: () => void ebook.goToFraction(0) },
				{ keys: ["End"], label: "keys.last", run: () => void ebook.goToFraction(1) },
				{ keys: ["c"], label: "keys.contents", run: () => toggle("contents") },
				{ keys: ["f"], label: "keys.search", run: () => toggle("search") },
				{ keys: ["d"], label: "keys.look", run: () => toggle("look") },
				{ keys: ["+", "="], label: "keys.larger", run: () => resize(1) },
				{ keys: ["-"], label: "keys.smaller", run: () => resize(-1) },
				{ keys: ["Escape"], label: "keys.closePanel", run: () => show(undefined) },
				{ keys: ["?"], label: "keys.help", run: () => setHelp(true) },
			]
		: [];
	const keys = useKeys(bindings);

	const ready = !progress.isPending;
	// The saved place is read once, when the book opens: later saves must
	// not open it again.
	const start = useRef<string | undefined>(undefined);
	start.current =
		ready && !findNow.current ? startIn(progress.data?.ebook, fileId) : undefined;
	useEffect(() => {
		if (!ready || !pages.current) return;
		const into = pages.current;
		let open = true;
		let opened: Ebook | undefined;
		kept.current = start.current;
		(async () => {
			const res = await fetch(downloadUrl({ id: fileId }));
			if (!res.ok) throw new Error(t("reader.unreadable"));
			const file = new File([await res.blob()], fileName);
			if (!open) return;
			const book = await openEbook(into, file, {
				start: start.current,
				look: lookNow.current,
				onPlace: (p) => {
					latest.current = p;
					if (open) setPlace(p);
				},
				onKey: keys,
			});
			if (open) {
				opened = book;
				setEbook(book);
			} else {
				book.close();
			}
		})().catch((err: unknown) => {
			if (open) setFailed(err instanceof Error ? err : new Error(String(err)));
		});
		return () => {
			open = false;
			opened?.close();
		};
	}, [ready, fileId, fileName, keys]);

	useEffect(() => {
		ebook?.setLook(look);
		storeLook(look);
	}, [ebook, look]);

	// The passage looked for is where the person is, but not a place they
	// read up to: it is not saved until they move on from it.
	useEffect(() => {
		const wanted = findNow.current;
		if (!ebook || !wanted) return;
		void ebook.find(wanted.word, wanted.passage).finally(() => {
			kept.current = latest.current?.cfi;
			finding.current = false;
		});
	}, [ebook]);

	useEffect(() => {
		if (!place || finding.current || place.cfi === kept.current) return;
		const timer = setTimeout(() => {
			kept.current = place.cfi;
			savePlace(
				{
					fileId,
					locator: place.cfi,
					fraction: place.atEnd ? 1 : Math.min(Math.max(place.fraction, 0), 1),
					chapter: place.chapter,
				},
				{
					onSuccess: (answer) =>
						setFurther(
							answer && !answer.saved && answer.progress
								? answer.progress
								: undefined,
						),
				},
			);
		}, SAVE_AFTER_MS);
		return () => clearTimeout(timer);
	}, [place, fileId, savePlace]);


	const furtherCfi = cfiIn(further, fileId);
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
					disabled={!ebook}
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
					disabled={!ebook}
					onClick={() => toggle("search")}
				>
					{t("reader.search")}
				</button>
				<button
					ref={(el) => {
						toggles.current.look = el;
					}}
					type="button"
					className={button}
					aria-expanded={panel === "look"}
					aria-controls="reader-panel"
					aria-keyshortcuts="D"
					onClick={() => toggle("look")}
				>
					{t("reader.look")}
				</button>
				<button
					type="button"
					className={button}
					aria-keyshortcuts="Shift+?"
					disabled={!ebook}
					onClick={() => setHelp(true)}
				>
					{t("keys.button")}
				</button>
				<button
					type="button"
					className={button}
					aria-keyshortcuts="ArrowLeft"
					disabled={!ebook}
					onClick={() => ebook?.goLeft()}
				>
					{t("reader.left")}
				</button>
				<span aria-live="polite" className="min-w-12 text-center tabular-nums">
					{place
						? `${Math.round((place.atEnd ? 1 : place.fraction) * 100)}%`
						: ""}
				</span>
				<button
					type="button"
					className={button}
					aria-keyshortcuts="ArrowRight"
					disabled={!ebook}
					onClick={() => ebook?.goRight()}
				>
					{t("reader.right")}
				</button>
			</div>
			{help && <KeyHelp bindings={bindings} onClose={() => setHelp(false)} />}
			{further && (
				<div
					role="status"
					className="flex flex-wrap items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm dark:border-amber-700 dark:bg-amber-950"
				>
					<span className="mr-auto">
						{t("reader.furtherElsewhere", {
							percent: Math.round(further.fraction * 100),
						})}
					</span>
					{furtherCfi && (
						<button
							type="button"
							className={button}
							onClick={() => {
								// What this browser now knows is that place.
								knowProgress(queryClient, bookId, "ebook", further);
								kept.current = furtherCfi;
								setFurther(undefined);
								void ebook?.goTo(furtherCfi);
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
							if (place) {
								save.mutate({
									fileId,
									locator: place.cfi,
									fraction: place.atEnd ? 1 : place.fraction,
									chapter: place.chapter,
									force: true,
								});
							}
						}}
					>
						{t("reader.stayHere")}
					</button>
				</div>
			)}
			<FormError error={failed} />
			{!ebook && !failed && (
				<p className="text-slate-500">{t("reader.opening")}</p>
			)}
			{/* The panel lies over the pages rather than beside them: narrowing
			    the pages lays the book out anew, which would undo a jump made
			    from the panel while it is still settling. */}
			<div className="relative">
				{panel && (
					<div
						id="reader-panel"
						ref={panelBox}
						className="absolute inset-y-0 left-0 z-10 w-72 max-w-full overflow-y-auto rounded-md border border-slate-200 bg-white p-3 shadow-lg dark:border-slate-700 dark:bg-slate-900"
					>
						{panel === "contents" && (
							<Contents
								toc={ebook?.toc ?? []}
								current={place?.chapter}
								onGo={(href) => {
									show(undefined);
									void ebook?.goTo(href);
								}}
							/>
						)}
						{panel === "search" && ebook && (
							<BookSearch ebook={ebook} onGo={(cfi) => void ebook.goTo(cfi)} />
						)}
						{panel === "look" && <LookControls look={look} onLook={setLook} />}
					</div>
				)}
				<section
					aria-label={t("reader.pages", { title })}
					data-chapter={place?.chapter ?? ""}
					className="w-full"
				>
					<div ref={pages} className="h-[calc(100dvh-11rem)] min-h-80 w-full" />
				</section>
			</div>
			{place?.chapter && (
				<p className="text-center text-sm text-slate-600 dark:text-slate-400">
					{place.chapter}
				</p>
			)}
		</div>
	);
}

type Panel = "contents" | "look" | "search";

/** How many places a search in the book lists. */
const SEARCH_LIMIT = 100;

/** Looks for words in the book and lists where they stand. */
function BookSearch({ ebook, onGo }: { ebook: Ebook; onGo: (cfi: string) => void }) {
	const [query, setQuery] = useState("");
	const [found, setFound] = useState<Found[]>();
	const [searching, setSearching] = useState(false);
	useEffect(() => () => ebook.clearSearch(), [ebook]);
	const submit = (e: FormEvent) => {
		e.preventDefault();
		const words = query.trim();
		if (!words) return;
		setSearching(true);
		ebook.clearSearch();
		ebook.search(words, SEARCH_LIMIT).then(
			(places) => {
				setFound(places);
				setSearching(false);
			},
			() => {
				setFound([]);
				setSearching(false);
			},
		);
	};
	return (
		<div className="flex flex-col gap-2 text-sm">
			<form role="search" className="flex gap-2" onSubmit={submit}>
				<label className="sr-only" htmlFor="book-search">
					{t("reader.searchLabel")}
				</label>
				<input
					id="book-search"
					type="search"
					value={query}
					onChange={(e) => setQuery(e.target.value)}
					className={`${control} min-w-0 flex-1`}
				/>
				<button type="submit" className={button}>
					{t("reader.searchSubmit")}
				</button>
			</form>
			<p role="status" className="text-slate-600 dark:text-slate-400">
				{searching
					? t("reader.searching")
					: found === undefined
						? ""
						: found.length === 0
							? t("reader.searchNone")
							: t("reader.searchFound", { count: found.length })}
			</p>
			{found && found.length > 0 && (
				<ol className="flex flex-col gap-1">
					{found.map((f) => (
						<li key={f.cfi}>
							<button
								type="button"
								className="text-left hover:underline"
								onClick={() => onGo(f.cfi)}
							>
								{f.excerpt.pre}
								<mark>{f.excerpt.match}</mark>
								{f.excerpt.post}
							</button>
						</li>
					))}
				</ol>
			)}
		</div>
	);
}

function Contents({
	toc,
	current,
	onGo,
}: {
	toc: TocEntry[];
	current?: string;
	onGo: (href: string) => void;
}) {
	if (toc.length === 0) {
		return (
			<p className="text-sm text-slate-600 dark:text-slate-400">
				{t("reader.noOutline")}
			</p>
		);
	}
	return (
		<nav aria-label={t("reader.outline")}>
			<ul className="flex flex-col gap-1 text-sm">
				{toc.map((entry) => (
					<li
						key={entry.key}
						style={{ paddingInlineStart: `${entry.depth}rem` }}
					>
						<button
							type="button"
							aria-current={entry.label === current ? "location" : undefined}
							className="text-left hover:underline aria-[current=location]:font-semibold"
							onClick={() => onGo(entry.href)}
						>
							{entry.label}
						</button>
					</li>
				))}
			</ul>
		</nav>
	);
}

const THEMES: Theme[] = ["light", "sepia", "dark"];

function LookControls({
	look,
	onLook,
}: {
	look: Look;
	onLook: (look: Look) => void;
}) {
	const at = FONT_SIZES.indexOf(look.fontSize);
	return (
		<fieldset className="flex flex-col gap-3 text-sm">
			<legend className="sr-only">{t("reader.look")}</legend>
			<label className="flex flex-col gap-1">
				<span>{t("reader.theme")}</span>
				<select
					value={look.theme}
					onChange={(e) => onLook({ ...look, theme: e.target.value as Theme })}
					className={control}
				>
					{THEMES.map((theme) => (
						<option key={theme} value={theme}>
							{t(`reader.theme.${theme}`)}
						</option>
					))}
				</select>
			</label>
			<label className="flex flex-col gap-1">
				<span>{t("reader.flow")}</span>
				<select
					value={look.flow}
					onChange={(e) => onLook({ ...look, flow: e.target.value as Flow })}
					className={control}
				>
					<option value="paginated">{t("reader.flow.paginated")}</option>
					<option value="scrolled">{t("reader.flow.scrolled")}</option>
				</select>
			</label>
			<div className="flex items-center gap-2">
				<span className="mr-auto">
					{t("reader.fontSize", { percent: look.fontSize })}
				</span>
				<button
					type="button"
					className={button}
					disabled={at === 0}
					onClick={() =>
						onLook({
							...look,
							fontSize: FONT_SIZES[Math.max(at - 1, 0)] ?? 100,
						})
					}
				>
					{t("reader.smaller")}
				</button>
				<button
					type="button"
					className={button}
					disabled={at === FONT_SIZES.length - 1}
					onClick={() =>
						onLook({
							...look,
							fontSize:
								FONT_SIZES[Math.min(at + 1, FONT_SIZES.length - 1)] ?? 100,
						})
					}
				>
					{t("reader.larger")}
				</button>
			</div>
		</fieldset>
	);
}
