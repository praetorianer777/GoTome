import { useQuery } from "@tanstack/react-query";
import { t } from "@/i18n";
import { formatSize } from "@/lib/format";
import { storageQuery } from "@/users/api";

/** How much of their storage the signed-in person's uploads take up. */
export function StorageUse() {
	const storage = useQuery(storageQuery);
	if (!storage.data) {
		return null;
	}
	const { usedBytes, quotaBytes } = storage.data;
	return (
		<p className="text-sm text-slate-600 dark:text-slate-400">
			{quotaBytes === undefined
				? t("storage.unlimited", { used: formatSize(usedBytes) })
				: t("storage.limited", { used: formatSize(usedBytes), quota: formatSize(quotaBytes) })}
		</p>
	);
}
