import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type DragEvent, useEffect, useId, useRef, useState } from "react";
import { type CurrentUser, can } from "@/auth/session";
import { UPLOAD_FORMATS, type Uploaded, uploadFile } from "@/books/upload";
import { errorMessage, FormError, fieldErrors } from "@/components/form";
import { t } from "@/i18n";
import { formatSize } from "@/lib/format";
import { librariesQuery } from "@/libraries/api";
import { storageQuery } from "@/users/api";
import { StorageUse } from "@/users/storage";

interface Item {
	id: number;
	file: File;
	libraryId: string;
	state: "waiting" | "sending" | "done" | "failed";
	sent: number;
	result?: Uploaded;
	error?: unknown;
}

function patched(id: number, patch: Partial<Item>) {
	return (items: Item[]) =>
		items.map((item) => (item.id === id ? { ...item, ...patch } : item));
}

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

export function Upload({ user }: { user: CurrentUser }) {
	const libraries = useQuery(librariesQuery);
	const managed = (libraries.data ?? []).filter((l) => l.mode === "managed");
	const [chosen, setChosen] = useState<string>();
	const target = managed.find((l) => l.id === chosen) ?? managed[0];
	const [items, setItems] = useState<Item[]>([]);
	const [dragging, setDragging] = useState(false);
	const nextId = useRef(1);
	const queryClient = useQueryClient();
	const inputId = useId();

	function add(files: FileList | null) {
		if (!files || !target) {
			return;
		}
		const added = Array.from(files).map(
			(file): Item => ({
				id: nextId.current++,
				file,
				libraryId: target.id,
				state: "waiting",
				sent: 0,
			}),
		);
		setItems((current) => [...current, ...added]);
	}


	// One file at a time: several at once would each be slower, and the
	// first to finish is what a person looks for.
	const sending = items.some((i) => i.state === "sending");
	const next = items.find((i) => i.state === "waiting");
	// Development runs every effect twice; the file is sent once.
	const started = useRef(new Set<number>());
	useEffect(() => {
		if (sending || !next || started.current.has(next.id)) {
			return;
		}
		started.current.add(next.id);
		setItems(patched(next.id, { state: "sending" }));
		uploadFile(next.libraryId, next.file, (sent) => setItems(patched(next.id, { sent })))
			.then((result) => {
				setItems(patched(next.id, { state: "done", sent: 1, result }));
				if (result.outcome === "added") {
					queryClient.invalidateQueries({ queryKey: ["books"] });
					queryClient.invalidateQueries({ queryKey: librariesQuery.queryKey });
					queryClient.invalidateQueries({ queryKey: storageQuery.queryKey });
				}
			})
			.catch((error: unknown) => setItems(patched(next.id, { state: "failed", error })));
	}, [sending, next, queryClient]);

	const busy = sending || next !== undefined;
	useEffect(() => {
		if (!busy) {
			return;
		}
		// Leaving the page would cut off what is still being sent.
		const warn = (event: BeforeUnloadEvent) => event.preventDefault();
		window.addEventListener("beforeunload", warn);
		return () => window.removeEventListener("beforeunload", warn);
	}, [busy]);

	function drop(event: DragEvent) {
		event.preventDefault();
		setDragging(false);
		add(event.dataTransfer.files);
	}

	const finished = items.some(
		(i) => i.state === "done" || i.state === "failed",
	);

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-1">
				<h1 className="text-2xl font-semibold">{t("upload.title")}</h1>
				<StorageUse />
			</div>
			{libraries.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={libraries.error} />

			{libraries.isSuccess && managed.length === 0 && (
				<div className="rounded-lg border border-dashed border-slate-300 px-6 py-10 text-center dark:border-slate-700">
					<p className="mx-auto max-w-md text-slate-600 dark:text-slate-400">
						{t(
							can(user, "storage:manage")
								? "upload.none.admin"
								: "upload.none.other",
						)}
					</p>
					{can(user, "storage:manage") && (
						<Link
							to="/admin/libraries"
							className="mt-4 inline-block rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800"
						>
							{t("library.none.add")}
						</Link>
					)}
				</div>
			)}

			{target && (
				<>
					<p className="max-w-2xl text-slate-600 dark:text-slate-400">
						{t("upload.intro")}
					</p>
					{managed.length > 1 && (
						<label className="flex items-center gap-2 text-sm">
							<span>{t("upload.library")}</span>
							<select
								value={target.id}
								onChange={(e) => setChosen(e.target.value)}
								className={control}
							>
								{managed.map((l) => (
									<option key={l.id} value={l.id}>
										{l.name}
									</option>
								))}
							</select>
						</label>
					)}

					<section
						aria-label={t("upload.dropZone")}
						onDragOver={(e) => {
							e.preventDefault();
							setDragging(true);
						}}
						onDragLeave={() => setDragging(false)}
						onDrop={drop}
						data-dragging={dragging || undefined}
						className="flex flex-col items-center gap-3 rounded-lg border-2 border-dashed border-slate-300 px-6 py-12 text-center data-dragging:border-sky-600 data-dragging:bg-sky-50 dark:border-slate-700 dark:data-dragging:bg-sky-950"
					>
						<p className="text-lg">
							{t("upload.drop", { library: target.name })}
						</p>
						<label
							htmlFor={inputId}
							className="cursor-pointer rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 has-focus-visible:outline-2 has-focus-visible:outline-offset-2"
						>
							{t("upload.choose")}
						</label>
						<input
							id={inputId}
							type="file"
							multiple
							accept={UPLOAD_FORMATS.join(",")}
							className="sr-only"
							onChange={(e) => {
								add(e.target.files);
								e.target.value = "";
							}}
						/>
						<p className="text-sm text-slate-600 dark:text-slate-400">
							{t("upload.formats")}
						</p>
					</section>
				</>
			)}

			{items.length > 0 && (
				<section aria-labelledby="uploads" className="flex flex-col gap-3">
					<div className="flex items-center gap-4">
						<h2 id="uploads" className="text-lg font-medium">
							{t("upload.files")}
						</h2>
						{finished && (
							<button
								type="button"
								onClick={() =>
									setItems((current) =>
										current.filter(
											(i) => i.state === "waiting" || i.state === "sending",
										),
									)
								}
								className="ml-auto rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("upload.clear")}
							</button>
						)}
					</div>
					<ul className="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
						{items.map((item) => (
							<UploadRow key={item.id} item={item} />
						))}
					</ul>
				</section>
			)}
		</div>
	);
}

