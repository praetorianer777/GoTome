import type { components } from "@/api/schema";
import { type MessageKey, t } from "@/i18n";
import { formatSize } from "@/lib/format";

type BookFile = components["schemas"]["BookFile"];
type Finding = components["schemas"]["EreaderCheck"]["findings"][number];

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
	}
}

/** Names what may make a file slow or troublesome on an e-reader. */
export function EReaderFindings({ file }: { file: BookFile }) {
	const check = file.ereader;
	if (!check || check.findings.length === 0) {
		return null;
	}
	return (
		<span className="mt-1 block text-amber-900 dark:text-amber-200">
			<span className="font-medium">{t("book.ereader.heading")}</span>
			<ul className="list-inside list-disc">
				{check.findings.map((finding) => (
					<li key={finding}>{findingText(finding, check)}</li>
				))}
			</ul>
		</span>
	);
}
