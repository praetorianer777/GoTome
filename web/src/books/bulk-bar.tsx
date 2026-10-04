import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Fragment, useState } from "react";
import {
	BULK_FIELDS,
	BULK_LISTS,
	type BulkAction,
	type BulkForm,
	type BulkMode,
	type BulkRequest,
	bulkChangeOf,
	emptyBulkForm,
	useStartBulk,
} from "@/books/bulk";
import {
	type ReadingStatus,
	STATUS_LABELS,
	STATUSES,
	useSetReadingBulk,
} from "@/books/reading";
import { collectionsQuery, useAddToCollection } from "@/books/collections";
import { FormError } from "@/components/form";
import { type MessageKey, t } from "@/i18n";

const quietButton =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";
const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

/**
 * What the selected books can have done to them. The books are those
 * ticked, or, once every book that matches is selected, the list's library
 * and filter, which the server turns into the books when it is asked.
 */
export function BulkBar({
	count,
	canEdit,
	all,
	selection,
	onSelectAll,
	onClear,
}: {
	count: number;
	/** Whether the person may change the books themselves, not only their own status. */
	canEdit: boolean;
	all: boolean;
	selection: Pick<BulkRequest, "books" | "library" | "filter">;
	onSelectAll: () => void;
	onClear: () => void;
}) {
	const start = useStartBulk();
	const navigate = useNavigate();
	const [editing, setEditing] = useState(false);
	const setStatus = useSetReadingBulk();
	const collections = useQuery(collectionsQuery());
	const mine = collections.data?.filter((c) => c.mine) ?? [];
	const collect = useAddToCollection();
	const none = !all && count === 0;
	const run = (action: BulkAction, change?: BulkRequest["change"]) =>
		start.mutate(
			{ ...selection, action, change },
			{
				onSuccess: (started) => {
					if (started) {
						navigate({ to: "/bulk/$bulkId", params: { bulkId: started.id } });
					}
				},
			},
		);

	return (
		<section
			aria-label={t("bulk.title")}
			className="flex flex-col gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm dark:border-slate-800 dark:bg-slate-900"
		>
			<div className="flex flex-wrap items-center gap-2">
				<span className="mr-2 font-medium" aria-live="polite">
					{all ? t("bulk.allSelected") : t("bulk.selected", { count })}
				</span>
				{!all && (
					<button type="button" className={quietButton} onClick={onSelectAll}>
						{t("bulk.selectAll")}
					</button>
				)}
				<button
					type="button"
					className={quietButton}
					disabled={none}
					onClick={onClear}
				>
					{t("bulk.clear")}
				</button>
				<span className="grow" />
				<select
					aria-label={t("bulk.status")}
					value=""
					disabled={none || setStatus.isPending}
					onChange={(e) =>
						setStatus.mutate({
							...selection,
							status: e.target.value as ReadingStatus,
						})
					}
					className={control}
				>
					<option value="" disabled>
						{t("bulk.status")}
					</option>
					{STATUSES.map((s) => (
						<option key={s} value={s}>
							{t(STATUS_LABELS[s])}
						</option>
					))}
				</select>
				{mine.length > 0 && (
					<select
						aria-label={t("bulk.collect")}
						value=""
						disabled={none || collect.isPending}
						onChange={(e) => collect.mutate({ ...selection, id: e.target.value })}
						className={control}
					>
						<option value="" disabled>
							{t("bulk.collect")}
						</option>
						{mine.map((c) => (
							<option key={c.id} value={c.id}>
								{c.name}
							</option>
						))}
					</select>
				)}
				{canEdit && (
					<>
				<button
					type="button"
					aria-expanded={editing}
					className={quietButton}
					disabled={none || start.isPending}
					onClick={() => setEditing(!editing)}
				>
					{t("bulk.edit")}
				</button>
				<button
					type="button"
					className={quietButton}
					disabled={none || start.isPending}
					onClick={() => run("fetch")}
				>
					{t("bulk.fetch")}
				</button>
				<button
					type="button"
					className={quietButton}
					disabled={none || start.isPending}
					onClick={() => run("writeBack")}
				>
					{t("bulk.writeBack")}
				</button>
					</>
				)}
			</div>
			{setStatus.data && (
				<p role="status">{t("bulk.statusDone", { count: setStatus.data.changed })}</p>
			)}
			{collect.data && (
				<p role="status">
					{t("bulk.collectDone", {
						count: collect.data.added,
						name: mine.find((c) => c.id === collect.variables?.id)?.name ?? "",
					})}
				</p>
			)}
			<FormError error={collect.error} />
			<FormError error={setStatus.error} />
			{!editing && <FormError error={start.error} />}
			{editing && (
				<BulkEditForm
					busy={start.isPending}
					error={start.error}
					onCancel={() => setEditing(false)}
					onSubmit={(change) => run("edit", change)}
				/>
			)}
		</section>
	);
}

