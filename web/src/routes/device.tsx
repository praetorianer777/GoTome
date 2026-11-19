import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { collectionsQuery } from "@/books/collections";
import { Cover } from "@/books/cover";
import { smartShelvesQuery } from "@/books/smart-shelves";
import { FormError } from "@/components/form";
import { Loading } from "@/components/ui";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";
import {
	type DeviceBook,
	type DeviceSource,
	deviceBooksQuery,
	deviceSourcesQuery,
	useSetDeviceSources,
	useSetDeviceKepub,
	useTakeOffDevice,
} from "@/users/api";

const button =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** One of the person's e-readers: the books sent to it, and how far each got. */
export function DevicePage({ deviceId }: { deviceId: string }) {
	const pages = useInfiniteQuery(deviceBooksQuery(deviceId));
	const takeOff = useTakeOffDevice();
	const setKepub = useSetDeviceKepub();
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
			{device && (
				<div className="flex flex-col gap-1">
					<label className="flex items-center gap-2">
						<input
							type="checkbox"
							checked={device.kepub}
							disabled={setKepub.isPending}
							onChange={(e) =>
								setKepub.mutate({ id: device.id, kepub: e.target.checked })
							}
						/>
						{t("devices.kepub")}
					</label>
					<p className="text-sm text-slate-600 dark:text-slate-400">
						{t("devices.kepubHint")}
					</p>
				</div>
			)}
			{device && <Follows deviceId={device.id} />}
			<FormError error={pages.error ?? takeOff.error ?? setKepub.error} />
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
							<span className="flex flex-col text-sm text-slate-600 dark:text-slate-400">
								<span>{stateText(b)}</span>
								{b.origins.length > 0 && (
									<span>
										{t("deviceBooks.from", {
											shelves: b.origins.map(originName).join(", "),
										})}
									</span>
								)}
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
			return t(b.sentFixed ? "deviceBooks.deliveredFixed" : "deviceBooks.delivered", {
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

function originName(o: { kind: string; name?: string }): string {
	if (o.kind === "reading") return t("devices.follow.reading");
	if (o.kind === "wishlist") return t("devices.follow.wishlist");
	return o.name ?? "";
}

/** The collections, smart shelves and lists a device is kept stocked from. */
function Follows({ deviceId }: { deviceId: string }) {
	const sources = useQuery(deviceSourcesQuery(deviceId));
	const collections = useQuery(collectionsQuery());
	const smart = useQuery(smartShelvesQuery);
	const set = useSetDeviceSources(deviceId);
	const followed = sources.data ?? [];
	const follows = (kind: string, shelf?: string) =>
		followed.some((s) => s.kind === kind && s.shelf === shelf);
	const toggle = (kind: DeviceSource["kind"], shelf: string | undefined, on: boolean) =>
		set.mutate(
			on
				? [...followed, { kind, shelf }]
				: followed.filter((s) => !(s.kind === kind && s.shelf === shelf)),
		);
	const choices: { key: string; kind: DeviceSource["kind"]; shelf?: string; label: string }[] = [
		{ key: "reading", kind: "reading", label: t("devices.follow.reading") },
		{ key: "wishlist", kind: "wishlist", label: t("devices.follow.wishlist") },
		...(collections.data ?? []).map((c) => ({
			key: `c-${c.id}`, kind: "collection" as const, shelf: c.id, label: c.name,
		})),
		...(smart.data ?? []).map((x) => ({
			key: `s-${x.id}`, kind: "smart" as const, shelf: x.id,
			label: t("devices.follow.smart", { name: x.name }),
		})),
	];
	return (
		<fieldset className="flex flex-col gap-1" disabled={sources.isPending}>
			<legend className="font-medium">{t("devices.follow.title")}</legend>
			<p className="text-sm text-slate-600 dark:text-slate-400">
				{t("devices.follow.hint")}
			</p>
			{choices.map((c) => (
				<label key={c.key} className="flex items-center gap-2">
					<input
						type="checkbox"
						checked={follows(c.kind, c.shelf)}
						disabled={set.isPending}
						onChange={(e) => toggle(c.kind, c.shelf, e.target.checked)}
					/>
					{c.label}
				</label>
			))}
			<FormError error={set.error} />
		</fieldset>
	);
}
