import { useMutation } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useId, useState } from "react";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { FormError, fieldErrors } from "@/components/form";
import { button } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { formatCount } from "@/lib/format";
import { type Library, useUpdateLibrary } from "@/libraries/api";

type Preview = components["schemas"]["OrganisePreview"];

const PLACEHOLDERS = [
	"{author}",
	"{author_sort}",
	"{title}",
	"{series}",
	"{series_index}",
	"{year}",
	"{language}",
	"{ext}",
];

const SKIP_REASONS: Record<string, MessageKey> = {
	missing: "libraries.organise.skip.missing",
	"too-long": "libraries.organise.skip.too-long",
};

const BLOCKED: Record<string, MessageKey> = {
	"read-only": "libraries.organise.blocked.read-only",
	off: "libraries.organise.blocked.off",
};

const quietButton =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

function usePreview(libraryId: string) {
	return useMutation({
		mutationFn: async (pattern: string): Promise<Preview | undefined> =>
			(
				await api.POST("/libraries/{libraryId}/organise/preview", {
					params: { path: { libraryId } },
					body: { pattern },
				})
			).data,
	});
}

function useStartOrganise(libraryId: string) {
	return useMutation({
		mutationFn: async (pattern: string) =>
			(
				await api.POST("/libraries/{libraryId}/organise", {
					params: { path: { libraryId } },
					body: { pattern },
				})
			).data,
	});
}

/** The pattern a library's files are named by, a preview of what it would move, and the button that moves them. */
export function OrganiseFiles({ library }: { library: Library }) {
	const update = useUpdateLibrary();
	const preview = usePreview(library.id);
	const start = useStartOrganise(library.id);
	const [pattern, setPattern] = useState(library.organisePattern);
	const field = useId();
	const hint = useId();
	const external = library.mode === "external";
	const errors = fieldErrors(update.error ?? preview.error ?? start.error);
	const shown = preview.data;
	const stale = shown !== undefined && shown.pattern !== pattern.trim();
	const mayRun = !external || (library.writable && library.organiseEnabled);

	function ask(event: FormEvent) {
		event.preventDefault();
		start.reset();
		preview.mutate(pattern.trim());
	}

	function save() {
		update.mutate({
			id: library.id,
			changes: { organisePattern: pattern.trim() },
		});
	}

	return (
		<section
			aria-labelledby={`${field}-title`}
			className="flex min-w-0 flex-col gap-3 border-t border-slate-200 pt-3 text-sm dark:border-slate-800"
		>
			<h3 id={`${field}-title`} className="font-medium">
				{t("libraries.organise.title")}
			</h3>
			<p className="text-slate-600 dark:text-slate-400">
				{t("libraries.organise.hint")}
			</p>
			{external && (
				<label className="flex min-h-6 items-center gap-2">
					<input
						type="checkbox"
						checked={library.organiseEnabled}
						disabled={update.isPending || !library.writable}
						onChange={(e) =>
							update.mutate({
								id: library.id,
								changes: { organiseEnabled: e.target.checked },
							})
						}
					/>
					<span>{t("libraries.organise.enable")}</span>
				</label>
			)}
			{external && !library.writable && (
				<p className="text-slate-600 dark:text-slate-400">
					{t("libraries.organise.needsWritable")}
				</p>
			)}
			{mayRun && (
				<form onSubmit={ask} className="flex min-w-0 flex-col gap-2">
					<label htmlFor={field} className="font-medium">
						{t("libraries.organise.pattern")}
					</label>
					<input
						id={field}
						type="text"
						value={pattern}
						onChange={(e) => setPattern(e.target.value)}
						aria-describedby={hint}
						aria-invalid={errors.organisePattern || errors.pattern ? true : undefined}
						className="min-w-0 rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-sm text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100"
					/>
					{(errors.organisePattern || errors.pattern) && (
						<p role="alert" className="text-red-700 dark:text-red-300">
							{errors.organisePattern ?? errors.pattern}
						</p>
					)}
					<div id={hint} className="flex min-w-0 flex-col gap-1 text-slate-600 dark:text-slate-400">
						<p id={`${field}-names`}>{t("libraries.organise.placeholders")}</p>
						<ul
							aria-labelledby={`${field}-names`}
							className="flex min-w-0 flex-wrap gap-x-4 gap-y-2"
						>
							{PLACEHOLDERS.map((p) => (
								<li key={p} className="min-w-0">
									<code className="break-all">{p}</code>
								</li>
							))}
						</ul>
						<p>{t("libraries.organise.brackets")}</p>
					</div>
					<div className="flex flex-wrap gap-2">
						<button type="submit" disabled={preview.isPending} className={quietButton}>
							{preview.isPending
								? t("libraries.organise.previewing")
								: t("libraries.organise.preview")}
						</button>
						<button
							type="button"
							onClick={save}
							disabled={update.isPending || pattern.trim() === library.organisePattern}
							className={quietButton}
						>
							{t("libraries.organise.save")}
						</button>
						<button
							type="button"
							onClick={() =>
								update.mutate(
									{ id: library.id, changes: { organisePattern: "" } },
									{ onSuccess: (saved) => saved && setPattern(saved.organisePattern) },
								)
							}
							disabled={update.isPending}
							className={quietButton}
						>
							{t("libraries.organise.default")}
						</button>
					</div>
				</form>
			)}
			{Object.keys(errors).length === 0 && (
				<FormError error={update.error ?? preview.error ?? start.error} />
			)}
			{shown && (
				<PreviewList
					preview={shown}
					stale={stale}
					starting={start.isPending}
					started={start.data?.runId}
					onApply={() => start.mutate(shown.pattern)}
				/>
			)}
		</section>
	);
}

