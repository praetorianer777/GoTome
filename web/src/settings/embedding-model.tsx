import { queryOptions } from "@tanstack/react-query";
import { useEffect, useId, useRef } from "react";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { type MessageKey, t } from "@/i18n";
import { formatSize } from "@/lib/format";

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
					</li>
				))}
			</ul>
		</div>
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
		<div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 px-4">
			<div
				role="alertdialog"
				aria-modal="true"
				aria-labelledby={`${id}-title`}
				aria-describedby={`${id}-body`}
				onKeyDown={(e) => {
					if (e.key === "Escape") {
						onCancel();
					}
				}}
				className="flex w-full max-w-md flex-col gap-4 rounded-lg bg-white p-5 shadow-xl dark:bg-slate-900"
			>
				<h2 id={`${id}-title`} className="text-lg font-semibold">
					{t("embeddingModel.confirm.title", { model })}
				</h2>
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
						className="rounded-md bg-brand-strong px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-800"
					>
						{t("embeddingModel.confirm.switch")}
					</button>
				</div>
			</div>
		</div>
	);
}
