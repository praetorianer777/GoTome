import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";
import {
	ownApiKeysQuery,
	useCreateApiKey,
	useRevokeApiKey,
} from "@/users/api";

const SCOPES = ["read", "progress", "upload", "edit", "admin"] as const;

function scopeName(scope: string): string {
	return (SCOPES as readonly string[]).includes(scope)
		? t(`apiKeys.scope.${scope as (typeof SCOPES)[number]}`)
		: scope;
}

/**
 * Keys for scripts and other programs, sent as a bearer token. A key is
 * limited to the scopes chosen here and never reaches more than its
 * owner's role; it is shown once.
 */
export function ApiKeys() {
	const keys = useQuery(ownApiKeysQuery);
	const create = useCreateApiKey();
	const revoke = useRevokeApiKey();
	const [name, setName] = useState("");
	const [scopes, setScopes] = useState<string[]>(["read"]);
	const [expires, setExpires] = useState("");
	const created = create.data;
	const errors = fieldErrors(create.error);

	function submit(e: FormEvent) {
		e.preventDefault();
		create.mutate(
			{
				name,
				scopes,
				// The end of the chosen day, in the browser's time zone.
				expiresAt: expires
					? new Date(`${expires}T23:59:59`).toISOString()
					: undefined,
			},
			{ onSuccess: () => setName("") },
		);
	}

	return (
		<section aria-labelledby="api-keys" className="flex flex-col gap-3">
			<h2 id="api-keys" className="text-lg font-medium">
				{t("apiKeys.title")}
			</h2>
			<p className="text-slate-600 dark:text-slate-400">{t("apiKeys.intro")}</p>
			<FormError error={keys.error ?? revoke.error} />
			{keys.data && keys.data.length > 0 && (
				<ul className="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
					{keys.data.map((k) => (
						<li
							key={k.id}
							className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3"
						>
							<span className="font-medium">{k.name}</span>
							<span className="font-mono text-sm text-slate-600 dark:text-slate-400">
								{k.prefix}…
							</span>
							<span className="text-sm text-slate-600 dark:text-slate-400">
								{k.scopes.map((s) => scopeName(s)).join(", ")}
							</span>
							<span className="text-sm text-slate-600 dark:text-slate-400">
								{t("apiKeys.created", { date: formatDate(k.createdAt) })}
							</span>
							<span className="text-sm text-slate-600 dark:text-slate-400">
								{k.lastUsedAt
									? t("apiKeys.lastUsed", { date: formatDate(k.lastUsedAt) })
									: t("apiKeys.neverUsed")}
							</span>
							{k.expiresAt && (
								<span className="text-sm text-slate-600 dark:text-slate-400">
									{new Date(k.expiresAt) < new Date()
										? t("apiKeys.expired", { date: formatDate(k.expiresAt) })
										: t("apiKeys.expires", { date: formatDate(k.expiresAt) })}
								</span>
							)}
							<button
								type="button"
								disabled={revoke.isPending}
								onClick={() => revoke.mutate(k.id)}
								aria-label={t("apiKeys.revokeNamed", { name: k.name })}
								className="ml-auto rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("apiKeys.revoke")}
							</button>
						</li>
					))}
				</ul>
			)}
			{created && (
				<div
					role="status"
					className="flex flex-col gap-2 rounded-lg border border-emerald-300 bg-emerald-50 p-4 dark:border-emerald-800 dark:bg-emerald-950"
				>
					<p className="font-medium">
						{t("apiKeys.createdNamed", { name: created.key.name })}
					</p>
					<p className="break-all font-mono text-sm">{created.token}</p>
					<p className="text-sm text-slate-600 dark:text-slate-400">
						{t("apiKeys.once")}
					</p>
				</div>
			)}
			<form onSubmit={submit} className="flex flex-col gap-3">
				<div className="flex flex-wrap items-end gap-3">
					<Field
						label={t("apiKeys.name")}
						value={name}
						onChange={(e) => setName(e.target.value)}
						error={errors.name}
						required
					/>
					<Field
						type="date"
						label={t("apiKeys.expiry")}
						value={expires}
						onChange={(e) => setExpires(e.target.value)}
						error={errors.expiresAt}
						optional
					/>
				</div>
				<fieldset className="flex flex-col gap-1">
					<legend className="text-sm font-medium">{t("apiKeys.scopes")}</legend>
					{SCOPES.map((scope) => (
						<label key={scope} className="flex items-baseline gap-2">
							<input
								type="checkbox"
								checked={scopes.includes(scope)}
								onChange={(e) =>
									setScopes(
										e.target.checked
											? [...scopes, scope]
											: scopes.filter((s) => s !== scope),
									)
								}
							/>
							<span>{t(`apiKeys.scope.${scope}`)}</span>
						</label>
					))}
					{errors.scopes && (
						<p className="text-sm text-red-700 dark:text-red-300">
							{errors.scopes}
						</p>
					)}
				</fieldset>
				<div>
					<SubmitButton pending={create.isPending}>
						{t("apiKeys.add")}
					</SubmitButton>
				</div>
			</form>
			<FormError error={errors.name || errors.scopes ? null : create.error} />
		</section>
	);
}
