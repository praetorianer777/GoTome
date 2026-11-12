import { Link } from "@tanstack/react-router";
import { useState } from "react";
import {
	type Annotation,
	COLORS,
	cssColor,
	exportUrl,
	useChangeAnnotation,
	useDeleteAnnotation,
} from "@/books/annotations";
import { FormError } from "@/components/form";
import { button, control } from "@/components/ui";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";

/** What the person is asked when they have selected text in a reader. */
export function SelectionBar({
	text,
	pending,
	onSave,
	onCancel,
	onEngage,
}: {
	text: string;
	pending: boolean;
	onSave: (highlight: { color: string; note: string }) => void;
	onCancel: () => void;
	/** The bar was touched: the selection going away is no reason to hide it. */
	onEngage: () => void;
}) {
	const [color, setColor] = useState<string>(COLORS[0]);
	const [note, setNote] = useState("");
	return (
		<form
			aria-label={t("highlights.selection")}
			onPointerDown={onEngage}
			onFocus={onEngage}
			onSubmit={(e) => {
				e.preventDefault();
				onSave({ color, note });
			}}
			className="fixed inset-x-3 bottom-3 z-30 mx-auto flex max-w-xl flex-col gap-2 rounded-xl border border-slate-300 bg-white p-3 text-sm shadow-lg dark:border-slate-600 dark:bg-slate-900"
		>
			<p className="line-clamp-2 break-words text-slate-600 italic dark:text-slate-400">
				{text}
			</p>
			<div className="flex flex-wrap items-center gap-2">
				<fieldset className="flex min-w-0 items-center gap-1">
					<legend className="sr-only">{t("highlights.color")}</legend>
					{COLORS.map((name) => (
						<label key={name} className="cursor-pointer">
							<input
								type="radio"
								name="highlight-color"
								value={name}
								checked={color === name}
								onChange={() => setColor(name)}
								className="peer sr-only"
							/>
							<span
								aria-hidden="true"
								style={{ backgroundColor: cssColor(name) }}
								className="block size-7 rounded-full border border-slate-400 peer-checked:ring-2 peer-checked:ring-brand-strong peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 dark:peer-checked:ring-brand"
							/>
							<span className="sr-only">{t(`highlights.color.${name}`)}</span>
						</label>
					))}
				</fieldset>
				<label className="min-w-0 flex-1">
					<span className="sr-only">{t("highlights.noteLabel")}</span>
					<input
						type="text"
						value={note}
						maxLength={2000}
						placeholder={t("highlights.notePlaceholder")}
						onChange={(e) => setNote(e.target.value)}
						className={`${control} w-full min-w-0`}
					/>
				</label>
				<button type="submit" disabled={pending} className={button("primary")}>
					{t("highlights.save")}
				</button>
				<button type="button" onClick={onCancel} className={button()}>
					{t("highlights.cancel")}
				</button>
			</div>
		</form>
	);
}

/** Where a highlight stands in its book, as a line of words. */
function whereLine(a: Annotation): string {
	return [
		a.chapter,
		a.page ? t("fulltext.page", { page: a.page }) : undefined,
		formatDate(a.createdAt),
	]
		.filter(Boolean)
		.join(" · ");
}

/** The words that find a highlighted passage again in its book. */
export function wordOf(passage: string): string {
	return passage
		.split(/\s+/)
		.map((w) => w.replace(/^[^\p{L}\p{N}]+|[^\p{L}\p{N}]+$/gu, ""))
		.reduce((a, b) => (b.length > a.length ? b : a), "");
}

/** The reader's address that opens a highlight, for a highlight of a file. */
export function readerSearchFor(a: Annotation) {
	if (a.kind === "pdf" && a.page) return { page: a.page };
	const find = wordOf(a.passage);
	return find ? { find, near: a.passage.slice(0, 300) } : {};
}

/**
 * One highlight: its passage, its note, and changing or deleting it. With
 * onGo it offers going to it in the reader open now; otherwise it names its
 * book and links to the book in the reader.
 */
