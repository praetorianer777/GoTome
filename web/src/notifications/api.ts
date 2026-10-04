import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { useEffect } from "react";
import { API_BASE, api } from "@/api/client";
import type { components } from "@/api/schema";
import {
	BULK_OUTCOME_LABELS,
	BULK_OUTCOMES,
	type BulkOutcome,
} from "@/books/bulk";
import { type MessageKey, t } from "@/i18n";

export type Notification = components["schemas"]["Notification"];

/** The newest of the person's notifications, and how many are unread. */
export const notificationsQuery = queryOptions({
	queryKey: ["notifications"],
	queryFn: async () =>
		(await api.GET("/notifications", { params: { query: { limit: 20 } } }))
			.data ?? {
			notifications: [],
			unread: 0,
		},
});

/** Marks the notifications named, or all of them, as read. */
export function useMarkRead() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (which: { ids: string[] } | { all: true }) =>
			(await api.POST("/notifications/read", { body: which })).data,
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["notifications"] }),
	});
}

/**
 * Listens to the server's stream while the app is open, and reads the
 * notifications again whenever their unread count changes. EventSource
 * opens the stream again by itself when it drops.
 */
export function useNotificationStream() {
	const queryClient = useQueryClient();
	useEffect(() => {
		if (typeof EventSource === "undefined") return;
		const stream = new EventSource(`${API_BASE}/notifications/stream`);
		stream.addEventListener("unread", (e) => {
			const { unread } = JSON.parse((e as MessageEvent<string>).data) as {
				unread: number;
			};
			const known = queryClient.getQueryData(notificationsQuery.queryKey);
			if (known?.unread !== unread) {
				queryClient.invalidateQueries({ queryKey: ["notifications"] });
			}
		});
		return () => stream.close();
	}, [queryClient]);
}

const BULK_ACTIONS: Record<string, MessageKey> = {
	edit: "notification.bulk.edit",
	fetch: "notification.bulk.fetch",
	writeBack: "notification.bulk.writeBack",
};

/** What a notification says, as a heading and a line. */
export function notificationText(n: Notification): {
	title: string;
	detail: string;
} {
	switch (n.kind) {
		case "bulk.finished": {
			const action = typeof n.data.action === "string" ? n.data.action : "";
			const outcomes = (n.data.outcomes ?? {}) as Partial<
				Record<BulkOutcome, number>
			>;
			return {
				title: t(BULK_ACTIONS[action] ?? "notification.bulk.edit"),
				detail: BULK_OUTCOMES.filter((o) => outcomes[o])
					.map((o) => `${t(BULK_OUTCOME_LABELS[o])}: ${outcomes[o]}`)
					.join(" · "),
			};
		}
	}
	return { title: t("notification.unknown"), detail: "" };
}
