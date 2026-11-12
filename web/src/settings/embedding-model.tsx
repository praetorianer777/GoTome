import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { Dialog } from "@/components/dialog";
import { errorMessage } from "@/components/form";
import { button } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { formatDateTime, formatSize } from "@/lib/format";

export type ModelList = components["schemas"]["ModelList"];
type ModelChoice = components["schemas"]["ModelChoice"];

/** The models embedding.model may name, with what each costs and how far it is. */
export const embeddingModelsQuery = queryOptions({
	queryKey: ["embedding", "models"],
	queryFn: async () => (await api.GET("/embedding/models")).data,
});

/** Whether choosing the model has most books embedded again. */
export function lacksMostVectors(list: ModelList, name: string): boolean {
	const model = list.models.find((m) => m.name === name);
	return (
		!!model &&
		!model.chosen &&
		list.books > 0 &&
		model.metadata * 2 < list.books
	);
}

const STATES: Record<ModelChoice["readiness"]["state"], MessageKey> = {
	ready: "embeddingModel.state.ready",
	download: "embeddingModel.state.download",
	noRuntime: "embeddingModel.state.noRuntime",
	notDownloaded: "embeddingModel.state.notDownloaded",
	noServer: "embeddingModel.state.noServer",
	serverDown: "embeddingModel.state.serverDown",
};

function readiness(m: ModelChoice): string {
	if (m.readiness.state === "ready" && m.readiness.onServer) {
		return t("embeddingModel.state.readyServer");
	}
	return t(STATES[m.readiness.state]);
}

/** Whether a model not in use has anything for "Remove now" to take away. */
function removable(m: ModelChoice): boolean {
	return !m.chosen && (m.storedBytes > 0 || m.databaseBytes > 0);
}

function unused(m: ModelChoice): string | null {
	if (m.chosen || !m.unusedSince) {
		return null;
	}
	const since = formatDateTime(m.unusedSince);
	return m.removeAt && removable(m)
		? t("embeddingModel.removeAt", { since, at: formatDateTime(m.removeAt) })
		: t("embeddingModel.unusedSince", { since });
}

/** Queues the removal of a model's files and vectors. */
function useRemoveModel() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (name: string) =>
			api.POST("/embedding/models/{modelName}/remove", {
				params: { path: { modelName: name } },
			}),
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: embeddingModelsQuery.queryKey }),
	});
}

/** The model to embed with, chosen from the list, with each one's facts. */
export function EmbeddingModelField({
	label,
	hint,
	value,
	list,
	error,
	onChange,
}: {
	label: string;
	hint?: string;
	value: string;
	list: ModelList;
	error?: string;
	onChange: (name: string) => void;
}) {
	const id = useId();
	const [confirming, setConfirming] = useState<ModelChoice | null>(null);
	const remove = useRemoveModel();
	const described =
		[hint && `${id}-hint`, error && `${id}-error`].filter(Boolean).join(" ") ||
		undefined;
	return (
		<div id="embedding-model" className="flex flex-col gap-1">
			<label htmlFor={id} className="text-sm font-medium">
				{label}
			</label>
			<select
				id={id}
				value={value}
				onChange={(e) => onChange(e.target.value)}
				aria-describedby={described}
				aria-invalid={error ? true : undefined}
				className="rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 aria-invalid:border-red-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100 dark:aria-invalid:border-red-400"
			>
				{list.models.map((m) => (
					<option key={m.name} value={m.name}>
						{m.chosen ? t("embeddingModel.inUse", { name: m.name }) : m.name}
					</option>
				))}
			</select>
			{hint && !error && (
				<p
					id={`${id}-hint`}
					className="text-sm text-slate-500 dark:text-slate-400"
				>
					{hint}
				</p>
			)}
			{error && (
				<p
					id={`${id}-error`}
					className="text-sm text-red-700 dark:text-red-300"
				>
					{error}
				</p>
			)}
			<ul
				aria-label={t("embeddingModel.list")}
				className="mt-2 flex flex-col gap-2"
			>
				{list.models.map((m) => (
					<li
						key={m.name}
						aria-label={m.name}
						className={`rounded-md border px-3 py-2 text-sm ${
							m.name === value
								? "border-sky-600 dark:border-sky-400"
								: "border-slate-200 dark:border-slate-800"
						}`}
					>
						<p className="font-medium">
							{m.name}
							<span className="font-normal text-slate-500 dark:text-slate-400">
								{" "}
								{t("embeddingModel.dimensions", { dimensions: m.dimensions })}
							</span>
						</p>
						<p>{readiness(m)}</p>
						<p className="text-slate-600 dark:text-slate-400">
							{m.diskBytes > 0
								? t("embeddingModel.cost", {
										disk: formatSize(m.diskBytes),
										memory: formatSize(m.memoryBytes),
									})
								: t("embeddingModel.serverOnly")}
						</p>
						<p className="text-slate-600 dark:text-slate-400">
							{t("embeddingModel.vectors", {
								metadata: m.metadata,
								books: list.books,
								content: m.content,
								withText: list.withText,
							})}
						</p>
						{(m.storedBytes > 0 || m.databaseBytes > 0) && (
							<p className="text-slate-600 dark:text-slate-400">
								{t("embeddingModel.stored", {
									disk: formatSize(m.storedBytes),
									database: formatSize(m.databaseBytes),
								})}
							</p>
						)}
						{unused(m) && (
							<p className="text-slate-600 dark:text-slate-400">{unused(m)}</p>
						)}
						{removable(m) && (
							<button
								type="button"
								aria-label={t("embeddingModel.removeNamed", { name: m.name })}
								disabled={remove.isPending}
								onClick={() => setConfirming(m)}
								className={`mt-1 ${button("danger")}`}
							>
								{t("embeddingModel.remove")}
							</button>
						)}
					</li>
				))}
			</ul>
			{remove.isSuccess && remove.variables && (
				<p role="status" className="text-sm text-green-800 dark:text-green-300">
					{t("embeddingModel.removeQueued", { name: remove.variables })}
				</p>
			)}
			{remove.isError && remove.variables && (
				<p role="alert" className="text-sm text-red-700 dark:text-red-300">
					{t("embeddingModel.removeFailed", {
						name: remove.variables,
						error: errorMessage(remove.error),
					})}
				</p>
			)}
			{confirming && (
				<RemoveModelDialog
					model={confirming}
					onConfirm={() => {
						setConfirming(null);
						remove.mutate(confirming.name);
					}}
					onCancel={() => setConfirming(null)}
				/>
			)}
		</div>
	);
}

