import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { formatDate } from "@/lib/format";
import { type Setting, settingsQuery, useUpdateSettings } from "@/settings/api";

/** What a setting is called and what it is for, by its key. */
const TEXTS: Record<string, { label: MessageKey; hint: MessageKey }> = {
	"metadata.language": {
		label: "settings.metadataLanguage",
		hint: "settings.metadataLanguage.hint",
	},
	"metadata.googleBooksKey": {
		label: "settings.googleBooksKey",
		hint: "settings.googleBooksKey.hint",
	},
	"metadata.hardcoverToken": {
		label: "settings.hardcoverToken",
		hint: "settings.hardcoverToken.hint",
	},
};

export function AdminSettings() {
	const settings = useQuery(settingsQuery);
	return (
		<div className="flex max-w-2xl flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("settings.title")}</h1>
			<p className="text-slate-600 dark:text-slate-400">
				{t("settings.intro")}
			</p>
			{settings.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={settings.error} />
			{settings.data && <SettingsForm settings={settings.data} />}
		</div>
	);
}

function SettingsForm({ settings }: { settings: Setting[] }) {
	const update = useUpdateSettings();
	const remove = useUpdateSettings();
	// Only what was typed: a secret's field starts empty, a text's with its value.
	const [typed, setTyped] = useState<Record<string, string>>({});
	const [saved, setSaved] = useState(false);
	const errors = { ...fieldErrors(remove.error), ...fieldErrors(update.error) };

	function submit(event: FormEvent) {
		event.preventDefault();
		const values: Record<string, string | null> = {};
		for (const s of settings) {
			const v = typed[s.key];
			if (v === undefined) {
				continue;
			}
			// An emptied secret field means nothing was typed, not "remove";
			// that is what the Remove button is for.
			if (s.kind === "secret" && v.trim() === "") {
				continue;
			}
			if (s.kind === "text" && v === (s.value ?? "")) {
				continue;
			}
			values[s.key] = v;
		}
		setSaved(false);
		update.mutate(values, {
			onSuccess: () => {
				setTyped({});
				setSaved(true);
			},
		});
	}

	return (
		<form onSubmit={submit} className="flex flex-col gap-5" noValidate>
			{settings.map((s) => {
				const texts = TEXTS[s.key];
				const label = texts ? t(texts.label) : s.key;
				if (s.kind === "text") {
					return (
						<Field
							key={s.key}
							label={label}
							hint={texts && t(texts.hint)}
							value={typed[s.key] ?? s.value ?? ""}
							onChange={(e) => setTyped({ ...typed, [s.key]: e.target.value })}
							error={errors[s.key]}
						/>
					);
				}
				return (
					<div key={s.key} className="flex flex-col gap-2">
						<Field
							label={label}
							hint={[
								texts && t(texts.hint),
								s.isSet && s.updatedAt
									? t("settings.secret.set", { date: formatDate(s.updatedAt) })
									: t("settings.secret.unset"),
							]
								.filter(Boolean)
								.join(" ")}
							type="password"
							autoComplete="off"
							placeholder={
								s.isSet
									? t("settings.secret.replace")
									: t("settings.secret.new")
							}
							value={typed[s.key] ?? ""}
							onChange={(e) => setTyped({ ...typed, [s.key]: e.target.value })}
							error={errors[s.key]}
						/>
						{s.isSet && (
							<button
								type="button"
								disabled={remove.isPending}
								onClick={() => {
									setSaved(false);
									remove.mutate({ [s.key]: null });
								}}
								className="self-start rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("settings.secret.remove", { name: label })}
							</button>
						)}
					</div>
				);
			})}
			{Object.keys(errors).length === 0 && (
				<FormError error={update.error ?? remove.error} />
			)}
			<div className="flex items-center gap-4">
				<SubmitButton pending={update.isPending}>
					{update.isPending ? t("settings.saving") : t("settings.save")}
				</SubmitButton>
				{saved && (
					<p
						role="status"
						className="text-sm text-green-800 dark:text-green-300"
					>
						{t("settings.saved")}
					</p>
				)}
			</div>
		</form>
	);
}
