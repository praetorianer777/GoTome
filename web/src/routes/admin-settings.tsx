import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import {
	ChoicesField,
	Field,
	FormError,
	fieldErrors,
	SelectField,
	SubmitButton,
	Switch,
} from "@/components/form";
import { button, Loading, PageHeader } from "@/components/ui";
import { type MessageKey, t } from "@/i18n";
import { en } from "@/i18n/en";
import { formatDate } from "@/lib/format";
import { API_BASE } from "@/api/client";
import { type Setting, settingsQuery, useUpdateSettings } from "@/settings/api";
import { MatchScorePanel } from "@/settings/match-score";
import {
	EmbeddingModelField,
	embeddingModelsQuery,
	lacksMostVectors,
	SwitchModelDialog,
} from "@/settings/embedding-model";

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
	"devices.fixOnSend": {
		label: "settings.devicesFixOnSend",
		hint: "settings.devicesFixOnSend.hint",
	},
	"updates.check": {
		label: "settings.updatesCheck",
		hint: "settings.updatesCheck.hint",
	},
	"metadata.autoMatch": {
		label: "settings.autoMatch",
		hint: "settings.autoMatch.hint",
	},
	"metadata.matchThreshold": {
		label: "settings.matchThreshold",
		hint: "settings.matchThreshold.hint",
	},
	"metadata.scoreWeights": {
		label: "settings.scoreWeights",
		hint: "settings.scoreWeights.hint",
	},
	"metadata.learnScore": {
		label: "settings.learnScore",
		hint: "settings.learnScore.hint",
	},
	"curation.cleanup.kinds": {
		label: "settings.curationCleanupKinds",
		hint: "settings.curationCleanupKinds.hint",
	},
	"curation.matches.auto": {
		label: "settings.curationMatchesAuto",
		hint: "settings.curationMatchesAuto.hint",
	},
	"curation.matches.edition": {
		label: "settings.curationMatchesEdition",
		hint: "settings.curationMatchesEdition.hint",
	},
	"metadata.providers": {
		label: "settings.providers",
		hint: "settings.providers.hint",
	},
	"metadata.contactEmail": {
		label: "settings.contactEmail",
		hint: "settings.contactEmail.hint",
	},
	"trash.retentionDays": {
		label: "settings.trashRetention",
		hint: "settings.trashRetention.hint",
	},
	"embedding.enabled": {
		label: "settings.embeddingEnabled",
		hint: "settings.embeddingEnabled.hint",
	},
	"embedding.model": {
		label: "settings.embeddingModel",
		hint: "settings.embeddingModel.hint",
	},
	"embedding.removeUnusedAfterDays": {
		label: "settings.embeddingRemoveUnused",
		hint: "settings.embeddingRemoveUnused.hint",
	},
	"embedding.server": {
		label: "settings.embeddingServer",
		hint: "settings.embeddingServer.hint",
	},
	"curation.duplicates.classes": {
		label: "settings.duplicateClasses",
		hint: "settings.duplicateClasses.hint",
	},
	"curation.duplicates.onConflict": {
		label: "settings.duplicateConflict",
		hint: "settings.duplicateConflict.hint",
	},
	"genres.suggest": {
		label: "settings.genreSuggestions",
		hint: "settings.genreSuggestions.hint",
	},
	"embedding.passages": {
		label: "settings.embeddingPassages",
		hint: "settings.embeddingPassages.hint",
	},
	"auth.passwords": {
		label: "settings.passwords",
		hint: "settings.passwords.hint",
	},
	"oidc.issuer": {
		label: "settings.oidcIssuer",
		hint: "settings.oidcIssuer.hint",
	},
	"oidc.clientId": {
		label: "settings.oidcClientId",
		hint: "settings.oidcClientId.hint",
	},
	"oidc.clientSecret": {
		label: "settings.oidcClientSecret",
		hint: "settings.oidcClientSecret.hint",
	},
	"oidc.name": {
		label: "settings.oidcName",
		hint: "settings.oidcName.hint",
	},
	"oidc.scopes": {
		label: "settings.oidcScopes",
		hint: "settings.oidcScopes.hint",
	},
	"oidc.groupsClaim": {
		label: "settings.oidcGroupsClaim",
		hint: "settings.oidcGroupsClaim.hint",
	},
	"oidc.adminGroups": {
		label: "settings.oidcAdminGroups",
		hint: "settings.oidcAdminGroups.hint",
	},
	"oidc.editorGroups": {
		label: "settings.oidcEditorGroups",
		hint: "settings.oidcEditorGroups.hint",
	},
	"oidc.readerGroups": {
		label: "settings.oidcReaderGroups",
		hint: "settings.oidcReaderGroups.hint",
	},
	"oidc.defaultRole": {
		label: "settings.oidcDefaultRole",
		hint: "settings.oidcDefaultRole.hint",
	},
	"oidc.signup": {
		label: "settings.oidcSignup",
		hint: "settings.oidcSignup.hint",
	},
	"oidc.linkByEmail": {
		label: "settings.oidcLinkByEmail",
		hint: "settings.oidcLinkByEmail.hint",
	},
	"mail.host": { label: "settings.mailHost", hint: "settings.mailHost.hint" },
	"mail.port": { label: "settings.mailPort", hint: "settings.mailPort.hint" },
	"mail.security": {
		label: "settings.mailSecurity",
		hint: "settings.mailSecurity.hint",
	},
	"mail.user": { label: "settings.mailUser", hint: "settings.mailUser.hint" },
	"mail.password": {
		label: "settings.mailPassword",
		hint: "settings.mailPassword.hint",
	},
	"mail.sender": {
		label: "settings.mailSender",
		hint: "settings.mailSender.hint",
	},
};

