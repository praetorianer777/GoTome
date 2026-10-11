import {
	queryOptions,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";
import { ownDevicesQuery } from "@/users/api";

type SendRequest = components["schemas"]["SendRequest"];

function deliveriesQuery(bookId: string) {
	return queryOptions({
		queryKey: ["deliveries", bookId],
		queryFn: async () =>
			(
				await api.GET("/books/{bookId}/deliveries", {
					params: { path: { bookId } },
				})
			).data?.deliveries ?? [],
	});
}

/** Sends books to one of the person's devices. */
export function useSendToDevice() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async ({ device, ...body }: SendRequest & { device: string }) =>
			(
				await api.POST("/devices/{deviceId}/books", {
					params: { path: { deviceId: device } },
					body,
				})
			).data,
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["deliveries"] }),
	});
}

function useTakeBack() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/deliveries/{deliveryId}", {
				params: { path: { deliveryId: id } },
			});
		},
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["deliveries"] }),
	});
}

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

/**
 * Sends the book to one of the person's e-readers, and says where it waits
 * and where it has arrived. Without a device, it says where to add one.
 */
export function SendToDevice({ bookId }: { bookId: string }) {
	const devices = useQuery(ownDevicesQuery);
	const deliveries = useQuery(deliveriesQuery(bookId));
	const send = useSendToDevice();
	const takeBack = useTakeBack();
	if (!devices.data) return null;
	return (
		<section
			aria-label={t("deliveries.title")}
			className="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 text-sm dark:border-slate-800"
		>
			{devices.data.length === 0 ? (
				<p className="text-slate-600 dark:text-slate-400">
					{t("deliveries.noDevice")}{" "}
					<Link
						to="/profile"
						className="text-brand-strong hover:underline dark:text-sky-400"
					>
						{t("deliveries.addDevice")}
					</Link>
				</p>
			) : (
				<select
					aria-label={t("deliveries.send")}
					value=""
					disabled={send.isPending}
					onChange={(e) =>
						send.mutate({ device: e.target.value, books: [bookId] })
					}
					className={`${control} self-start`}
				>
					<option value="" disabled>
						{t("deliveries.send")}
					</option>
					{devices.data.map((d) => (
						<option key={d.id} value={d.id}>
							{d.name}
						</option>
					))}
				</select>
			)}
			{send.data?.queued === 0 && (
				<p role="status">{t("deliveries.notQueued")}</p>
			)}
			{(deliveries.data?.length ?? 0) > 0 && (
				<ul className="flex flex-col gap-1">
					{deliveries.data?.map((d) => (
						<li key={d.id} className="flex flex-wrap items-center gap-x-3">
							<span>
								{d.deliveredAt
									? t("deliveries.delivered", {
											device: d.deviceName,
											date: formatDate(d.deliveredAt),
										})
									: t("deliveries.waiting", { device: d.deviceName })}
							</span>
							{!d.deliveredAt && (
								<button
									type="button"
									disabled={takeBack.isPending}
									onClick={() => takeBack.mutate(d.id)}
									className="rounded-md border border-slate-300 px-2 py-0.5 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
								>
									{t("deliveries.takeBack")}
								</button>
							)}
						</li>
					))}
				</ul>
			)}
			<FormError error={send.error ?? takeBack.error ?? deliveries.error} />
		</section>
	);
}