const MODES: Record<BulkMode, MessageKey> = {
	"": "bulk.mode.leave",
	set: "bulk.mode.set",
	add: "bulk.mode.add",
	remove: "bulk.mode.remove",
};

function BulkEditForm({
	busy,
	error,
	onCancel,
	onSubmit,
}: {
	busy: boolean;
	error: Error | null;
	onCancel: () => void;
	onSubmit: (change: NonNullable<BulkRequest["change"]>) => void;
}) {
	const [form, setForm] = useState<BulkForm>(emptyBulkForm);
	const [empty, setEmpty] = useState(false);
	return (
		<form
			className="flex flex-col gap-3"
			onSubmit={(e) => {
				e.preventDefault();
				const change = bulkChangeOf(form);
				setEmpty(!change);
				if (change) {
					onSubmit(change);
				}
			}}
		>
			<p className="max-w-2xl text-slate-600 dark:text-slate-400">
				{t("bulk.form.intro")}
			</p>
			<div className="grid gap-2 sm:grid-cols-[auto_auto_1fr] sm:items-center">
				{BULK_FIELDS.map((field) => {
					const modes: BulkMode[] = BULK_LISTS.includes(field)
						? ["", "set", "add", "remove"]
						: ["", "set"];
					const { mode, value } = form[field];
					const label = t(`bulk.field.${field}`);
					return (
						<Fragment key={field}>
							<span className="font-medium">{label}</span>
							<select
								aria-label={t("bulk.form.modeOf", { field: label })}
								value={mode}
								onChange={(e) =>
									setForm({
										...form,
										[field]: { mode: e.target.value as BulkMode, value },
									})
								}
								className={control}
							>
								{modes.map((m) => (
									<option key={m} value={m}>
										{t(MODES[m])}
									</option>
								))}
							</select>
							<input
								aria-label={t("bulk.form.valueOf", { field: label })}
								value={value}
								disabled={!mode}
								placeholder={
									mode === ""
										? ""
										: BULK_LISTS.includes(field)
											? t("bulk.form.names")
											: mode === "set"
												? t("bulk.form.emptyRemoves")
												: ""
								}
								onChange={(e) =>
									setForm({ ...form, [field]: { mode, value: e.target.value } })
								}
								className={`${control} disabled:opacity-50`}
							/>
						</Fragment>
					);
				})}
			</div>
			<label className="flex items-center gap-2">
				<input
					type="checkbox"
					checked={form.includeLocked}
					onChange={(e) =>
						setForm({ ...form, includeLocked: e.target.checked })
					}
				/>
				{t("bulk.form.includeLocked")}
			</label>
			{empty && (
				<p role="alert" className="text-red-700 dark:text-red-400">
					{t("bulk.form.nothing")}
				</p>
			)}
			<FormError error={error} />
			<div className="flex gap-2">
				<button
					type="submit"
					disabled={busy}
					className="rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 disabled:opacity-60"
				>
					{t("bulk.form.start")}
				</button>
				<button type="button" className={quietButton} onClick={onCancel}>
					{t("bulk.form.cancel")}
				</button>
			</div>
		</form>
	);
}
