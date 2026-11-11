import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { EREADER_FINDINGS } from "@/books/rules";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { formatSize } from "@/lib/format";

type BookFile = components["schemas"]["BookFile"];
type Finding = components["schemas"]["EreaderCheck"]["findings"][number];
export type Fix = components["schemas"]["FixRequest"]["findings"][number];

/** The ways to fix a finding: the fonts can be cut down to the book's characters or removed. */
export function fixesFor(finding: Finding): Fix[] {
	return finding === "large-fonts" ? ["subset-fonts", "large-fonts"] : [finding];
}

/** A finding as a filter names it ("Very large images"); a name this app does not know stands as it is. */
export function ereaderLabel(finding: string): string {
	return (EREADER_FINDINGS as readonly string[]).includes(finding)
		? t(`filter.ereader.${finding as (typeof EREADER_FINDINGS)[number]}`)
		: finding;
}

/** What each finding says about the file, from its measures. */
function findingText(finding: Finding, check: components["schemas"]["EreaderCheck"]): string {
	const key: MessageKey = `book.ereader.${finding}`;
	switch (finding) {
		case "large-document":
			return t(key, { size: formatSize(check.largestDocument) });
		case "large-fonts":
			return t(key, { count: check.fonts, size: formatSize(check.fontBytes) });
		case "large-image":
			if (check.imageWidth === 0) {
				return t("book.ereader.large-image-bytes", { size: formatSize(check.largestImage) });
			}
			return t(key, {
				width: check.imageWidth,
				height: check.imageHeight,
				size: formatSize(check.largestImage),
			});
		case "missing-resources":
			return t(key, { count: check.missingResources.length });
		case "malformed-document":
			return t(key, { count: check.malformedDocuments.length });
		case "broken-toc":
			return t(key, { count: check.brokenTocEntries.length });
	}
}

/** What a book's findings are called together, for a marker's name. */
export function ereaderSummary(findings: readonly string[]): string {
	return findings.map(ereaderLabel).join(", ");
}

/** Fixes findings of a file; the original goes into the trash. */
export function useFixFile() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async ({ id, findings }: { id: string; findings: Fix[] }) =>
			(
				await api.POST("/files/{fileId}/fix", {
					params: { path: { fileId: id } },
					body: { findings },
				})
			).data,
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["trash"] });
			queryClient.invalidateQueries({ queryKey: ["book"] });
			queryClient.invalidateQueries({ queryKey: ["books"] });
		},
	});
}

const fixButton =
	"ml-2 rounded-md border border-amber-400 px-2 py-0.5 text-xs text-amber-950 hover:bg-amber-100 disabled:opacity-60 dark:border-amber-700 dark:text-amber-100 dark:hover:bg-amber-900/40";

/**
 * Names what may make a file slow or troublesome on an e-reader, each with
 * the button that fixes it where the person may change the file.
 */
export function EReaderFindings({
	file,
	canFix,
	readOnly = false,
}: {
	file: BookFile;
	canFix: boolean;
	/** The file's library is a folder GOtome cannot write to, which is why canFix is off. */
	readOnly?: boolean;
}) {
	const fix = useFixFile();
	const check = file.ereader;
	const findings = check?.findings ?? [];
	if (!check || (findings.length === 0 && !fix.isSuccess)) {
		return null;
	}
	return (
		<span className="mt-1 block text-amber-900 dark:text-amber-200">
			{findings.length > 0 && (
				<>
					<span className="font-medium">{t("book.ereader.heading")}</span>
					<ul className="list-inside list-disc">
						{findings.map((finding) => (
							<li key={finding}>
								{findingText(finding, check)}
								{canFix &&
									!file.missing &&
									fixesFor(finding).map((option) => (
										<button
											key={option}
											type="button"
											className={fixButton}
											disabled={fix.isPending}
											aria-label={t("fix.named", { fix: t(`fix.${option}`), name: file.name })}
											onClick={() => fix.mutate({ id: file.id, findings: [option] })}
										>
											{t(`fix.${option}`)}
										</button>
									))}
							</li>
						))}
					</ul>
					{readOnly && !canFix && (
						<span className="block text-slate-700 dark:text-slate-300">
							{t("book.ereader.readOnly")}
						</span>
					)}
				</>
			)}
			{fix.isSuccess && (
				<span role="status" className="block text-slate-700 dark:text-slate-300">
					{t("fix.done")}
				</span>
			)}
			<FormError error={fix.error} />
		</span>
	);
}
