import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Webhook = components["schemas"]["Webhook"];
export type WebhookList = components["schemas"]["WebhookList"];
export type WebhookRequest = components["schemas"]["WebhookRequest"];
export type WebhookDelivery = components["schemas"]["WebhookDelivery"];

/** The webhooks and the kinds of event they may ask for. */
export const webhooksQuery = queryOptions({
	queryKey: ["webhooks"],
	queryFn: async (): Promise<WebhookList> =>
		(await api.GET("/webhooks")).data ?? { webhooks: [], kinds: [] },
});

/** The latest deliveries to one webhook. */
export const deliveriesQuery = (id: string) =>
	queryOptions({
		queryKey: ["webhooks", id, "deliveries"],
		queryFn: async (): Promise<WebhookDelivery[]> =>
			(
				await api.GET("/webhooks/{webhookId}/deliveries", {
					params: { path: { webhookId: id } },
				})
			).data?.deliveries ?? [],
	});

export function useCreateWebhook() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: WebhookRequest) =>
			(await api.POST("/webhooks", { body })).data,
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["webhooks"] }),
	});
}

export function useUpdateWebhook() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async ({ id, body }: { id: string; body: WebhookRequest }) =>
			(
				await api.PUT("/webhooks/{webhookId}", {
					params: { path: { webhookId: id } },
					body,
				})
			).data,
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["webhooks"] }),
	});
}

export function useDeleteWebhook() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/webhooks/{webhookId}", {
				params: { path: { webhookId: id } },
			});
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["webhooks"] }),
	});
}

export function useTestWebhook() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (id: string) => {
			await api.POST("/webhooks/{webhookId}/test", {
				params: { path: { webhookId: id } },
			});
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["webhooks"] }),
	});
}
