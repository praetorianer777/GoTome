import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { calibreImportQuery, usePreviewCalibre, useStartCalibre } from "@/calibre/api";
import { FormError } from "@/components/form";
import { button, control, PageHeader } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { formatCount } from "@/lib/format";
import { librariesQuery } from "@/libraries/api";

const OUTCOMES = ["changed", "unchanged", "skipped", "failed"] as const;

/**
 * Brings the metadata of a Calibre library in: a preview of what would
 * happen first, then the import, which a job makes.
 */
export function AdminCalibre() {
	const libraries = useQuery(librariesQuery);
	const folders = (libraries.data ?? []).filter((l) => l.mode === "external");
	const [chosen, setChosen] = useState("");
	const library = chosen || folders[0]?.id || "";
	const preview = usePreviewCalibre();
	const id = preview.data?.id;
	const current = useQuery(calibreImportQuery(id));
	const shown = current.data ?? preview.data;
	const start = useStartCalibre(id ?? "");
	const [ratings, setRatings] = useState(false);
	const [picks, setPicks] = useState<Record<string, string>>({});

	return (
		<div className="flex flex-col gap-6">
			<PageHeader title={t("calibre.title")} intro={t("calibre.intro")} />
			<section aria-labelledby="calibre-library" className="flex min-w-0 max-w-2xl flex-col gap-3">
				<h2 id="calibre-library" className="sr-only">
					{t("calibre.library")}
				</h2>
				{folders.length === 0 && !libraries.isPending && (
					<p className="text-slate-600 dark:text-slate-400">{t("calibre.none")}</p>
				)}
				{folders.length > 0 && (
					<form
						className="flex flex-wrap items-end gap-3 text-sm"
						onSubmit={(e) => {
							e.preventDefault();
							preview.mutate(library);
						}}
					>
						<label className="flex min-w-0 max-w-full flex-col gap-1">
							<span>{t("calibre.library")}</span>
							<select
								value={library}
								onChange={(e) => setChosen(e.target.value)}
								className={`${control} min-w-0`}
							>
								{folders.map((l) => (
									<option key={l.id} value={l.id}>
										{l.name}
									</option>
								))}
							</select>
						</label>
						<button type="submit" className={button()} disabled={preview.isPending}>
							{t("calibre.preview")}
						</button>
					</form>
				)}
				<FormError error={preview.error} />
			</section>
			{shown && (
				<section aria-label={t("calibre.title")} className="flex min-w-0 max-w-2xl flex-col gap-4 text-sm">
					<ul className="flex flex-col gap-1">
						<li>{t("calibre.books", { count: formatCount(shown.books) })}</li>
						<li>{t("calibre.ready", { count: formatCount(shown.ready) })}</li>
						{shown.skipped > 0 && <li>{t("calibre.skipped", { count: formatCount(shown.skipped) })}</li>}
						{shown.missingFiles > 0 && (
							<li>{t("calibre.missingFiles", { count: formatCount(shown.missingFiles) })}</li>
						)}
						<li>
							{t("calibre.formats", {
								formats:
									Object.entries(shown.formats)
										.map(([format, n]) => `${format} ${formatCount(n)}`)
										.join(", ") || "-",
							})}
						</li>
						<li>{t("calibre.covers", { count: formatCount(shown.covers) })}</li>
						<li>{t("calibre.series", { count: formatCount(shown.series) })}</li>
						<li>{t("calibre.tags", { count: formatCount(shown.tags) })}</li>
						<li>{t("calibre.rated", { count: formatCount(shown.rated) })}</li>
					</ul>
					{shown.problems.length > 0 && (
						<ul className="min-w-0 text-slate-600 dark:text-slate-400">
							{shown.problems.map((p) => (
								<li key={p.calibreId} className="break-words">
									{t("calibre.problem", { title: p.title, message: p.message })}
								</li>
							))}
						</ul>
					)}
					{shown.state === "preview" && (
						<>
							{shown.columns.length > 0 && (
								<fieldset className="flex min-w-0 flex-col gap-2">
									<legend className="font-medium">{t("calibre.columns")}</legend>
									<p className="text-slate-600 dark:text-slate-400">{t("calibre.columnsIntro")}</p>
									{shown.columns.map((c) => (
										<label key={c.label} className="flex min-w-0 flex-wrap items-center gap-2">
											<span className="min-w-0 break-words">
												{t("calibre.column", {
													name: c.name,
													datatype: c.datatype,
													count: formatCount(c.books),
												})}
											</span>
											<select
												value={picks[c.label] ?? "ignore"}
												onChange={(e) => setPicks({ ...picks, [c.label]: e.target.value })}
												className={`${control} min-w-0`}
											>
												{c.choices.map((choice) => (
													<option key={choice} value={choice}>
														{t(`calibre.choice.${choice}` as MessageKey)}
													</option>
												))}
											</select>
										</label>
									))}
								</fieldset>
							)}
							<label className="flex items-center gap-2">
								<input type="checkbox" checked={ratings} onChange={(e) => setRatings(e.target.checked)} />
								<span>{t("calibre.ratings")}</span>
							</label>
							<div>
								<button
									type="button"
									className={button("primary")}
									disabled={start.isPending || shown.ready === 0}
									onClick={() => start.mutate({ ratings, columns: picks })}
								>
									{t("calibre.start")}
								</button>
							</div>
						</>
					)}
					<FormError error={start.error} />
					{shown.state === "running" && <p role="status">{t("calibre.running")}</p>}
					{shown.state === "done" && (
						<div role="status" className="flex flex-col gap-1">
							<p className="font-medium">{t("calibre.done")}</p>
							<ul>
								{OUTCOMES.filter((o) => shown.outcomes[o]).map((o) => (
									<li key={o}>
										{t(`calibre.outcome.${o}` as MessageKey, { count: formatCount(shown.outcomes[o] ?? 0) })}
									</li>
								))}
							</ul>
						</div>
					)}
				</section>
			)}
		</div>
	);
}
