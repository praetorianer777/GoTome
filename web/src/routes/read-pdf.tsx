import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { bookQuery, downloadUrl } from "@/books/api";
import {
	knowProgress,
	type Progress,
	progressQuery,
	useSaveProgress,
} from "@/books/progress";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { type OutlineItem, openPdf, type PdfDocument } from "@/reader/pdf";
import { typing } from "@/routes/book-find";
import "@/reader/text-layer.css";

type Zoom = "width" | number;

const ZOOMS = [0.5, 0.75, 1, 1.25, 1.5, 2, 3];
/** How long the page must stay before it is saved as the place. */
const SAVE_AFTER_MS = 600;

const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-600 dark:hover:bg-slate-800";

/** The page a saved position names, if it is one in this file. */
function pageIn(
	progress: Progress | null | undefined,
	fileId: string,
): number | undefined {
	const match = /^page:(\d+)$/.exec(progress?.locator ?? "");
	if (!match || progress?.fileId !== fileId) {
		return undefined;
	}
	return Number(match[1]);
}

/** The reader at its address, which names the book and the file. */
export function PdfReaderRoute() {
	const { bookId, fileId } = useParams({ from: "/app/books/$bookId/read/$fileId" });
	return <PdfReader bookId={bookId} fileId={fileId} />;
}

/** A PDF of a book, a page at a time, picking up where the person left off. */
export function PdfReader({
	bookId,
	fileId,
}: {
	bookId: string;
	fileId: string;
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
	const [outlineOpen, setOutlineOpen] = useState(false);
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

	// Once the file and the saved place are both known, the reader starts
	// there.
	if (doc && !progress.isPending && page === undefined) {
		const resumed = pageIn(progress.data?.ebook, fileId);
		const start = Math.min(Math.max(resumed ?? 1, 1), doc.pages);
		kept.current = resumed;
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

	const keys = useRef({ go, page });
	keys.current = { go, page };
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (typing(e) || e.ctrlKey || e.metaKey || e.altKey) return;
			const at = keys.current.page ?? 1;
			const step: Record<string, number> = {
				ArrowRight: 1,
				PageDown: 1,
				ArrowLeft: -1,
				PageUp: -1,
			};
			if (e.key in step) {
				e.preventDefault();
				keys.current.go(at + (step[e.key] ?? 0));
			} else if (e.key === "Home") {
				e.preventDefault();
				keys.current.go(1);
			} else if (e.key === "End") {
				e.preventDefault();
				keys.current.go(Number.MAX_SAFE_INTEGER);
			}
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, []);

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
					type="button"
					className={button}
					aria-expanded={outlineOpen}
					aria-controls="reader-outline"
					disabled={!doc}
					onClick={() => setOutlineOpen(!outlineOpen)}
				>
					{t("reader.outline")}
				</button>
				<ZoomControls zoom={zoom} onZoom={setZoom} disabled={!doc} />
				<PageControls page={page} pages={doc?.pages} onGo={go} />
			</div>
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
				{doc && outlineOpen && (
					<Outline doc={doc} current={page} onGo={(n) => go(n)} />
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
}: {
	page?: number;
	pages?: number;
	onGo: (page: number) => void;
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
					aria-label={t("reader.pageNumber")}
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
				onClick={() =>
					onZoom(
						[...ZOOMS].reverse().find((z) => z < (at ?? 1)) ?? ZOOMS[0] ?? 1,
					)
				}
			>
				{t("reader.zoomOut")}
			</button>
			<button
				type="button"
				className={button}
				aria-pressed={zoom === "width"}
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
				onClick={() =>
					onZoom(ZOOMS.find((z) => z > (at ?? 1)) ?? ZOOMS.at(-1) ?? 1)
				}
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
		<nav
			id="reader-outline"
			aria-label={t("reader.outline")}
			className="w-64 shrink-0 overflow-auto text-sm"
		>
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
