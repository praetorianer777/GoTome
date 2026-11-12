import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { button, card, Loading, PageHeader } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { formatDateTime } from "@/lib/format";
import {
	deliveriesQuery,
	type Webhook,
	type WebhookRequest,
	useCreateWebhook,
	useDeleteWebhook,
	useTestWebhook,
	useUpdateWebhook,
	webhooksQuery,
} from "@/webhooks/api";

function lastKey(status: string): MessageKey {
	switch (status) {
		case "delivered":
			return "webhooks.last.delivered";
		case "failed":
			return "webhooks.last.failed";
		default:
			return "webhooks.last.pending";
	}
}

function statusKey(status: string): MessageKey {
	switch (status) {
		case "delivered":
			return "webhooks.status.delivered";
		case "failed":
			return "webhooks.status.failed";
		default:
			return "webhooks.status.pending";
	}
}

export function AdminWebhooks() {
	const hooks = useQuery(webhooksQuery);
	const kinds = hooks.data?.kinds ?? [];
	return (
		<div className="flex flex-col gap-8">
			<PageHeader title={t("webhooks.title")} intro={t("webhooks.intro")} />
			<section aria-labelledby="existing-webhooks" className="flex flex-col gap-3">
				<h2 id="existing-webhooks" className="text-lg font-medium">
					{t("webhooks.existing")}
				</h2>
				{hooks.isPending && <Loading />}
				<FormError error={hooks.error} />
				{hooks.data?.webhooks.length === 0 && (
					<p className="text-slate-600 dark:text-slate-400">{t("webhooks.none")}</p>
				)}
				<ul className="flex flex-col gap-3">
					{hooks.data?.webhooks.map((hook) => (
						<HookRow key={hook.id} hook={hook} kinds={kinds} />
					))}
				</ul>
			</section>
			<section aria-labelledby="add-webhook" className="flex max-w-xl flex-col gap-4">
				<h2 id="add-webhook" className="text-lg font-medium">
					{t("webhooks.add")}
				</h2>
				<HookForm kinds={kinds} />
			</section>
		</div>
	);
}

function HookRow({ hook, kinds }: { hook: Webhook; kinds: string[] }) {
	const test = useTestWebhook();
	const remove = useDeleteWebhook();
	const [editing, setEditing] = useState(false);
	const [showing, setShowing] = useState(false);
	const [confirming, setConfirming] = useState(false);
	return (
		<li aria-label={hook.name} className={`${card} flex min-w-0 flex-col gap-3 p-4`}>
			<div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
				<span className="font-medium break-words">{hook.name}</span>
				{!hook.enabled && (
					<span className="rounded bg-slate-200 px-2 text-sm dark:bg-slate-800">
						{t("webhooks.off")}
					</span>
				)}
				{hook.trusted && (
					<span className="rounded bg-slate-200 px-2 text-sm dark:bg-slate-800">
						{t("webhooks.trusted")}
					</span>
				)}
				<span className="text-sm text-slate-600 dark:text-slate-400">
					{hook.last
						? t(lastKey(hook.last.status), {
								when: formatDateTime(hook.last.at),
							})
						: t("webhooks.neverSent")}
				</span>
			</div>
			<p className="min-w-0 text-sm break-all text-slate-600 dark:text-slate-400">
				{hook.url}
			</p>
			<p className="min-w-0 text-sm break-words">
				{hook.kinds.length === 0 ? t("webhooks.allKinds") : hook.kinds.join(", ")}
			</p>
			<div className="flex flex-wrap items-center gap-2">
				<button
					type="button"
					disabled={test.isPending}
					onClick={() => test.mutate(hook.id)}
					className={button()}
				>
					{t("webhooks.test")}
				</button>
				<button
					type="button"
					aria-expanded={showing}
					onClick={() => setShowing(!showing)}
					className={button()}
				>
					{t("webhooks.deliveries")}
				</button>
				<button
					type="button"
					aria-expanded={editing}
					onClick={() => setEditing(!editing)}
					className={button()}
				>
					{t("webhooks.edit")}
				</button>
				<div className="ml-auto flex items-center gap-2">
					{confirming ? (
						<>
							<span>{t("webhooks.delete.confirm")}</span>
							<button
								type="button"
								disabled={remove.isPending}
								onClick={() => remove.mutate(hook.id)}
								className={button("danger")}
							>
								{t("webhooks.delete.yes")}
							</button>
							<button type="button" onClick={() => setConfirming(false)} className={button()}>
								{t("webhooks.delete.no")}
							</button>
						</>
					) : (
						<button type="button" onClick={() => setConfirming(true)} className={button("danger")}>
							{t("webhooks.delete")}
						</button>
					)}
				</div>
			</div>
			{test.isSuccess && (
				<p role="status" className="text-sm text-slate-600 dark:text-slate-400">
					{t("webhooks.test.queued")}
				</p>
			)}
			<FormError error={test.error ?? remove.error} />
			{editing && <HookForm hook={hook} kinds={kinds} onDone={() => setEditing(false)} />}
			{showing && <Deliveries id={hook.id} />}
		</li>
	);
}

