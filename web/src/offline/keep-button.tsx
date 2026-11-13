import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type BookDetail } from "@/books/api";
import { FormError } from "@/components/form";
import { button } from "@/components/ui";
import { t } from "@/i18n";
import { type BookFile, type KeptFile, keepFile, keptQuery, removeKept } from "./kept";
import { workerSupported } from "./support";

/** Whether a file is one a reader or the player opens from a kept copy. */
export function keepable(file: BookFile): boolean {
	return !file.missing && (file.kind === "ebook" || file.kind === "audio");
}

function useKeep(book: BookDetail) {
	const queryClient = useQueryClient();
	const done = () => queryClient.invalidateQueries({ queryKey: keptQuery.queryKey });
	const keep = useMutation({
		mutationFn: async (files: BookFile[]) => {
			for (const file of files) await keepFile(book, file);
		},
		onSettled: done,
	});
	const remove = useMutation({
		mutationFn: async (files: BookFile[]) => {
			for (const file of files) await removeKept(file.id);
		},
		onSettled: done,
	});
	return { keep, remove };
}

/** "Keep offline" for one file, or "Remove offline copy" when it is kept. */
export function KeepFileButton({ book, file }: { book: BookDetail; file: BookFile }) {
	const kept = useQuery(keptQuery);
	const { keep, remove } = useKeep(book);
	if (!workerSupported() || !keepable(file)) return null;
	const isKept = kept.data?.some((k: KeptFile) => k.fileId === file.id);
	const pending = keep.isPending || remove.isPending;
	return (
		<>
			<button
				type="button"
				disabled={pending}
				onClick={() => (isKept ? remove : keep).mutate([file])}
				aria-label={t(isKept ? "offline.removeNamed" : "offline.keepNamed", { name: file.name })}
				className={button()}
			>
				{keep.isPending ? t("offline.keeping") : isKept ? t("offline.remove") : t("offline.keep")}
			</button>
			<FormError error={keep.error ?? remove.error} />
		</>
	);
}

/** One button for all the parts of an audiobook. */
export function KeepAudioButton({ book }: { book: BookDetail }) {
	const kept = useQuery(keptQuery);
	const { keep, remove } = useKeep(book);
	const parts = book.files.filter((f) => f.kind === "audio" && !f.missing);
	if (!workerSupported() || parts.length < 2) return null;
	const all = parts.every((p) => kept.data?.some((k: KeptFile) => k.fileId === p.id));
	const pending = keep.isPending || remove.isPending;
	return (
		<>
			<button
				type="button"
				disabled={pending}
				onClick={() =>
					all
						? remove.mutate(parts)
						: keep.mutate(parts.filter((p) => !kept.data?.some((k: KeptFile) => k.fileId === p.id)))
				}
				className={button()}
			>
				{keep.isPending ? t("offline.keeping") : all ? t("offline.removeAudio") : t("offline.keepAudio")}
			</button>
			<FormError error={keep.error ?? remove.error} />
		</>
	);
}