/** Values hints may name: the address the identity provider sends people back to. */
function hintValues() {
	return { callback: `${window.location.origin}${API_BASE}/auth/oidc/callback` };
}

/** The page's sections, each with the settings whose keys start so. */
const SECTIONS: { id: string; title: MessageKey; prefixes: string[] }[] = [
	{ id: "sign-in", title: "settings.section.signIn", prefixes: ["auth.", "oidc."] },
	{ id: "metadata", title: "settings.section.metadata", prefixes: ["metadata."] },
	{ id: "embedding", title: "settings.section.embedding", prefixes: ["embedding.", "search."] },
	{ id: "genres", title: "settings.section.genres", prefixes: ["genres."] },
	{ id: "trash", title: "settings.section.trash", prefixes: ["trash."] },
	{ id: "mail", title: "settings.section.mail", prefixes: ["mail."] },
];
const OTHER = { id: "other", title: "settings.section.other" as MessageKey };

function sectionOf(key: string) {
	return (
		SECTIONS.find((s) => s.prefixes.some((p) => key.startsWith(p))) ?? OTHER
	);
}

function isOnOff(s: Setting) {
	return s.choices?.length === 2 && s.choices.includes("on") && s.choices.includes("off");
}

/** A choice as people read it, or as the server names it where no text says otherwise. */
function choiceLabel(key: string, value: string): string {
	const text = `settingChoice.${key}.${value}`;
	return text in en ? t(text as MessageKey) : value;
}

export function AdminSettings() {
	const settings = useQuery(settingsQuery);
	return (
		<div className="flex max-w-6xl flex-col gap-8">
			<PageHeader title={t("settings.title")} intro={t("settings.intro")} />
			{settings.isPending && <Loading />}
			<FormError error={settings.error} />
			{settings.data && <SettingsForm settings={settings.data} />}
		</div>
	);
}