function Deliveries({ id }: { id: string }) {
	const deliveries = useQuery(deliveriesQuery(id));
	return (
		<div className="flex min-w-0 flex-col gap-2">
			<h3 className="text-sm font-medium">{t("webhooks.deliveries")}</h3>
			{deliveries.isPending && <Loading rows={1} />}
			{deliveries.data?.length === 0 && (
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{t("webhooks.deliveries.none")}
				</p>
			)}
			<ul className="flex flex-col gap-2">
				{deliveries.data?.map((d) => (
					<li
						key={d.id}
						className="min-w-0 rounded-lg border border-slate-200 p-2 text-sm dark:border-slate-700"
					>
						<div className="flex flex-wrap gap-x-3">
							<span className="font-medium">{d.kind}</span>
							<span>{t(statusKey(d.status))}</span>
							<span>{t("webhooks.attempts", { count: d.attempts })}</span>
							{d.responseCode && <span>{d.responseCode}</span>}
							<span className="text-slate-600 dark:text-slate-400">
								{formatDateTime(d.createdAt)}
							</span>
						</div>
						{d.lastError && (
							<p className="break-words text-red-700 dark:text-red-300">{d.lastError}</p>
						)}
						<pre className="mt-1 max-w-full overflow-x-auto text-xs">
							{JSON.stringify(d.payload)}
						</pre>
					</li>
				))}
			</ul>
		</div>
	);
}

function HookForm({
	hook,
	kinds,
	onDone,
}: {
	hook?: Webhook;
	kinds: string[];
	onDone?: () => void;
}) {
	const create = useCreateWebhook();
	const update = useUpdateWebhook();
	const mutation = hook ? update : create;
	const [name, setName] = useState(hook?.name ?? "");
	const [url, setUrl] = useState(hook?.url ?? "");
	const [secret, setSecret] = useState("");
	const [picked, setPicked] = useState<string[]>(hook?.kinds ?? []);
	const [trusted, setTrusted] = useState(hook?.trusted ?? false);
	const [enabled, setEnabled] = useState(hook?.enabled ?? true);
	const errors = fieldErrors(mutation.error);

	function submit(event: FormEvent) {
		event.preventDefault();
		const body: WebhookRequest = {
			name,
			url,
			kinds: picked,
			trusted,
			enabled,
			secret: secret === "" ? undefined : secret,
		};
		const done = () => {
			setSecret("");
			if (!hook) {
				setName("");
				setUrl("");
				setPicked([]);
				setTrusted(false);
			}
			onDone?.();
		};
		if (hook) {
			update.mutate({ id: hook.id, body }, { onSuccess: done });
		} else {
			create.mutate(body, { onSuccess: done });
		}
	}

	return (
		<form onSubmit={submit} className="flex min-w-0 flex-col gap-4" noValidate>
			<Field
				label={t("webhooks.name")}
				value={name}
				onChange={(e) => setName(e.target.value)}
				error={errors.name}
			/>
			<Field
				label={t("webhooks.url")}
				type="url"
				value={url}
				onChange={(e) => setUrl(e.target.value)}
				error={errors.url}
			/>
			<Field
				label={t("webhooks.secret")}
				type="password"
				autoComplete="new-password"
				hint={hook ? t("webhooks.secret.keep") : t("webhooks.secret.hint")}
				value={secret}
				onChange={(e) => setSecret(e.target.value)}
				error={errors.secret}
			/>
			<fieldset className="flex min-w-0 flex-col gap-1">
				<legend className="mb-1 text-sm font-medium">{t("webhooks.kinds")}</legend>
				<p className="text-sm text-slate-500 dark:text-slate-400">{t("webhooks.kinds.hint")}</p>
				<div className="grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-2">
					{kinds.map((kind) => (
						<label key={kind} className="flex min-w-0 items-center gap-2">
							<input
								type="checkbox"
								checked={picked.includes(kind)}
								onChange={(e) =>
									setPicked(
										e.target.checked ? [...picked, kind] : picked.filter((k) => k !== kind),
									)
								}
							/>
							<span className="break-all">{kind}</span>
						</label>
					))}
				</div>
				{errors.kinds && <p className="text-sm text-red-700 dark:text-red-300">{errors.kinds}</p>}
			</fieldset>
			<label className="flex items-center gap-2">
				<input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
				<span>{t("webhooks.enabled")}</span>
			</label>
			<label className="flex items-start gap-2">
				<input
					type="checkbox"
					className="mt-1"
					checked={trusted}
					onChange={(e) => setTrusted(e.target.checked)}
				/>
				<span>
					{t("webhooks.trustedLabel")}
					<span className="block text-sm text-slate-500 dark:text-slate-400">
						{t("webhooks.trusted.hint")}
					</span>
				</span>
			</label>
			{Object.keys(errors).length === 0 && <FormError error={mutation.error} />}
			<div>
				<SubmitButton pending={mutation.isPending}>
					{hook ? t("webhooks.save") : t("webhooks.add.submit")}
				</SubmitButton>
			</div>
		</form>
	);
}
