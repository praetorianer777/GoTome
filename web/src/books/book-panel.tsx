import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	type ReactNode,
	Suspense,
	lazy,
	useEffect,
	useId,
	useRef,
	useState,
	useSyncExternalStore,
} from "react";
import { createPortal } from "react-dom";
import { ApiError } from "@/api/client";
import { bookQuery } from "@/books/api";
import { Cover } from "@/books/cover";
import { Dialog } from "@/components/dialog";
import { FormError } from "@/components/form";
import { Loading } from "@/components/ui";
import { t } from "@/i18n";
import { formatDuration, formatSize } from "@/lib/format";
import { typing } from "@/lib/keys";
import { readable } from "@/routes/read";

const ReaderPreview = lazy(() =>
	import("@/routes/read").then((m) => ({ default: m.ReaderPreview })),
);

const WIDE = "(min-width: 1024px)";

function subscribeWide(notify: () => void) {
	const query = window.matchMedia?.(WIDE);
	query?.addEventListener("change", notify);
	return () => query?.removeEventListener("change", notify);
}

/** Whether the window is wide enough for a panel beside the list. */
function useWide(): boolean {
	return useSyncExternalStore(
		subscribeWide,
		() => window.matchMedia?.(WIDE).matches ?? false,
		() => false,
	);
}

const quietButton =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/**
 * A page with a list and, when a book is open, that book beside it: a sticky
 * panel with its own scroll on a wide window, a full-screen dialog on a
 * phone. The open book belongs in the address, so back closes it.
 */
export function BookPanelLayout({
	bookId,
	onClose,
	panel,
	children,
}: {
	bookId?: string;
	onClose: () => void;
	/** Shown in the panel under the book, such as the decision the queue asks for. */
	panel?: ReactNode;
	children: ReactNode;
}) {
	const wide = useWide();
	return (
		<div
			className={
				bookId && wide
					? "grid grid-cols-[minmax(0,1fr)_minmax(0,26rem)] items-start gap-6"
					: undefined
			}
		>
			<div className="min-w-0">{children}</div>
			{bookId && (
				<BookPanel key={bookId} bookId={bookId} onClose={onClose} wide={wide}>
					{panel}
				</BookPanel>
			)}
		</div>
	);
}

/** A book's description, folded to a few lines until it is unfolded in place. */
export function FoldedText({ text }: { text: string }) {
	const [open, setOpen] = useState(false);
	const [folds, setFolds] = useState(false);
	const box = useRef<HTMLParagraphElement>(null);
	useEffect(() => {
		const el = box.current;
		if (el && !open) setFolds(el.scrollHeight > el.clientHeight + 1);
	}, [open]);
	return (
		<div className="flex flex-col items-start gap-1">
			<p
				ref={box}
				className={`whitespace-pre-line break-words text-sm text-slate-700 dark:text-slate-300 ${open ? "" : "line-clamp-3"}`}
			>
				{text}
			</p>
			{(folds || open) && (
				<button
					type="button"
					aria-expanded={open}
					onClick={() => setOpen(!open)}
					className="text-sm text-brand-strong underline underline-offset-4 dark:text-brand"
				>
					{open ? t("panel.less") : t("panel.more")}
				</button>
			)}
		</div>
	);
}

/**
 * One book beside a review queue: cover, names, description, files, tags,
 * the link to its page and a look inside that saves no place. One prop for
 * the book and one for closing; Escape and the close button close it, and
 * focus goes in and back to where it was.
 */
