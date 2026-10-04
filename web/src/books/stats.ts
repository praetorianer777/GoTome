import { queryOptions } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type ReadingStats = components["schemas"]["ReadingStats"];

/** This browser's time zone, which the server counts days in. */
function zone(): string | undefined {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
	} catch {
		return undefined;
	}
}

/** What the signed-in person read and listened to over the last days. */
export function statsQuery(days: number) {
	return queryOptions({
		queryKey: ["stats", days],
		queryFn: async () =>
			(await api.GET("/me/stats", { params: { query: { days, tz: zone() } } })).data,
	});
}