function PreviewList({
	preview,
	stale,
	starting,
	started,
	onApply,
}: {
	preview: Preview;
	stale: boolean;
	starting: boolean;
	started: string | undefined;
	onApply: () => void;
}) {
	const blocked = preview.blocked ? BLOCKED[preview.blocked] : undefined;
	return (
		<div className="flex min-w-0 flex-col gap-3" aria-live="polite">
			<p>
				{t("libraries.organise.summary", {
					moves: formatCount(preview.moves, 0),
					files: formatCount(preview.files, 0),
					unchanged: formatCount(preview.unchanged, 0),
					collisions: formatCount(preview.collisions, 0),
					skipped: formatCount(preview.skipped, 0),
				})}
			</p>
			{blocked && (
				<p role="status" className="text-amber-800 dark:text-amber-300">
					{t(blocked)}
				</p>
			)}
			{stale && (
				<p role="status" className="text-amber-800 dark:text-amber-300">
					{t("libraries.organise.stale")}
				</p>
			)}
			{preview.moves === 0 ? (
				<p className="text-slate-600 dark:text-slate-400">
					{t("libraries.organise.nothing")}
				</p>
			) : (
				<>
					<ul aria-label={t("libraries.organise.moves")} className="flex min-w-0 flex-col gap-2">
						{preview.examples.map((m) => (
							<li
								key={`${m.bookId}:${m.from}`}
								className="min-w-0 rounded-md border border-slate-200 px-3 py-2 dark:border-slate-800"
							>
								<span className="block break-words font-medium">{m.title}</span>
								<span className="block break-all font-mono text-xs text-slate-600 dark:text-slate-400">
									<span className="sr-only">{t("libraries.organise.from")}: </span>
									{m.from}
								</span>
								<span className="block break-all font-mono text-xs">
									<span className="sr-only">{t("libraries.organise.to")}: </span>
									{"-> "}
									{m.to}
									{m.collision && (
										<em className="ml-2 font-sans text-slate-600 dark:text-slate-400">
											{t("libraries.organise.numbered")}
										</em>
									)}
								</span>
							</li>
						))}
					</ul>
					{preview.examples.length < preview.moves && (
						<p className="text-slate-600 dark:text-slate-400">
							{t("libraries.organise.shown", {
								shown: formatCount(preview.examples.length, 0),
								moves: formatCount(preview.moves, 0),
							})}
						</p>
					)}
				</>
			)}
			{preview.left.length > 0 && (
				<ul aria-label={t("libraries.organise.left")} className="flex min-w-0 flex-col gap-1">
					{preview.left.map((k) => (
						<li key={`${k.bookId}:${k.path}`} className="min-w-0 break-words">
							<span className="font-medium">{k.title}</span>{" "}
							<span className="break-all font-mono text-xs">{k.path}</span>:{" "}
							{SKIP_REASONS[k.reason] ? t(SKIP_REASONS[k.reason] as MessageKey) : k.reason}
						</li>
					))}
				</ul>
			)}
			{started ? (
				<p role="status" className="flex flex-wrap items-center gap-x-3">
					<span>{t("libraries.organise.started")}</span>
					<Link
						to="/curation"
						search={{ run: started }}
						className="text-brand-strong underline underline-offset-4 dark:text-brand"
					>
						{t("libraries.organise.open")}
					</Link>
				</p>
			) : (
				preview.allowed &&
				preview.moves > 0 && (
					<button
						type="button"
						disabled={stale || starting}
						onClick={onApply}
						className={`${button("primary", "md")} self-start`}
					>
						{t("libraries.organise.apply", { count: formatCount(preview.moves, 0) })}
					</button>
				)
			)}
		</div>
	);
}
