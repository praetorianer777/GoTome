import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import {
	importQuery,
	previewImport,
	type ReadingImport as Import,
	useChooseRow,
	useStartImport,
} from "@/books/history";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { librariesQuery } from "@/libraries/api";

const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";
const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

const OUTCOMES = ["created", "changed", "unchanged", "skipped", "failed"] as const;

/**
 * Brings a reading history in from a Goodreads or StoryGraph export: a
 * preview of what would happen first, then the import, which a job makes.
 */
export function ReadingImport() {
	const libraries = useQuery(librariesQuery);
	const [file, setFile] = useState<File>();
	const [library, setLibrary] = useState("");
	const [overwrite, setOverwrite] = useState(false);
	const preview = useMutation({
		mutationFn: () => previewImport(file as File, library || undefined),
	});
	const id = preview.data?.id;
	const current = useQuery(importQuery(id));
	const shown: Import | undefined = current.data ?? preview.data;
	const start = useStartImport(id ?? "");
	const choose = useChooseRow(id ?? "");

	return (
		<section aria-labelledby="reading-import" className="flex flex-col gap-3">
			<h2 id="reading-import" className="text-lg font-medium">
				{t("import.title")}
			</h2>
			<p className="text-sm text-slate-600 dark:text-slate-400">{t("import.intro")}</p>
			<form
				className="flex flex-wrap items-end gap-3 text-sm"
				onSubmit={(e) => {
					e.preventDefault();
					if (file) {
						preview.mutate();
					}
				}}
			>
				<label className="flex min-w-0 max-w-full flex-col gap-1">
					<span>{t("import.file")}</span>
					<input
						type="file"
						accept=".csv,text/csv"
						onChange={(e) => setFile(e.target.files?.[0])}
						className={`${control} min-w-0 max-w-full pointer-coarse:min-h-11`}
					/>
				</label>
				{(libraries.data?.length ?? 0) > 1 && (
					<label className="flex min-w-0 max-w-full flex-col gap-1">
						<span>{t("import.library")}</span>
						<select value={library} onChange={(e) => setLibrary(e.target.value)} className={`${control} min-w-0`}>
							<option value="">{t("wishlist.libraryAuto")}</option>
							{libraries.data?.map((l) => (
								<option key={l.id} value={l.id}>
									{l.name}
								</option>
							))}
						</select>
					</label>
				)}
				<button type="submit" className={button} disabled={!file || preview.isPending}>
					{t("import.preview")}
				</button>
			</form>
			<FormError error={preview.error} />
			{shown && (
				<div className="flex flex-col gap-3 text-sm">
					<p className="font-medium">{t(`import.source.${shown.source as "goodreads" | "storygraph"}`)}</p>
					<ul className="flex flex-col gap-1">
						<li>{t("import.matched", { count: shown.matched })}</li>
						<li>{t("import.new", { count: shown.new })}</li>
						<li>{t("import.ambiguous", { count: shown.ambiguous })}</li>
						<li>{t("import.skipped", { count: shown.skipped })}</li>
					</ul>
					{shown.roundedRatings > 0 && (
						<p className="text-slate-600 dark:text-slate-400">
							{t("import.rounded", { count: shown.roundedRatings })}
						</p>
					)}
					{shown.skippedRows.length > 0 && (
						<ul className="text-slate-600 dark:text-slate-400">
							{shown.skippedRows.map((r) => (
								<li key={r.row}>{t("import.skippedRow", { row: r.row, title: r.title, message: r.message })}</li>
							))}
						</ul>
					)}
					{shown.state === "preview" &&
						shown.ambiguities.map((a) => (
							<fieldset key={a.row} className="flex flex-col gap-1 rounded-md border border-slate-200 p-2 dark:border-slate-800">
								<legend className="px-1">{t("import.pick", { title: a.title })}</legend>
								{a.candidates.map((c) => (
									<label key={c.id} className="flex items-center gap-2">
										<input
											type="radio"
											name={`row-${a.row}`}
											checked={a.choice === c.id}
											onChange={() => choose.mutate({ row: a.row, book: c.id })}
										/>
										<span>{[c.title, c.authors.join(", "), c.publishedYear].filter(Boolean).join(" · ")}</span>
									</label>
								))}
								<label className="flex items-center gap-2">
									<input
										type="radio"
										name={`row-${a.row}`}
										checked={a.skip}
										onChange={() => choose.mutate({ row: a.row, skip: true })}
									/>
									<span>{t("import.skipRow")}</span>
								</label>
							</fieldset>
						))}
					<FormError error={choose.error} />
					{shown.state === "preview" && (
						<div className="flex flex-wrap items-center gap-4">
							<label className="flex items-center gap-2">
								<input type="checkbox" checked={overwrite} onChange={(e) => setOverwrite(e.target.checked)} />
								<span>{t("import.overwrite")}</span>
							</label>
							<button type="button" className={button} disabled={start.isPending} onClick={() => start.mutate(overwrite)}>
								{t("import.start")}
							</button>
						</div>
					)}
					<FormError error={start.error} />
					{shown.state === "running" && <p role="status">{t("import.running")}</p>}
					{shown.state === "done" && (
						<div role="status" className="flex flex-col gap-1">
							<p className="font-medium">{t("import.done")}</p>
							<ul>
								{OUTCOMES.filter((o) => shown.outcomes[o]).map((o) => (
									<li key={o}>{t(`import.outcome.${o}` as MessageKey, { count: shown.outcomes[o] ?? 0 })}</li>
								))}
							</ul>
						</div>
					)}
				</div>
			)}
		</section>
	);
}
