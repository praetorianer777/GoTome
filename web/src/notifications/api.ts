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
import { formatReleaseDate } from "@/lib/format";

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

export type NotificationSetting =
	components["schemas"]["NotificationSetting"];

/** The kinds of event the person may be told of, and whether they are. */
export const notificationSettingsQuery = queryOptions({
	queryKey: ["notification-settings"],
	queryFn: async () =>
		(await api.GET("/me/notification-settings")).data?.kinds ?? [],
});

/** Changes whether the person is told of a kind of event. */
export function useSetNotificationSetting() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (setting: NotificationSetting) =>
			(
				await api.PUT("/me/notification-settings", {
					body: { kinds: [setting] },
				})
			).data?.kinds ?? [],
		onSuccess: (kinds) =>
			queryClient.setQueryData(notificationSettingsQuery.queryKey, kinds),
	});
}

/** What each kind of event is called where the person chooses. */
export const EVENT_LABELS: Record<NotificationSetting["kind"], MessageKey> = {
	"books.added": "notifications.kind.booksAdded",
	"wish.fulfilled": "notifications.kind.wishFulfilled",
	"release.announced": "notifications.kind.releaseAnnounced",
	"release.out": "notifications.kind.releaseOut",
	"review.needed": "notifications.kind.reviewNeeded",
	"duplicates.found": "notifications.kind.duplicatesFound",
	"files.unreadable": "notifications.kind.filesUnreadable",
	"scan.failed": "notifications.kind.scanFailed",
};

const text = (v: unknown) => (typeof v === "string" ? v : "");

/** A release's date in a notification's data, as exactly as it is known. */
function releaseDate(d: Record<string, unknown>): string {
	const precision = d.precision;
	if (typeof d.date !== "string" || (precision !== "day" && precision !== "month" && precision !== "year")) {
		return "";
	}
	return formatReleaseDate(d.date, precision);
}
const count = (v: unknown) => (typeof v === "number" ? v : 1);

/** What a notification says, as a heading and a line. */
export function notificationText(n: Notification): {
	title: string;
	detail: string;
} {
	const d = n.data;
	// One event names its book; several are a count.
	const one = typeof d.title === "string" || typeof d.file === "string";
	switch (n.kind) {
		case "books.added":
			return {
				title: t("notification.booksAdded", { library: text(d.library) }),
				detail:
					count(d.count) === 1
						? t("notification.booksAdded.one")
						: t("notification.booksAdded.many", { count: count(d.count) }),
			};
		case "scan.failed":
			return {
				title: t("notification.scanFailed", { library: text(d.library) }),
				detail: d.error
					? text(d.error)
					: t("notification.scanFailed.many", { count: count(d.count) }),
			};
		case "files.unreadable":
			return one
				? {
						title: t("notification.fileUnreadable"),
						detail: t("notification.fileUnreadable.one", { file: text(d.file), title: text(d.title) }),
					}
				: {
						title: t("notification.filesUnreadable"),
						detail: t("notification.filesUnreadable.many", { count: count(d.count) }),
					};
		case "duplicates.found":
			return {
				title: t("notification.duplicatesFound"),
				detail: !one
					? t("notification.duplicatesFound.many", { count: count(d.count) })
					: count(d.count) === 1
						? t("notification.duplicatesFound.one", { title: text(d.title) })
						: t("notification.duplicatesFound.some", { title: text(d.title), count: count(d.count) }),
			};
		case "review.needed":
			return {
				title: t("notification.reviewNeeded"),
				detail: one
					? t("notification.reviewNeeded.one", { title: text(d.title) })
					: t("notification.reviewNeeded.many", { count: count(d.count) }),
			};
		case "wish.fulfilled":
			return {
				title: t("notification.wishFulfilled"),
				detail: one
					? t("notification.wishFulfilled.one", { title: text(d.title) })
					: t("notification.wishFulfilled.many", { count: count(d.count) }),
			};
		case "release.announced":
		case "release.out": {
			const out = n.kind === "release.out";
			if (!one) {
				return {
					title: t(out ? "notification.releaseOut" : "notification.releaseAnnounced"),
					detail: t(out ? "notification.releaseOut.many" : "notification.releaseAnnounced.many", {
						count: count(d.count),
					}),
				};
			}
			const values = { title: text(d.title), authors: text(d.authors), date: releaseDate(d) };
			return {
				title: t(out ? "notification.releaseOut" : "notification.releaseAnnounced"),
				detail: out
					? t("notification.releaseOut.one", values)
					: values.date
						? t("notification.releaseAnnounced.one", values)
						: t("notification.releaseAnnounced.undated", values),
			};
		}
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
