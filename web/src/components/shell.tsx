import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import {
	type CurrentUser,
	can,
	type Permission,
	useLogout,
} from "@/auth/session";
import { type MessageKey, t } from "@/i18n";
import { type Theme, useTheme } from "@/lib/theme";

export interface NavItem {
	to: "/";
	label: MessageKey;
	/** What the person must be allowed to do for the entry to be offered. */
	permission: Permission;
}

/**
 * The main navigation. An entry is listed here with the permission its page
 * needs, so the menu and the server cannot disagree about who sees what.
 */
export const NAV_ITEMS: NavItem[] = [
	{ to: "/", label: "nav.library", permission: "library:read" },
];

export function visibleNavItems(
	items: NavItem[],
	user: CurrentUser | null,
): NavItem[] {
	return items.filter((item) => can(user, item.permission));
}

const THEMES: { value: Theme; label: MessageKey }[] = [
	{ value: "system", label: "theme.system" },
	{ value: "light", label: "theme.light" },
	{ value: "dark", label: "theme.dark" },
];

const ROLE_LABELS: Record<string, MessageKey> = {
	admin: "role.admin",
	editor: "role.editor",
	reader: "role.reader",
};

export function Shell({ user }: { user: CurrentUser }) {
	const logout = useLogout();
	const navigate = useNavigate();
	const [theme, setTheme] = useTheme();
	const roleLabel = ROLE_LABELS[user.role];

	return (
		<div className="flex min-h-dvh flex-col">
			<a
				href="#content"
				className="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-10 focus:rounded-md focus:bg-white focus:px-3 focus:py-2 focus:text-slate-900"
			>
				{t("nav.skip")}
			</a>
			<header className="border-b border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-950">
				<div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
					<Link to="/" className="text-xl font-bold tracking-tight">
						<span className="text-brand-strong dark:text-brand">GO</span>tome
					</Link>
					<nav aria-label={t("nav.main")} className="flex gap-4">
						{visibleNavItems(NAV_ITEMS, user).map((item) => (
							<Link
								key={item.to}
								to={item.to}
								className="rounded px-1 py-0.5 text-slate-600 hover:text-slate-900 data-[status=active]:font-semibold data-[status=active]:text-slate-900 dark:text-slate-300 dark:hover:text-white dark:data-[status=active]:text-white"
							>
								{t(item.label)}
							</Link>
						))}
					</nav>
					<div className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
						<label className="flex items-center gap-2">
							<span className="text-slate-600 dark:text-slate-400">
								{t("theme.label")}
							</span>
							<select
								value={theme}
								onChange={(e) => setTheme(e.target.value as Theme)}
								className="rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100"
							>
								{THEMES.map((option) => (
									<option key={option.value} value={option.value}>
										{t(option.label)}
									</option>
								))}
							</select>
						</label>
						<span title={roleLabel ? t(roleLabel) : user.role}>
							{t("user.signedInAs", { name: user.username })}
						</span>
						<button
							type="button"
							disabled={logout.isPending}
							onClick={() =>
								logout.mutate(undefined, {
									onSuccess: () => navigate({ to: "/login" }),
								})
							}
							className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
						>
							{t("user.signOut")}
						</button>
					</div>
				</div>
			</header>
			<main id="content" className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
				<Outlet />
			</main>
		</div>
	);
}