function UploadRow({ item }: { item: Item }) {
	return (
		<li aria-label={item.file.name} className="flex flex-col gap-1 px-4 py-3">
			<div className="flex flex-wrap items-baseline gap-x-3">
				<span className="font-medium break-all">{item.file.name}</span>
				<span className="text-sm text-slate-500">
					{formatSize(item.file.size)}
				</span>
			</div>
			{(item.state === "waiting" || item.state === "sending") && (
				<div className="flex items-center gap-3 text-sm">
					<progress
						value={item.sent}
						max={1}
						aria-label={t("upload.progress", { name: item.file.name })}
						className="h-2 flex-1 accent-sky-700"
					/>
					<span className="w-28 text-right text-slate-600 dark:text-slate-400">
						{item.state === "waiting"
							? t("upload.waiting")
							: t("upload.sending", { percent: Math.round(item.sent * 100) })}
					</span>
				</div>
			)}
			{item.result && <Outcome result={item.result} />}
			{item.state === "failed" && (
				<p className="text-sm text-red-700 dark:text-red-300">
					{fieldErrors(item.error).file ?? errorMessage(item.error)}
				</p>
			)}
		</li>
	);
}

function Outcome({ result }: { result: Uploaded }) {
	const link = (
		<Link
			to="/books/$bookId"
			params={{ bookId: result.bookId }}
			className="font-medium text-sky-800 hover:underline dark:text-sky-300"
		>
			{t("upload.open")}
		</Link>
	);
	return result.outcome === "added" ? (
		<p className="flex flex-wrap gap-x-3 text-sm text-green-800 dark:text-green-300">
			<span>{t("upload.added")}</span>
			{link}
		</p>
	) : (
		<p className="flex flex-wrap gap-x-3 text-sm text-amber-800 dark:text-amber-300">
			<span>{t("upload.duplicate", { title: result.title })}</span>
			{link}
		</p>
	);
}
