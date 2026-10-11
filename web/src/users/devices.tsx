import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { t } from "@/i18n";
import { formatDate } from "@/lib/format";
import { ownDevicesQuery, useCreateDevice, useDeleteDevice } from "@/users/api";

/**
 * E-readers such as KOReader browse the library as an OPDS catalogue,
 * signed in with the person's user name and a token made here, which is
 * shown once.
 */
export function Devices({ username }: { username: string }) {
	const devices = useQuery(ownDevicesQuery);
	const create = useCreateDevice();
	const remove = useDeleteDevice();
	const [name, setName] = useState("");
	const created = create.data;
	const errors = fieldErrors(create.error);

	function submit(e: FormEvent) {
		e.preventDefault();
		create.mutate(name, { onSuccess: () => setName("") });
	}

	return (
		<section aria-labelledby="devices" className="flex flex-col gap-3">
			<h2 id="devices" className="text-lg font-medium">
				{t("devices.title")}
			</h2>
			<p className="text-slate-600 dark:text-slate-400">{t("devices.intro")}</p>
			<p className="text-slate-600 dark:text-slate-400">
				{t("devices.plugin")}{" "}
				<a
					href="/api/v1/koreader/plugin.zip"
					download
					className="text-brand-strong hover:underline dark:text-sky-400"
				>
					{t("devices.pluginDownload")}
				</a>
			</p>
			<FormError error={devices.error ?? remove.error} />
			{devices.data && devices.data.length > 0 && (
				<ul className="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
					{devices.data.map((d) => (
						<li
							key={d.id}
							className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3"
						>
							<span className="font-medium">{d.name}</span>
							<span className="text-sm text-slate-600 dark:text-slate-400">
								{d.lastUsedAt
									? t("devices.lastUsed", { date: formatDate(d.lastUsedAt) })
									: t("devices.neverUsed")}
							</span>
							<button
								type="button"
								disabled={remove.isPending}
								onClick={() => remove.mutate(d.id)}
								aria-label={t("devices.revokeNamed", { name: d.name })}
								className="ml-auto rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("devices.revoke")}
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
						{t("devices.created", { name: created.device.name })}
					</p>
					<dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
						<dt>{t("devices.catalog")}</dt>
						<dd className="break-all font-mono">
							{window.location.origin + created.catalog}
						</dd>
						<dt>{t("devices.username")}</dt>
						<dd className="break-all font-mono">{username}</dd>
						<dt>{t("devices.token")}</dt>
						<dd className="break-all font-mono">{created.token}</dd>
					</dl>
					<p className="text-sm text-slate-600 dark:text-slate-400">
						{t("devices.once")}
					</p>
				</div>
			)}
			<form onSubmit={submit} className="flex flex-wrap items-end gap-3">
				<Field
					label={t("devices.name")}
					value={name}
					onChange={(e) => setName(e.target.value)}
					error={errors.name}
					required
				/>
				<SubmitButton pending={create.isPending}>
					{t("devices.add")}
				</SubmitButton>
			</form>
			<FormError error={errors.name ? null : create.error} />
		</section>
	);
}