function SettingsForm({ settings }: { settings: Setting[] }) {
	const update = useUpdateSettings();
	const remove = useUpdateSettings();
	const models = useQuery(embeddingModelsQuery);
	// Only what was typed: a secret's field starts empty, a text's with its value.
	const [typed, setTyped] = useState<Record<string, string>>({});
	const [saved, setSaved] = useState(false);
	// Values held back while the switch of model is confirmed.
	const [asking, setAsking] = useState<Record<string, string | null> | null>(null);
	const errors = { ...fieldErrors(remove.error), ...fieldErrors(update.error) };
	const sections = [...SECTIONS, OTHER]
		.map((section) => ({
			...section,
			settings: settings.filter((s) => sectionOf(s.key).id === section.id),
		}))
		.filter((section) => section.settings.length > 0);

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
		const model = values["embedding.model"];
		if (model && models.data && lacksMostVectors(models.data, model)) {
			setAsking(values);
			return;
		}
		save(values);
	}

	function save(values: Record<string, string | null>) {
		setAsking(null);
		setSaved(false);
		update.mutate(values, {
			onSuccess: () => {
				setTyped({});
				setSaved(true);
			},
		});
	}

	function field(s: Setting) {
		const texts = TEXTS[s.key];
		const label = texts ? t(texts.label) : s.key;
		const hint = texts && t(texts.hint, hintValues());
		const value = typed[s.key] ?? s.value ?? "";
		const set = (v: string) => setTyped({ ...typed, [s.key]: v });
		if (s.key === "embedding.model" && models.isPending) {
			return (
				<div key={s.key} className="md:col-span-2">
					<Loading rows={1} />
				</div>
			);
		}
		if (s.key === "embedding.model" && models.data) {
			return (
				<div key={s.key} className="md:col-span-2">
					<EmbeddingModelField
						label={label}
						hint={hint}
						value={value}
						list={models.data}
						onChange={set}
						error={errors[s.key]}
					/>
				</div>
			);
		}
		if (s.kind === "text" && isOnOff(s)) {
			return (
				<Switch
					key={s.key}
					label={label}
					hint={hint}
					checked={value === "on"}
					onChange={(on) => set(on ? "on" : "off")}
					error={errors[s.key]}
				/>
			);
		}
		if (s.kind === "text" && s.choices?.length) {
			const choices = s.choices.map((c) => ({
				value: c,
				label: choiceLabel(s.key, c),
			}));
			if (s.multiple) {
				return (
					<ChoicesField
						key={s.key}
						label={label}
						hint={hint}
						value={value.split(",").filter(Boolean)}
						choices={choices}
						onChange={(picked) => set(picked.join(","))}
						error={errors[s.key]}
					/>
				);
			}
			return (
				<SelectField
					key={s.key}
					label={label}
					hint={hint}
					value={value}
					choices={choices}
					onChange={set}
					error={errors[s.key]}
				/>
			);
		}
		if (s.kind === "text") {
			return (
				<Field
					key={s.key}
					label={label}
					hint={hint}
					value={value}
					onChange={(e) => set(e.target.value)}
					error={errors[s.key]}
				/>
			);
		}
		return (
			<div key={s.key} className="flex flex-col gap-2">
				<Field
					label={label}
					hint={[
						hint,
						s.isSet && s.updatedAt
							? t("settings.secret.set", { date: formatDate(s.updatedAt) })
							: t("settings.secret.unset"),
					]
						.filter(Boolean)
						.join(" ")}
					type="password"
					autoComplete="off"
					placeholder={
						s.isSet ? t("settings.secret.replace") : t("settings.secret.new")
					}
					value={typed[s.key] ?? ""}
					onChange={(e) => set(e.target.value)}
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
						className={`${button("secondary")} self-start text-sm`}
					>
						{t("settings.secret.remove", { name: label })}
					</button>
				)}
			</div>
		);
	}

	return (
		<div className="flex flex-col gap-6 lg:grid lg:grid-cols-[12rem_minmax(0,1fr)] lg:items-start lg:gap-8">
			<nav aria-label={t("settings.sections")} className="lg:sticky lg:top-24">
				<ul className="flex flex-wrap gap-x-4 gap-y-2 text-sm lg:flex-col lg:gap-2">
					{sections.map((section) => (
						<li key={section.id}>
							<a
								href={`#settings-${section.id}`}
								className="inline-flex min-h-6 items-center text-brand-strong underline-offset-2 hover:underline dark:text-brand"
							>
								{t(section.title)}
							</a>
						</li>
					))}
				</ul>
			</nav>
			<form onSubmit={submit} className="flex min-w-0 flex-col gap-8" noValidate>
				{sections.map((section) => (
					<section
						key={section.id}
						id={`settings-${section.id}`}
						aria-labelledby={`settings-${section.id}-title`}
						className="flex scroll-mt-20 flex-col gap-3"
					>
						<h2 id={`settings-${section.id}-title`} className="text-lg font-medium">
							{t(section.title)}
						</h2>
						<div className="grid grid-cols-1 gap-x-8 gap-y-5 md:grid-cols-2">
							{section.settings.map(field)}
						</div>
						{section.id === "metadata" && <MatchScorePanel />}
					</section>
				))}
				{Object.keys(errors).length === 0 && (
					<FormError error={update.error ?? remove.error} />
				)}
				<div className="sticky bottom-0 flex items-center gap-4 border-t border-slate-200 bg-white pt-3 pb-safe-3 dark:border-slate-700 dark:bg-slate-900">
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
				{asking && models.data && (
					<SwitchModelDialog
						model={asking["embedding.model"] ?? ""}
						list={models.data}
						onConfirm={() => save(asking)}
						onCancel={() => setAsking(null)}
					/>
				)}
			</form>
		</div>
	);
}
