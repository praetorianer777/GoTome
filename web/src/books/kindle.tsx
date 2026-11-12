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
import { type MessageKey, t } from "@/i18n";
import { formatDate, formatSize } from "@/lib/format";

type SendRequest = components["schemas"]["SendRequest"];
type KindleSend = components["schemas"]["KindleSendView"];

/** Whether books can be sent to a Kindle, from where, and the person's address. */
export const kindleQuery = queryOptions({
	queryKey: ["kindle"],
	queryFn: async () => (await api.GET("/me/kindle")).data,
});

function kindleSendsQuery(bookId: string) {
	return queryOptions({
		queryKey: ["kindle-sends", bookId],
		queryFn: async () =>
			(
				await api.GET("/books/{bookId}/kindle", {
					params: { path: { bookId } },
				})
			).data?.sends ?? [],
		// A send is a job: its outcome shows by itself.
		refetchInterval: (query) =>
			query.state.data?.some(
				(s) => s.state === "queued" || s.state === "sending",
			)
				? 3000
				: false,
	});
}

/** Sends books to the person's Kindle by e-mail. */
export function useSendToKindle() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: SendRequest) =>
			(await api.POST("/kindle/sends", { body })).data,
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["kindle-sends"] }),
	});
}

const SKIPPED: Record<string, MessageKey> = {
	unavailable: "kindle.skipped.unavailable",
	tooLarge: "kindle.skipped.tooLarge",
	pending: "kindle.skipped.pending",
};

/** What became of the books a request did not queue. */
export function skippedText(
	skipped: { reason: string }[] | undefined,
	maxBytes: number,
): string[] {
	const reasons = new Set((skipped ?? []).map((s) => s.reason));
	return [...reasons].map((r) =>
		t(SKIPPED[r] ?? "kindle.skipped.unavailable", { size: formatSize(maxBytes) }),
	);
}

function sendText(s: KindleSend): string {
	switch (s.state) {
		case "sent":
			return t("kindle.sent", {
				date: formatDate(s.finishedAt ?? s.createdAt),
				address: s.address,
			});
		case "failed":
			return t("kindle.failed", { message: s.message ?? "" });
		default:
			return t("kindle.waiting", { address: s.address });
	}
}

/**
 * Sends the book's EPUB to the person's Kindle and says how the latest
 * sends went. Not shown while no administrator has set up a mail server.
 */
export function SendToKindle({ bookId }: { bookId: string }) {
	const status = useQuery(kindleQuery);
	const sends = useQuery(kindleSendsQuery(bookId));
	const send = useSendToKindle();
	if (!status.data?.available) return null;
	const skipped = skippedText(send.data?.skipped, status.data.maxBytes);
	return (
		<section
			aria-label={t("kindle.title")}
			className="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 text-sm dark:border-slate-800"
		>
			{status.data.address ? (
				<button
					type="button"
					disabled={send.isPending}
					onClick={() => send.mutate({ books: [bookId] })}
					className="min-w-0 max-w-full self-start rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
				>
					{t("kindle.send")}
				</button>
			) : (
				<p className="text-slate-600 dark:text-slate-400">
					{t("kindle.noAddress")}{" "}
					<Link
						to="/profile"
						className="text-brand-strong hover:underline dark:text-brand"
					>
						{t("kindle.addAddress")}
					</Link>
				</p>
			)}
			<p className="text-slate-600 dark:text-slate-400">
				{t("kindle.approve", { sender: status.data.sender ?? "" })}
			</p>
			{skipped.map((text) => (
				<p key={text} role="status">
					{text}
				</p>
			))}
			{(sends.data?.length ?? 0) > 0 && (
				<ul className="flex flex-col gap-1">
					{sends.data?.map((s) => (
						<li key={s.id} className="break-words">
							{sendText(s)}
						</li>
					))}
				</ul>
			)}
			<FormError error={send.error ?? sends.error} />
		</section>
	);
}
