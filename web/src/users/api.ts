import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Account = components["schemas"]["Account"];
export type NewAccount = components["schemas"]["CreateAccountRequest"];
export type AccountChanges = components["schemas"]["UpdateAccountRequest"];
export type Session = components["schemas"]["SessionView"];

/** Every account, for administrators. */
export const usersQuery = queryOptions({
	queryKey: ["users"],
	queryFn: async (): Promise<Account[]> =>
		(await api.GET("/users")).data?.users ?? [],
});

/** The signed-in person's own sessions. */
export const ownSessionsQuery = queryOptions({
	queryKey: ["auth", "sessions"],
	queryFn: async (): Promise<Session[]> =>
		(await api.GET("/auth/sessions")).data?.sessions ?? [],
});

function useInvalidate(queryKey: readonly unknown[]) {
	const queryClient = useQueryClient();
	return () => queryClient.invalidateQueries({ queryKey });
}

export function useCreateUser() {
	return useMutation({
		mutationFn: async (body: NewAccount) =>
			(await api.POST("/users", { body })).data,
		onSuccess: useInvalidate(usersQuery.queryKey),
	});
}

export function useUpdateUser() {
	return useMutation({
		mutationFn: async ({ id, changes }: { id: string; changes: AccountChanges }) =>
			(
				await api.PATCH("/users/{userId}", {
					params: { path: { userId: id } },
					body: changes,
				})
			).data,
		onSuccess: useInvalidate(usersQuery.queryKey),
	});
}

export function useEndUserSessions() {
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/users/{userId}/sessions", {
				params: { path: { userId: id } },
			});
		},
		onSuccess: useInvalidate(usersQuery.queryKey),
	});
}

export function useEndOwnSession() {
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/auth/sessions/{sessionId}", {
				params: { path: { sessionId: id } },
			});
		},
		onSuccess: useInvalidate(ownSessionsQuery.queryKey),
	});
}

export function useChangePassword() {
	return useMutation({
		mutationFn: async (body: { currentPassword: string; newPassword: string }) => {
			await api.POST("/auth/password", { body });
		},
		onSuccess: useInvalidate(ownSessionsQuery.queryKey),
	});
}
