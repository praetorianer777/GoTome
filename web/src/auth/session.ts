import {
	type QueryClient,
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api, ApiError } from "@/api/client";
import type { components } from "@/api/schema";

export type CurrentUser = components["schemas"]["CurrentUser"];
export type Permission = CurrentUser["permissions"][number];

/** Who is signed in, or null when nobody is. */
export const currentUserQuery = queryOptions({
	queryKey: ["auth", "me"],
	queryFn: async (): Promise<CurrentUser | null> => {
		try {
			return (await api.GET("/auth/me")).data ?? null;
		} catch (error) {
			// Not being signed in is an answer, not a failure.
			if (error instanceof ApiError && error.status === 401) {
				return null;
			}
			throw error;
		}
	},
	staleTime: 60_000,
});

/** Whether the first account still has to be created. */
export const setupNeededQuery = queryOptions({
	queryKey: ["auth", "setup"],
	queryFn: async (): Promise<boolean> =>
		(await api.GET("/setup")).data?.needed ?? false,
	// Setup is needed once in the life of an installation. After that the
	// answer never changes, so it is not asked again on every navigation.
	staleTime: (query) =>
		query.state.data === false ? Number.POSITIVE_INFINITY : 0,
});

export function can(
	user: CurrentUser | null | undefined,
	permission: Permission,
): boolean {
	return user?.permissions.includes(permission) ?? false;
}

/**
 * Asks the server again who is signed in, after signing in changed the answer.
 * Invalidating is not enough: the route guards read the cache, and nothing on
 * the sign-in page is subscribed to the query that would refetch it.
 */
function refetchCurrentUser(queryClient: QueryClient): Promise<CurrentUser | null> {
	return queryClient.fetchQuery({ ...currentUserQuery, staleTime: 0 });
}

export function useCompleteSetup() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: components["schemas"]["SetupRequest"]) =>
			(await api.POST("/setup", { body })).data,
		onSuccess: async () => {
			queryClient.setQueryData(setupNeededQuery.queryKey, false);
			await refetchCurrentUser(queryClient);
		},
	});
}

export function useLogin() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: components["schemas"]["LoginRequest"]) =>
			(await api.POST("/auth/login", { body })).data,
		onSuccess: () => refetchCurrentUser(queryClient),
	});
}

export function useLogout() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async () => {
			await api.POST("/auth/logout");
		},
		onSuccess: () => {
			// Everything cached was fetched as the user who just left.
			queryClient.clear();
			queryClient.setQueryData(currentUserQuery.queryKey, null);
		},
	});
}

/**
 * Where to go after signing in. Only a path on this site is accepted: the
 * value comes from the address bar, and "//evil.example" or a full URL would
 * send a person who just typed their password somewhere else.
 */
export function safeRedirect(target: string | undefined): string {
	if (
		!target?.startsWith("/") ||
		target.startsWith("//") ||
		target.startsWith("/\\")
	) {
		return "/";
	}
	return target;
}
