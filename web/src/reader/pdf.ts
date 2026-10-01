import {
	getDocument,
	GlobalWorkerOptions,
	type PDFDocumentProxy,
	TextLayer,
} from "pdfjs-dist";
import worker from "pdfjs-dist/build/pdf.worker.min.mjs?url";

GlobalWorkerOptions.workerSrc = worker;

/** An entry of a PDF's table of contents, and the page it leads to. */
export interface OutlineItem {
	/** Its place in the outline, unique within it: "0", "0.2", ... */
	id: string;
	title: string;
	/** The page, from 1; undefined for an entry that leads nowhere in the file. */
	page?: number;
	items: OutlineItem[];
}

/** An open PDF, as the reader uses it. */
export interface PdfDocument {
	pages: number;
	/** The width of a page at scale 1, in CSS pixels. */
	pageWidth(page: number): Promise<number>;
	outline(): Promise<OutlineItem[]>;
	/**
	 * Draws a page into the canvas at the scale, with its text laid over it
	 * in the text layer so that it can be selected. The promise ends when
	 * the page is drawn; cancel stops it.
	 */
	render(
		page: number,
		scale: number,
		canvas: HTMLCanvasElement,
		textLayer: HTMLElement,
	): { done: Promise<void>; cancel: () => void };
	close(): void;
}

/** The bytes a range request asks for at most. */
const CHUNK = 256 * 1024;

/**
 * Opens a PDF by its address. Only what the pages shown need is fetched, in
 * range requests, so a large file shows its first page long before the rest
 * has arrived.
 */
export async function openPdf(url: string): Promise<PdfDocument> {
	const doc = await getDocument({
		url,
		rangeChunkSize: CHUNK,
		disableAutoFetch: true,
		disableStream: true,
		enableXfa: false,
	}).promise;
	return wrap(doc);
}

function wrap(doc: PDFDocumentProxy): PdfDocument {
	return {
		pages: doc.numPages,
		async pageWidth(n) {
			const page = await doc.getPage(n);
			return page.getViewport({ scale: 1 }).width;
		},
		async outline() {
			const items = (await doc.getOutline()) ?? [];
			const walk = async (list: typeof items, at: string): Promise<OutlineItem[]> =>
				Promise.all(
					list.map(async (item, i) => ({
						id: `${at}${i}`,
						title: item.title,
						page: await pageOf(doc, item.dest),
						items: await walk(item.items ?? [], `${at}${i}.`),
					})),
				);
			return walk(items, "");
		},
		render(n, scale, canvas, textLayer) {
			let cancelled = false;
			let cancelTask = () => {};
			const done = (async () => {
				const page = await doc.getPage(n);
				if (cancelled) {
					return;
				}
				const viewport = page.getViewport({ scale });
				// Drawn at the screen's pixel density, shown at CSS size.
				const density = window.devicePixelRatio || 1;
				canvas.width = Math.floor(viewport.width * density);
				canvas.height = Math.floor(viewport.height * density);
				canvas.style.width = `${Math.floor(viewport.width)}px`;
				canvas.style.height = `${Math.floor(viewport.height)}px`;
				const task = page.render({
					canvas,
					viewport,
					transform: density === 1 ? undefined : [density, 0, 0, density, 0, 0],
				});
				cancelTask = () => task.cancel();
				await task.promise;
				textLayer.replaceChildren();
				textLayer.style.setProperty("--scale-factor", String(scale));
				textLayer.style.setProperty("--total-scale-factor", String(scale));
				textLayer.style.width = canvas.style.width;
				textLayer.style.height = canvas.style.height;
				const text = new TextLayer({
					textContentSource: page.streamTextContent(),
					container: textLayer,
					viewport,
				});
				cancelTask = () => text.cancel();
				await text.render();
			})().catch((err: unknown) => {
				if (!cancelled) {
					throw err;
				}
			});
			return {
				done,
				cancel: () => {
					cancelled = true;
					cancelTask();
				},
			};
		},
		close() {
			void doc.loadingTask.destroy();
		},
	};
}

async function pageOf(
	doc: PDFDocumentProxy,
	dest: string | unknown[] | null,
): Promise<number | undefined> {
	try {
		const explicit = typeof dest === "string" ? await doc.getDestination(dest) : dest;
		const ref = explicit?.[0];
		if (ref == null) {
			return undefined;
		}
		if (typeof ref === "number") {
			return ref + 1;
		}
		return (await doc.getPageIndex(ref as Parameters<PDFDocumentProxy["getPageIndex"]>[0])) + 1;
	} catch {
		return undefined;
	}
}
