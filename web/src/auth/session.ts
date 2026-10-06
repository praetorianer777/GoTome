import {
	type QueryClient,
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api, ApiError } from "@/api/client";
import type { components } from "@/api/schema";
import { leave } from "@/auth/sso";

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

export type SignInMethods = components["schemas"]["SignInMethods"];

/** How one may sign in here: with a password, single sign-on, or both. */
export const signInMethodsQuery = queryOptions({
	queryKey: ["auth", "methods"],
	queryFn: async (): Promise<SignInMethods> =>
		(await api.GET("/auth/methods")).data ?? { password: true },
});

/**
 * Sends the browser to the identity provider, to sign in or, with `link`,
 * to link an account there to the one signed in. It comes back through the
 * server, which takes it to `returnTo`, or to the profile after a link.
 */
export function useSingleSignOn() {
	return useMutation({
		mutationFn: async ({ returnTo, link }: { returnTo?: string; link?: boolean }) =>
			link
				? (await api.POST("/auth/identities")).data
				: (await api.POST("/auth/oidc/start", { body: { returnTo } })).data,
		onSuccess: (data) => {
			if (data) {
				leave.to(data.url);
			}
		},
	});
}
