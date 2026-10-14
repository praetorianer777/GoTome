import {
	infiniteQueryOptions,
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
export type Storage = components["schemas"]["Storage"];

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

export type LinkedIdentity = components["schemas"]["LinkedIdentity"];

/** The accounts at the identity provider linked to the signed-in person's. */
export const ownIdentitiesQuery = queryOptions({
	queryKey: ["auth", "identities"],
	queryFn: async (): Promise<LinkedIdentity[]> =>
		(await api.GET("/auth/identities")).data?.items ?? [],
});

/** What the signed-in person's uploads take up, and how much they may. */
export const storageQuery = queryOptions({
	queryKey: ["auth", "storage"],
	queryFn: async (): Promise<Storage | undefined> =>
		(await api.GET("/auth/storage")).data,
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

export type Device = components["schemas"]["DeviceView"];

/** The signed-in person's e-readers, each with a token of its own. */
export const ownDevicesQuery = queryOptions({
	queryKey: ["auth", "devices"],
	queryFn: async (): Promise<Device[]> =>
		(await api.GET("/auth/devices")).data?.devices ?? [],
});

export function useCreateDevice() {
	return useMutation({
		mutationFn: async (name: string) =>
			(await api.POST("/auth/devices", { body: { name } })).data,
		onSuccess: useInvalidate(ownDevicesQuery.queryKey),
	});
}

export function useDeleteDevice() {
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/auth/devices/{deviceId}", {
				params: { path: { deviceId: id } },
			});
		},
		onSuccess: useInvalidate(ownDevicesQuery.queryKey),
	});
}

export type DeviceBook = components["schemas"]["DeviceBook"];

/**
 * The books of one of the person's devices, the last sent first, a page at
 * a time. Its key starts with deliveries, so sending a book or taking one
 * back anywhere refreshes it.
 */
export function deviceBooksQuery(deviceId: string) {
	return infiniteQueryOptions({
		queryKey: ["deliveries", "device", deviceId],
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/devices/{deviceId}/books", {
					params: {
						path: { deviceId },
						query: { limit: 50, cursor: pageParam || undefined },
					},
				})
			).data,
		initialPageParam: "",
		getNextPageParam: (last) => last?.nextCursor || undefined,
	});
}

/** Takes a book off a device: out of its queue, or off a Kobo reader. */
export function useTakeOffDevice() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (deliveryId: string) => {
			await api.DELETE("/deliveries/{deliveryId}", {
				params: { path: { deliveryId } },
			});
		},
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["deliveries"] }),
	});
}

export type DeviceRequest = components["schemas"]["DeviceRequestView"];

/**
 * E-readers asking to be added, which wait for the person to accept them.
 * Looked at again every few seconds, as one is asked for while the person
 * watches the device beside them.
 */
export const deviceRequestsQuery = queryOptions({
	queryKey: ["auth", "device-requests"],
	queryFn: async (): Promise<DeviceRequest[]> =>
		(await api.GET("/auth/device-requests")).data?.requests ?? [],
	refetchInterval: 5000,
});

export function useDecideDeviceRequest() {
	const requests = useInvalidate(deviceRequestsQuery.queryKey);
	const devices = useInvalidate(ownDevicesQuery.queryKey);
	return useMutation({
		mutationFn: async ({ id, accept }: { id: string; accept: boolean }) => {
			const path = { params: { path: { requestId: id } } };
			await (accept
				? api.POST("/auth/device-requests/{requestId}/accept", path)
				: api.POST("/auth/device-requests/{requestId}/decline", path));
		},
		onSuccess: () => {
			requests();
			devices();
		},
	});
}

export function useUnlinkIdentity() {
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/auth/identities/{identityId}", {
				params: { path: { identityId: id } },
			});
		},
		onSuccess: useInvalidate(ownIdentitiesQuery.queryKey),
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

export function useSetQuota() {
	return useMutation({
		mutationFn: async ({ id, quotaBytes }: { id: string; quotaBytes: number | null }) =>
			(
				await api.PUT("/users/{userId}/quota", {
					params: { path: { userId: id } },
					body: { quotaBytes },
				})
			).data,
		onSuccess: useInvalidate(usersQuery.queryKey),
	});
}
