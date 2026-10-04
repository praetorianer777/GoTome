import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import {
	type Notification,
	notificationText,
	notificationsQuery,
	useMarkRead,
	useNotificationStream,
} from "@/notifications/api";

const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The bell in the header: how many notifications are unread, and the newest of them. */
export function Bell() {
	useNotificationStream();
	const notifications = useQuery(notificationsQuery);
	const [open, setOpen] = useState(false);
	const box = useRef<HTMLDivElement>(null);
	const unread = notifications.data?.unread ?? 0;

	useEffect(() => {
		if (!open) return;
		const close = (e: Event) => {
			if (
				e instanceof KeyboardEvent
					? e.key === "Escape"
					: !box.current?.contains(e.target as Node)
			) {
				setOpen(false);
			}
		};
		document.addEventListener("keydown", close);
		document.addEventListener("pointerdown", close);
		return () => {
			document.removeEventListener("keydown", close);
			document.removeEventListener("pointerdown", close);
		};
	}, [open]);

	return (
		<div ref={box} className="relative">
			<button
				type="button"
				aria-expanded={open}
				aria-controls="notifications"
				aria-label={t("notifications.bell", { count: unread })}
				onClick={() => setOpen(!open)}
				className="relative rounded-md border border-slate-300 px-2 py-1 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
			>
				<svg
					aria-hidden="true"
					viewBox="0 0 24 24"
					className="h-5 w-5 fill-none stroke-current stroke-2"
				>
					<path d="M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
					<path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
				</svg>
				{unread > 0 && (
					<span
						aria-hidden="true"
						className="absolute -right-2 -top-2 min-w-5 rounded-full bg-red-600 px-1 text-center text-xs font-semibold text-white"
					>
						{unread > 99 ? "99+" : unread}
					</span>
				)}
			</button>
			{open && (
				<section
					id="notifications"
					aria-label={t("notifications.title")}
					className="absolute right-0 z-20 mt-2 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2 rounded-lg border border-slate-200 bg-white p-3 shadow-lg dark:border-slate-700 dark:bg-slate-900"
				>
					<NotificationList
						items={notifications.data?.notifications ?? []}
						unread={unread}
						onDone={() => setOpen(false)}
					/>
					<FormError error={notifications.error} />
				</section>
			)}
		</div>
	);
}

function NotificationList({
	items,
	unread,
	onDone,
}: {
	items: Notification[];
	unread: number;
	onDone: () => void;
}) {
	const mark = useMarkRead();
	const navigate = useNavigate();
	return (
		<>
			<div className="flex items-center gap-2">
				<h2 className="mr-auto font-medium">{t("notifications.title")}</h2>
				<button
					type="button"
					className={button}
					disabled={unread === 0 || mark.isPending}
					onClick={() => mark.mutate({ all: true })}
				>
					{t("notifications.markAll")}
				</button>
			</div>
			{items.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">
					{t("notifications.none")}
				</p>
			)}
			<ul className="flex max-h-96 flex-col divide-y divide-slate-200 overflow-y-auto dark:divide-slate-800">
				{items.map((n) => {
					const { title, detail } = notificationText(n);
					return (
						<li key={n.id}>
							<button
								type="button"
								className="flex w-full flex-col items-start gap-0.5 py-2 text-left hover:bg-slate-50 dark:hover:bg-slate-800"
								onClick={() => {
									if (!n.readAt) mark.mutate({ ids: [n.id] });
									onDone();
									if (n.link) navigate({ to: n.link });
								}}
							>
								<span className={n.readAt ? "" : "font-semibold"}>
									{n.readAt ? title : t("notifications.unread", { title })}
								</span>
								{detail && (
									<span className="text-slate-600 dark:text-slate-400">
										{detail}
									</span>
								)}
								<time dateTime={n.createdAt} className="text-xs text-slate-500">
									{new Date(n.createdAt).toLocaleString()}
								</time>
							</button>
						</li>
					);
				})}
			</ul>
			<FormError error={mark.error} />
		</>
	);
}