/** Asks before a model's files and vectors are removed. */
function RemoveModelDialog({
	model,
	onConfirm,
	onCancel,
}: {
	model: ModelChoice;
	onConfirm: () => void;
	onCancel: () => void;
}) {
	const id = useId();
	const cancel = useRef<HTMLButtonElement>(null);
	// The safe answer is the one a stray Enter gives.
	useEffect(() => cancel.current?.focus(), []);
	return (
		<Dialog
			alert
			title={t("embeddingModel.removeConfirm.title", { name: model.name })}
			describedBy={`${id}-body`}
			onClose={onCancel}
		>
			<p
				id={`${id}-body`}
				className="text-sm text-slate-700 dark:text-slate-300"
			>
				{t("embeddingModel.removeConfirm.body", {
					name: model.name,
					disk: formatSize(model.storedBytes),
					database: formatSize(model.databaseBytes),
				})}
			</p>
			<div className="flex flex-wrap justify-end gap-2">
				<button
					ref={cancel}
					type="button"
					onClick={onCancel}
					className={button("secondary", "md")}
				>
					{t("embeddingModel.removeConfirm.cancel")}
				</button>
				<button
					type="button"
					onClick={onConfirm}
					className={button("danger", "md")}
				>
					{t("embeddingModel.removeConfirm.remove")}
				</button>
			</div>
		</Dialog>
	);
}

/** Asks before a switch that has most books embedded again. */
export function SwitchModelDialog({
	model,
	list,
	onConfirm,
	onCancel,
}: {
	model: string;
	list: ModelList;
	onConfirm: () => void;
	onCancel: () => void;
}) {
	const id = useId();
	const done = list.models.find((m) => m.name === model)?.metadata ?? 0;
	const cancel = useRef<HTMLButtonElement>(null);
	// The safe answer is the one a stray Enter gives.
	useEffect(() => cancel.current?.focus(), []);
	return (
		<Dialog
			alert
			title={t("embeddingModel.confirm.title", { model })}
			describedBy={`${id}-body`}
			onClose={onCancel}
		>
			<p
				id={`${id}-body`}
				className="text-sm text-slate-700 dark:text-slate-300"
			>
				{t("embeddingModel.confirm.body", {
					model,
					metadata: done,
					books: list.books,
				})}
			</p>
			<div className="flex flex-wrap justify-end gap-2">
				<button
					ref={cancel}
					type="button"
					onClick={onCancel}
					className="rounded-md border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
				>
					{t("embeddingModel.confirm.cancel")}
				</button>
				<button
					type="button"
					onClick={onConfirm}
					className="rounded-md bg-brand-strong px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-800 dark:bg-brand dark:text-ink dark:hover:bg-sky-300"
				>
					{t("embeddingModel.confirm.switch")}
				</button>
			</div>
		</Dialog>
	);
}
