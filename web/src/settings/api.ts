import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Setting = components["schemas"]["SettingView"];

/** Every setting; a secret only says whether it is set. */
export const settingsQuery = queryOptions({
	queryKey: ["settings"],
	queryFn: async (): Promise<Setting[]> =>
		(await api.GET("/settings")).data?.settings ?? [],
});

/** Changes settings; null unsets one. */
export function useUpdateSettings() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (values: Record<string, string | null>) =>
			(await api.PATCH("/settings", { body: { values } })).data?.settings ?? [],
		onSuccess: (settings) => queryClient.setQueryData(settingsQuery.queryKey, settings),
	});
}
