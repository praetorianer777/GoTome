import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import type { CurrentUser } from "@/auth/session";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { type MessageKey, t } from "@/i18n";
import { formatDate } from "@/lib/format";
import {
	type Account,
	useCreateUser,
	useEndUserSessions,
	useUpdateUser,
	usersQuery,
} from "@/users/api";

type Role = Account["role"];

const ROLES: { value: Role; label: MessageKey }[] = [
	{ value: "reader", label: "role.reader" },
	{ value: "editor", label: "role.editor" },
	{ value: "admin", label: "role.admin" },
];

const selectClass =
	"rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
const quietButton =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

export function AdminUsers({ user }: { user: CurrentUser }) {
	const users = useQuery(usersQuery);
	return (
		<div className="flex flex-col gap-8">
			<h1 className="text-2xl font-semibold">{t("users.title")}</h1>
			<section aria-labelledby="existing-users" className="flex flex-col gap-3">
				<h2 id="existing-users" className="text-lg font-medium">
					{t("users.existing")}
				</h2>
				{users.isPending && <p className="text-slate-500">{t("loading")}</p>}
				<FormError error={users.error} />
				<ul className="flex flex-col gap-3">
					{users.data?.map((account) => (
						<UserRow
							key={account.id}
							account={account}
							isMe={account.id === user.id}
						/>
					))}
				</ul>
			</section>
			<CreateUser />
		</div>
	);
}

function UserRow({ account, isMe }: { account: Account; isMe: boolean }) {
	const update = useUpdateUser();
	const endSessions = useEndUserSessions();
	const [password, setPassword] = useState<string>();
	const errors = fieldErrors(update.error);

	function change(
		changes: Parameters<typeof update.mutate>[0]["changes"],
		after?: () => void,
	) {
		update.mutate({ id: account.id, changes }, { onSuccess: after });
	}

	return (
		<li
			aria-label={account.username}
			className="flex flex-col gap-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-950"
		>
			<div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
				<span className="font-medium">{account.username}</span>
				{isMe && (
					<span className="text-sm text-slate-500">{t("users.you")}</span>
				)}
				{account.email && (
					<span className="text-sm text-slate-600 dark:text-slate-400">
						{account.email}
					</span>
				)}
				{account.disabled && (
					<span className="rounded bg-slate-200 px-2 text-sm dark:bg-slate-800">
						{t("users.disabled")}
					</span>
				)}
				<span className="text-sm text-slate-500">
					{account.lastSeenAt
						? t("users.lastSeen", { date: formatDate(account.lastSeenAt) })
						: t("users.neverSeen")}
				</span>
			</div>
			<div className="flex flex-wrap items-end gap-3">
				<label className="flex flex-col gap-1 text-sm font-medium">
					{t("users.role")}
					<select
						value={account.role}
						disabled={update.isPending}
						onChange={(e) => change({ role: e.target.value as Role })}
						className={selectClass}
					>
						{ROLES.map((r) => (
							<option key={r.value} value={r.value}>
								{t(r.label)}
							</option>
						))}
					</select>
				</label>
				<button
					type="button"
					disabled={update.isPending}
					onClick={() => change({ disabled: !account.disabled })}
					className={quietButton}
				>
					{account.disabled ? t("users.enable") : t("users.disable")}
				</button>
				<button
					type="button"
					disabled={endSessions.isPending || account.sessions === 0}
					onClick={() => endSessions.mutate(account.id)}
					className={quietButton}
				>
					{t("users.signOutEverywhere", { count: account.sessions })}
				</button>
				{password === undefined && (
					<button
						type="button"
						onClick={() => setPassword("")}
						className={quietButton}
					>
						{t("users.newPassword")}
					</button>
				)}
			</div>
			{password !== undefined && (
				<form
					onSubmit={(e) => {
						e.preventDefault();
						change({ password }, () => setPassword(undefined));
					}}
					className="flex flex-wrap items-end gap-3"
				>
					<div className="min-w-48 flex-1">
						<Field
							label={t("users.newPassword.label", { name: account.username })}
							type="password"
							autoComplete="new-password"
							hint={t("users.newPassword.hint")}
							value={password}
							onChange={(e) => setPassword(e.target.value)}
							error={errors.password}
						/>
					</div>
					<SubmitButton pending={update.isPending}>
						{t("users.newPassword.save")}
					</SubmitButton>
					<button
						type="button"
						onClick={() => setPassword(undefined)}
						className={quietButton}
					>
						{t("users.cancel")}
					</button>
				</form>
			)}
			{!errors.password && (
				<FormError error={update.error ?? endSessions.error} />
			)}
		</li>
	);
}

function CreateUser() {
	const create = useCreateUser();
	const [username, setUsername] = useState("");
	const [email, setEmail] = useState("");
	const [password, setPassword] = useState("");
	const [role, setRole] = useState<Role>("reader");
	const errors = fieldErrors(create.error);

	function submit(event: FormEvent) {
		event.preventDefault();
		create.mutate(
			{ username, password, role, email: email || undefined },
			{
				onSuccess: () => {
					setUsername("");
					setEmail("");
					setPassword("");
					setRole("reader");
				},
			},
		);
	}

	return (
		<section
			aria-labelledby="add-user"
			className="flex max-w-md flex-col gap-4"
		>
			<h2 id="add-user" className="text-lg font-medium">
				{t("users.add")}
			</h2>
			<form onSubmit={submit} className="flex flex-col gap-4" noValidate>
				<Field
					label={t("field.username")}
					autoComplete="off"
					value={username}
					onChange={(e) => setUsername(e.target.value)}
					error={errors.username}
				/>
				<Field
					label={t("field.email")}
					optional
					type="email"
					autoComplete="off"
					value={email}
					onChange={(e) => setEmail(e.target.value)}
					error={errors.email}
				/>
				<Field
					label={t("field.password")}
					type="password"
					autoComplete="new-password"
					hint={t("users.add.passwordHint")}
					value={password}
					onChange={(e) => setPassword(e.target.value)}
					error={errors.password}
				/>
				<label className="flex flex-col gap-1 text-sm font-medium">
					{t("users.role")}
					<select
						value={role}
						onChange={(e) => setRole(e.target.value as Role)}
						className={selectClass}
					>
						{ROLES.map((r) => (
							<option key={r.value} value={r.value}>
								{t(r.label)}
							</option>
						))}
					</select>
				</label>
				{Object.keys(errors).length === 0 && <FormError error={create.error} />}
				<div>
					<SubmitButton pending={create.isPending}>
						{create.isPending
							? t("users.add.submitting")
							: t("users.add.submit")}
					</SubmitButton>
				</div>
			</form>
		</section>
	);
}
