import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import type { CurrentUser } from "@/auth/session";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { formatDate } from "@/lib/format";
import {
	EVENT_LABELS,
	notificationSettingsQuery,
	useSetNotificationSetting,
} from "@/notifications/api";
import {
	ownSessionsQuery,
	useChangePassword,
	useEndOwnSession,
} from "@/users/api";
import { StorageUse } from "@/users/storage";

const ROLE_LABELS: Record<string, MessageKey> = {
	admin: "role.admin",
	editor: "role.editor",
	reader: "role.reader",
};

/**
 * A browser as people name it, from what it says about itself: "Firefox on
 * Linux". What is not recognised is shown as it came, shortened.
 */
export function describeAgent(agent: string): string {
	const browser = [
		["Edg/", "Edge"],
		["Firefox/", "Firefox"],
		["Chrome/", "Chrome"],
		["Safari/", "Safari"],
	].find(([mark]) => agent.includes(mark as string))?.[1];
	const system = [
		["iPhone", "iPhone"],
		["iPad", "iPad"],
		["Android", "Android"],
		["Windows", "Windows"],
		["Mac OS", "macOS"],
		["Linux", "Linux"],
	].find(([mark]) => agent.includes(mark as string))?.[1];
	if (browser && system) {
		return t("profile.agent", { browser, system });
	}
	if (!agent) {
		return t("profile.agentUnknown");
	}
	return agent.length > 60 ? `${agent.slice(0, 57)}…` : agent;
}

export function Profile({ user }: { user: CurrentUser }) {
	return (
		<div className="flex max-w-2xl flex-col gap-8">
			<div>
				<h1 className="text-2xl font-semibold">{t("profile.title")}</h1>
				<p className="mt-1 text-slate-600 dark:text-slate-400">
					{t("profile.who", {
						name: user.username,
						role: ROLE_LABELS[user.role]
							? t(ROLE_LABELS[user.role] as MessageKey)
							: user.role,
					})}
				</p>
			</div>
			<section aria-labelledby="storage" className="flex flex-col gap-2">
				<h2 id="storage" className="text-lg font-medium">
					{t("profile.storage")}
				</h2>
				<StorageUse />
			</section>
			<NotificationSettings />
			<ChangePassword />
			<Sessions />
		</div>
	);
}

function NotificationSettings() {
	const settings = useQuery(notificationSettingsQuery);
	const set = useSetNotificationSetting();
	if (!settings.data?.length) return null;
	return (
		<section aria-labelledby="notification-settings" className="flex flex-col gap-2">
			<h2 id="notification-settings" className="text-lg font-medium">
				{t("notifications.settings")}
			</h2>
			<p className="text-sm text-slate-600 dark:text-slate-400">
				{t("notifications.settings.hint")}
			</p>
			<ul className="flex flex-col gap-1">
				{settings.data.map((s) => (
					<li key={s.kind}>
						<label className="flex items-center gap-2">
							<input
								type="checkbox"
								checked={s.app}
								disabled={set.isPending}
								onChange={(e) => set.mutate({ kind: s.kind, app: e.target.checked })}
							/>
							{t(EVENT_LABELS[s.kind])}
						</label>
					</li>
				))}
			</ul>
		</section>
	);
}

function ChangePassword() {
	const change = useChangePassword();
	const [current, setCurrent] = useState("");
	const [next, setNext] = useState("");
	const [done, setDone] = useState(false);
	const errors = fieldErrors(change.error);

	function submit(event: FormEvent) {
		event.preventDefault();
		setDone(false);
		change.mutate(
			{ currentPassword: current, newPassword: next },
			{
				onSuccess: () => {
					setCurrent("");
					setNext("");
					setDone(true);
				},
			},
		);
	}

	return (
		<section
			aria-labelledby="change-password"
			className="flex max-w-md flex-col gap-4"
		>
			<h2 id="change-password" className="text-lg font-medium">
				{t("profile.password")}
			</h2>
			<form onSubmit={submit} className="flex flex-col gap-4" noValidate>
				<Field
					label={t("profile.password.current")}
					type="password"
					autoComplete="current-password"
					value={current}
					onChange={(e) => setCurrent(e.target.value)}
					error={errors.currentPassword}
				/>
				<Field
					label={t("profile.password.new")}
					type="password"
					autoComplete="new-password"
					hint={t("profile.password.hint")}
					value={next}
					onChange={(e) => setNext(e.target.value)}
					error={errors.newPassword}
				/>
				{Object.keys(errors).length === 0 && <FormError error={change.error} />}
				<div className="flex items-center gap-4">
					<SubmitButton pending={change.isPending}>
						{t("profile.password.submit")}
					</SubmitButton>
					{done && (
						<p
							role="status"
							className="text-sm text-green-800 dark:text-green-300"
						>
							{t("profile.password.done")}
						</p>
					)}
				</div>
			</form>
		</section>
	);
}

function Sessions() {
	const sessions = useQuery(ownSessionsQuery);
	const end = useEndOwnSession();
	return (
		<section aria-labelledby="sessions" className="flex flex-col gap-3">
			<h2 id="sessions" className="text-lg font-medium">
				{t("profile.sessions")}
			</h2>
			<p className="text-slate-600 dark:text-slate-400">
				{t("profile.sessions.intro")}
			</p>
			<FormError error={sessions.error ?? end.error} />
			<ul className="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
				{sessions.data?.map((s) => (
					<li
						key={s.id}
						className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3"
					>
						<span className="font-medium">{describeAgent(s.userAgent)}</span>
						<span className="text-sm text-slate-600 dark:text-slate-400">
							{s.current
								? t("profile.sessions.current")
								: t("profile.sessions.lastSeen", {
										date: formatDate(s.lastSeenAt),
									})}
						</span>
						{!s.current && (
							<button
								type="button"
								disabled={end.isPending}
								onClick={() => end.mutate(s.id)}
								aria-label={t("profile.sessions.endNamed", {
									name: describeAgent(s.userAgent),
								})}
								className="ml-auto rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("profile.sessions.end")}
							</button>
						)}
					</li>
				))}
			</ul>
		</section>
	);
}
