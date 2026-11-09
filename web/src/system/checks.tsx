import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { type MessageKey, t } from "@/i18n";
import { formatCount, formatSize } from "@/lib/format";

export type SystemCheck = components["schemas"]["Check"];

/** How each part of GOtome is doing, for the administrators. */
export const systemChecksQuery = queryOptions({
	queryKey: ["system", "checks"],
	queryFn: async () => (await api.GET("/system/checks")).data,
	refetchInterval: 30_000,
});

const PROVIDER = "provider:";

/** What a component is called; a name the app does not know is shown as it is. */
export function checkName(name: string): string {
	if (name.startsWith(PROVIDER)) {
		return t("checks.name.provider", { name: name.slice(PROVIDER.length) });
	}
	const key = `checks.name.${name}` as MessageKey;
	return t(key) === key ? name : t(key);
}

const STATUS_STYLES: Record<SystemCheck["status"], string> = {
	ok: "bg-emerald-600",
	degraded: "bg-amber-500",
	failing: "bg-red-600",
};

function checkValue(c: SystemCheck): string {
	if (c.value === undefined) {
		return "";
	}
	switch (c.unit) {
		case "ms":
			return t("checks.value.ms", { value: formatCount(c.value) });
		case "s":
			return t("checks.value.s", { value: formatCount(c.value, 0) });
		case "bytes":
			return formatSize(c.value);
		default:
			return formatCount(c.value, 0);
	}
}

/** The checks as a list with a colour, and the word, for each status. */
export function ChecksList() {
	const checks = useQuery(systemChecksQuery);
	if (!checks.data) {
		return null;
	}
	return (
		<div className="flex flex-col gap-2">
			<h3 className="font-medium">{t("checks.title")}</h3>
			<ul className="grid gap-x-6 gap-y-1 sm:grid-cols-2">
				{checks.data.checks.map((c) => (
					<li key={c.name} className="flex items-baseline gap-2">
						<span
							aria-hidden="true"
							className={`mt-1.5 size-2.5 shrink-0 self-start rounded-full ${STATUS_STYLES[c.status]}`}
						/>
						<span>
							{checkName(c.name)}
							<span className="text-slate-600 dark:text-slate-400">
								{" "}
								· {t(`checks.status.${c.status}` as MessageKey)}
								{checkValue(c) && ` · ${checkValue(c)}`}
							</span>
							{c.message && (
								<span className="block text-sm text-slate-600 dark:text-slate-400">
									{c.message}
								</span>
							)}
						</span>
					</li>
				))}
			</ul>
		</div>
	);
}
