import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { can, type CurrentUser } from "@/auth/session";

export type ReleaseStatus = components["schemas"]["ReleaseStatus"];

/** The latest release GitHub named at the daily check. */
export const updateQuery = queryOptions({
	queryKey: ["system", "update"],
	queryFn: async () => (await api.GET("/system/update")).data ?? null,
	staleTime: 10 * 60_000,
});

/** What is known of the latest release; asked only for those who may see it. */
export function useUpdate(user: CurrentUser): ReleaseStatus | null {
	const { data } = useQuery({
		...updateQuery,
		enabled: can(user, "settings:manage"),
	});
	return data ?? null;
}
