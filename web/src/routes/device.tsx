import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { Cover } from "@/books/cover";
import { FormError } from "@/components/form";
import { Loading } from "@/components/ui";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";
import {
	type DeviceBook,
	deviceBooksQuery,
	useTakeOffDevice,
} from "@/users/api";

const button =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** One of the person's e-readers: the books sent to it, and how far each got. */
export function DevicePage({ deviceId }: { deviceId: string }) {
	const pages = useInfiniteQuery(deviceBooksQuery(deviceId));
	const takeOff = useTakeOffDevice();
	if (pages.error instanceof ApiError && pages.error.status === 404) {
		return (
			<p className="text-slate-600 dark:text-slate-400">
				{t("deviceBooks.notFound")}
			</p>
		);
	}
	const device = pages.data?.pages[0]?.device;
	const books = pages.data?.pages.flatMap((p) => p?.books ?? []) ?? [];
	return (
		<div className="flex flex-col gap-6">
			<div>
				<Link
					to="/profile"
					className="text-sm text-brand-strong hover:underline dark:text-brand"
				>
					{t("deviceBooks.back")}
				</Link>
				<h1 className="text-2xl font-semibold">{device?.name}</h1>
				{device && (
					<p className="mt-1 text-slate-600 dark:text-slate-400">
						{device.lastUsedAt
							? t("devices.lastUsed", { date: formatDate(device.lastUsedAt) })
							: t("devices.neverUsed")}
					</p>
				)}
			</div>
			<FormError error={pages.error ?? takeOff.error} />
			{pages.isPending && <Loading />}
			{pages.isSuccess && books.length === 0 && (
				<p role="status" className="text-slate-600 dark:text-slate-400">
					{t("deviceBooks.none")}
				</p>
			)}
			{books.length > 0 && (
				<ul
					aria-label={t("deviceBooks.list")}
					className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800"
				>
					{books.map((b) => (
						<li
							key={b.deliveryId}
							aria-label={b.book.title}
							className="flex flex-wrap items-center gap-3 py-2"
						>
							<Link
								to="/books/$bookId"
								params={{ bookId: b.book.id }}
								className="flex min-w-0 flex-1 items-center gap-3"
							>
								<span className="w-10 shrink-0">
									<Cover book={b.book} size="small" />
								</span>
								<span className="flex min-w-0 flex-col">
									<span className="truncate font-medium">{b.book.title}</span>
									{b.book.authors.length > 0 && (
										<span className="truncate text-sm text-slate-600 dark:text-slate-400">
											{b.book.authors.join(", ")}
										</span>
									)}
								</span>
							</Link>
							<span className="text-sm text-slate-600 dark:text-slate-400">
								{stateText(b)}
							</span>
							{b.removable && (
								<button
									type="button"
									disabled={takeOff.isPending}
									onClick={() => takeOff.mutate(b.deliveryId)}
									aria-label={t("deviceBooks.takeOffNamed", {
										title: b.book.title,
									})}
									className={button}
								>
									{t("deviceBooks.takeOff")}
								</button>
							)}
						</li>
					))}
				</ul>
			)}
			{pages.hasNextPage && (
				<button
					type="button"
					disabled={pages.isFetchingNextPage}
					onClick={() => pages.fetchNextPage()}
					className={`${button} self-start`}
				>
					{t("deviceBooks.more")}
				</button>
			)}
		</div>
	);
}

function stateText(b: DeviceBook): string {
	switch (b.state) {
		case "delivered":
			return t("deviceBooks.delivered", {
				date: formatDate(b.deliveredAt ?? b.queuedAt),
			});
		case "synced":
			return t("deviceBooks.synced", {
				date: formatDate(b.syncedAt ?? b.queuedAt),
			});
		default:
			return t("deviceBooks.waiting", { date: formatDate(b.queuedAt) });
	}
}