export function HighlightItem({
	annotation: a,
	withBook,
	onGo,
	highlighted,
}: {
	annotation: Annotation;
	withBook?: boolean;
	onGo?: () => void;
	highlighted?: boolean;
}) {
	const change = useChangeAnnotation();
	const remove = useDeleteAnnotation();
	const [editing, setEditing] = useState(false);
	const [note, setNote] = useState(a.note ?? "");
	const [confirming, setConfirming] = useState(false);
	return (
		<li
			aria-label={a.passage.slice(0, 80)}
			data-highlighted={highlighted ? "true" : undefined}
			className="flex min-w-0 flex-col gap-2 py-3 data-[highlighted=true]:bg-slate-100 dark:data-[highlighted=true]:bg-slate-800"
		>
			{withBook && (
				<p className="min-w-0 text-sm">
					<Link
						to="/books/$bookId"
						params={{ bookId: a.bookId }}
						className="break-words font-medium text-brand-strong underline underline-offset-4 dark:text-brand"
					>
						{a.bookTitle}
					</Link>
					{a.bookAuthors && a.bookAuthors.length > 0 && (
						<span className="text-slate-600 dark:text-slate-400">
							{" · "}
							{a.bookAuthors.join(", ")}
						</span>
					)}
				</p>
			)}
			<blockquote
				style={{ borderColor: cssColor(a.color) }}
				className="min-w-0 break-words border-l-4 pl-3 leading-relaxed"
			>
				{a.passage}
			</blockquote>
			{!editing && a.note && (
				<p className="min-w-0 break-words whitespace-pre-wrap pl-4 text-sm">
					<span className="font-medium">{t("highlights.note")}: </span>
					{a.note}
				</p>
			)}
			<p className="pl-4 text-xs text-slate-600 dark:text-slate-400">
				{whereLine(a)}
			</p>
			{editing ? (
				<form
					className="flex flex-col gap-2 pl-4"
					onSubmit={(e) => {
						e.preventDefault();
						change.mutate(
							{ id: a.id, note },
							{ onSuccess: () => setEditing(false) },
						);
					}}
				>
					<label className="flex flex-col gap-1 text-sm">
						<span>{t("highlights.noteLabel")}</span>
						<textarea
							value={note}
							maxLength={20000}
							rows={3}
							onChange={(e) => setNote(e.target.value)}
							className={`${control} w-full min-w-0`}
						/>
					</label>
					<div className="flex flex-wrap gap-2">
						<button
							type="submit"
							disabled={change.isPending}
							className={button("primary")}
						>
							{t("highlights.saveNote")}
						</button>
						<button
							type="button"
							onClick={() => {
								setEditing(false);
								setNote(a.note ?? "");
							}}
							className={button()}
						>
							{t("highlights.cancel")}
						</button>
					</div>
				</form>
			) : (
				<div className="flex flex-wrap items-center gap-2 pl-4 text-sm">
					{onGo ? (
						<button type="button" onClick={onGo} className={button()}>
							{t("highlights.show")}
						</button>
					) : (
						a.fileId && (
							<Link
								to="/books/$bookId/read/$fileId"
								params={{ bookId: a.bookId, fileId: a.fileId }}
								search={readerSearchFor(a)}
								className={button()}
							>
								{t("highlights.open")}
							</Link>
						)
					)}
					<button
						type="button"
						onClick={() => setEditing(true)}
						className={button()}
					>
						{a.note ? t("highlights.editNote") : t("highlights.addNote")}
					</button>
					<label className="inline-flex items-center gap-1">
						<span className="sr-only">{t("highlights.color")}</span>
						<select
							value={a.color}
							disabled={change.isPending}
							onChange={(e) => change.mutate({ id: a.id, color: e.target.value })}
							className={control}
						>
							{COLORS.map((name) => (
								<option key={name} value={name}>
									{t(`highlights.color.${name}`)}
								</option>
							))}
						</select>
					</label>
					{confirming ? (
						<>
							<button
								type="button"
								disabled={remove.isPending}
								onClick={() => remove.mutate(a.id)}
								className={button("danger")}
							>
								{t("highlights.deleteConfirm")}
							</button>
							<button
								type="button"
								onClick={() => setConfirming(false)}
								className={button()}
							>
								{t("highlights.cancel")}
							</button>
						</>
					) : (
						<button
							type="button"
							onClick={() => setConfirming(true)}
							className={button()}
						>
							{t("highlights.delete")}
						</button>
					)}
				</div>
			)}
			<FormError error={change.error ?? remove.error} />
		</li>
	);
}

/** Download links for a person's highlights: all of them, or one book's. */
export function ExportLinks({ book, q }: { book?: string; q?: string }) {
	return (
		<div className="flex flex-wrap gap-2 text-sm">
			<a
				href={exportUrl("md", { book, q })}
				download
				className={button()}
			>
				{t("highlights.exportMd")}
			</a>
			<a
				href={exportUrl("csv", { book, q })}
				download
				className={button()}
			>
				{t("highlights.exportCsv")}
			</a>
		</div>
	);
}

/** A book's highlights as the reader's side panel lists them. */
export function HighlightsPanel({
	annotations,
	onGo,
	note,
}: {
	annotations: Annotation[];
	onGo: (annotation: Annotation) => void;
	/** Something to say about this reader's highlights. */
	note?: string;
}) {
	return (
		<div className="flex min-w-0 flex-col gap-2 text-sm">
			{note && <p className="text-slate-600 dark:text-slate-400">{note}</p>}
			{annotations.length === 0 ? (
				<p role="status" className="text-slate-600 dark:text-slate-400">
					{t("highlights.panelEmpty")}
				</p>
			) : (
				<ul
					aria-label={t("highlights.list")}
					className="flex min-w-0 flex-col divide-y divide-slate-200 dark:divide-slate-800"
				>
					{annotations.map((a) => (
						<HighlightItem key={a.id} annotation={a} onGo={() => onGo(a)} />
					))}
				</ul>
			)}
		</div>
	);
}