export function BookPanel({
	bookId,
	onClose,
	wide: wideNow,
	children,
}: {
	bookId: string;
	onClose: () => void;
	wide?: boolean;
	children?: ReactNode;
}) {
	const isWide = useWide();
	const wide = wideNow ?? isWide;
	const [looking, setLooking] = useState(false);
	const book = useQuery(bookQuery(bookId));
	const heading = useId();
	const box = useRef<HTMLElement>(null);
	const close = useRef(onClose);
	close.current = onClose;
	const [opener] = useState(() => document.activeElement as HTMLElement | null);

	useEffect(() => {
		if (!wide) return;
		box.current?.focus();
		return () => {
			if (opener?.isConnected) opener.focus();
		};
	}, [wide, opener]);

	useEffect(() => {
		if (!wide || looking) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== "Escape" || e.defaultPrevented || typing(e)) return;
			if (document.querySelector('[aria-modal="true"]')) return;
			e.preventDefault();
			close.current();
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, [wide, looking]);

	const data = book.data;
	const file = data?.files.find((f) => !f.missing && readable(f.format));
	const body = (
		<div className="flex flex-col gap-4">
			{book.isPending && <Loading />}
			{book.error instanceof ApiError && book.error.status === 404 ? (
				<p className="text-slate-600 dark:text-slate-400">{t("book.notFound")}</p>
			) : (
				<FormError error={book.error} />
			)}
			{data && (
				<>
					<div className="flex gap-4">
						<div className="w-24 shrink-0">
							<Cover book={data} size="small" named />
						</div>
						<div className="flex min-w-0 flex-col gap-1">
							<h3 id={heading} className="break-words font-serif text-lg font-semibold">
								{data.title}
							</h3>
							{data.subtitle && (
								<p className="break-words text-slate-600 dark:text-slate-400">
									{data.subtitle}
								</p>
							)}
							{data.contributors.length > 0 && (
								<p className="break-words">
									{data.contributors.map((c) => c.name).join(", ")}
								</p>
							)}
							{data.series && (
								<p className="break-words text-sm text-slate-600 dark:text-slate-400">
									{data.seriesIndex != null
										? `${data.series} #${data.seriesIndex}`
										: data.series}
								</p>
							)}
						</div>
					</div>
					{children}
					<div className="flex flex-wrap gap-2">
						<Link
							to="/books/$bookId"
							params={{ bookId }}
							className="rounded-md bg-brand-strong px-3 py-2 text-sm font-medium text-white hover:bg-sky-800 dark:bg-brand dark:text-ink dark:hover:bg-sky-300"
						>
							{t("panel.open")}
						</Link>
						{file && (
							<button
								type="button"
								className={`${quietButton} py-2`}
								onClick={() => setLooking(true)}
							>
								{t("panel.look")}
							</button>
						)}
					</div>
					<section aria-label={t("book.description")} className="flex flex-col gap-1">
						<h4 className="text-sm font-medium">{t("book.description")}</h4>
						{data.description ? (
							<p className="whitespace-pre-line break-words text-sm text-slate-700 dark:text-slate-300">
								{data.description}
							</p>
						) : (
							<p className="text-sm text-slate-500">{t("panel.noDescription")}</p>
						)}
					</section>
					{data.tags.length > 0 && (
						<section aria-label={t("book.tags")} className="flex flex-col gap-1">
							<h4 className="text-sm font-medium">{t("book.tags")}</h4>
							<ul className="flex flex-wrap gap-1 text-xs">
								{data.tags.map((tag) => (
									<li
										key={tag}
										className="rounded-full border border-slate-300 px-2 py-0.5 dark:border-slate-600"
									>
										{tag}
									</li>
								))}
							</ul>
						</section>
					)}
					{data.files.length > 0 && (
						<section aria-label={t("book.files")} className="flex flex-col gap-1">
							<h4 className="text-sm font-medium">{t("book.files")}</h4>
							<ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 text-sm dark:divide-slate-800 dark:border-slate-800">
								{data.files.map((f) => (
									<li key={f.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
										<span className="w-12 rounded bg-slate-100 py-0.5 text-center text-xs font-semibold uppercase dark:bg-slate-800">
											{f.format}
										</span>
										<span className="min-w-0 flex-1 break-words">{f.name}</span>
										<span className="text-slate-600 dark:text-slate-400">
											{f.durationMs ? formatDuration(f.durationMs) : formatSize(f.size)}
										</span>
									</li>
								))}
							</ul>
						</section>
					)}
				</>
			)}
		</div>
	);

	const title = data?.title ?? t("panel.title");
	const preview =
		looking &&
		createPortal(
			<Preview title={title} onClose={() => setLooking(false)}>
				<Suspense fallback={<Loading />}>
					<ReaderPreview bookId={bookId} />
				</Suspense>
			</Preview>,
			document.body,
		);

	if (wide) {
		return (
			<>
				<aside
					ref={box}
					tabIndex={-1}
					aria-label={t("panel.label", { title })}
					className="panel sticky top-4 flex max-h-[calc(100dvh-2rem)] flex-col gap-3 overflow-y-auto rounded-2xl p-4 focus:outline-none"
				>
					<div className="flex items-center gap-2">
						<span className="mr-auto text-sm text-slate-500 dark:text-slate-400">
							{t("panel.title")}
						</span>
						<button type="button" onClick={onClose} className={quietButton}>
							{t("dialog.close")}
						</button>
					</div>
					{body}
				</aside>
				{preview}
			</>
		);
	}
	return looking ? (
		preview
	) : (
		<Dialog title={title} onClose={onClose} wide>
			{body}
		</Dialog>
	);
}

/**
 * The reader over the page. It is a dialog to assistive technology but not
 * a modal to the page's keys, which the reader answers itself.
 */
function Preview({
	title,
	onClose,
	children,
}: {
	title: string;
	onClose: () => void;
	children: ReactNode;
}) {
	const id = useId();
	const box = useRef<HTMLDivElement>(null);
	const close = useRef(onClose);
	close.current = onClose;
	const [opener] = useState(() => document.activeElement as HTMLElement | null);
	useEffect(() => {
		box.current?.focus();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape" && !typing(e)) {
				e.preventDefault();
				close.current();
			}
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			if (opener?.isConnected) opener.focus();
		};
	}, [opener]);
	return (
		<div className="fixed inset-0 z-50 flex bg-slate-950/50 p-2 sm:p-4">
			<div
				ref={box}
				role="dialog"
				aria-modal="true"
				aria-labelledby={id}
				tabIndex={-1}
				className="mx-auto flex h-full w-full max-w-5xl flex-col gap-3 overflow-y-auto rounded-2xl border border-slate-200 bg-white p-4 text-slate-900 shadow-xl focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
			>
				<div className="flex items-center gap-2">
					<h2 id={id} className="mr-auto min-w-0 truncate text-lg font-medium">
						{t("panel.lookTitle", { title })}
					</h2>
					<button type="button" onClick={() => close.current()} className={quietButton}>
						{t("dialog.close")}
					</button>
				</div>
				{children}
			</div>
		</div>
	);
}
