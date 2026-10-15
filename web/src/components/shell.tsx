import {
	Link,
	Outlet,
	useMatchRoute,
	useNavigate,
	useRouterState,
} from "@tanstack/react-router";
import { useEffect, useId, useRef, useState } from "react";
import {
	type CurrentUser,
	can,
	type Permission,
	useLogout,
} from "@/auth/session";
import mark from "@/assets/mark.webp";
import { QuickSearch } from "@/books/quick-search";
import { Dialog } from "@/components/dialog";
import { LanguageSelect, ThemeSelect } from "@/components/preferences";
import { useServerVersion, VersionFooter } from "@/components/version-footer";
import { Bell } from "@/notifications/bell";
import { type MessageKey, t } from "@/i18n";
import { MiniPlayer } from "@/player/controls";
import { PlayerProvider } from "@/player/player";

export type NavGroup = "library" | "catalogue" | "admin";

export interface NavItem {
	to:
		| "/"
		| "/search"
		| "/duplicates"
		| "/upload"
		| "/review"
		| "/cleanup"
		| "/collections"
		| "/wishlist"
		| "/releases"
		| "/stats"
		| "/overview"
		| "/profile"
		| "/jobs"
		| "/trash"
		| "/admin/libraries"
		| "/admin/users"
		| "/admin/settings";
	label: MessageKey;
	/**
	 * What the person must be allowed to do for the entry to be offered;
	 * none for a page every signed-in person may open.
	 */
	permission?: Permission;
	group: NavGroup;
	icon: IconName;
}

/**
 * The main navigation. An entry is listed here with the permission its page
 * needs, so the menu and the server cannot disagree about who sees what.
 */
export const NAV_ITEMS: NavItem[] = [
	{
		to: "/",
		label: "nav.library",
		permission: "library:read",
		group: "library",
		icon: "library",
	},
	{
		to: "/search",
		label: "nav.search",
		permission: "library:read",
		group: "library",
		icon: "search",
	},
	{
		to: "/collections",
		label: "nav.collections",
		permission: "library:read",
		group: "library",
		icon: "shelf",
	},
	{
		to: "/wishlist",
		label: "nav.wishlist",
		permission: "personal:manage",
		group: "library",
		icon: "heart",
	},
	{
		to: "/releases",
		label: "nav.releases",
		permission: "personal:manage",
		group: "library",
		icon: "spark",
	},
	{
		to: "/stats",
		label: "nav.stats",
		permission: "personal:manage",
		group: "library",
		icon: "chart",
	},
	{
		to: "/overview",
		label: "nav.overview",
		permission: "library:read",
		group: "library",
		icon: "gauge",
	},
	{ to: "/profile", label: "nav.profile", group: "library", icon: "person" },
	{
		to: "/upload",
		label: "nav.upload",
		permission: "books:upload",
		group: "catalogue",
		icon: "upload",
	},
	{
		to: "/review",
		label: "nav.review",
		permission: "metadata:edit",
		group: "catalogue",
		icon: "check",
	},
	{
		to: "/duplicates",
		label: "nav.duplicates",
		permission: "library:read",
		group: "catalogue",
		icon: "copy",
	},
	{
		to: "/cleanup",
		label: "nav.cleanup",
		permission: "metadata:edit",
		group: "catalogue",
		icon: "wand",
	},
	{
		to: "/trash",
		label: "nav.trash",
		permission: "metadata:edit",
		group: "catalogue",
		icon: "trash",
	},
	{
		to: "/jobs",
		label: "nav.jobs",
		permission: "index:rebuild",
		group: "catalogue",
		icon: "jobs",
	},
	{
		to: "/admin/libraries",
		label: "nav.libraries",
		permission: "storage:manage",
		group: "admin",
		icon: "folder",
	},
	{
		to: "/admin/users",
		label: "nav.users",
		permission: "users:manage",
		group: "admin",
		icon: "people",
	},
	{
		to: "/admin/settings",
		label: "nav.settings",
		permission: "settings:manage",
		group: "admin",
		icon: "sliders",
	},
];

export function visibleNavItems(
	items: NavItem[],
	user: CurrentUser | null,
): NavItem[] {
	return items.filter(
		(item) => !!user && (!item.permission || can(user, item.permission)),
	);
}

const GROUPS: { group: NavGroup; label?: MessageKey }[] = [
	{ group: "library" },
	{ group: "catalogue", label: "nav.group.catalogue" },
	{ group: "admin", label: "nav.group.admin" },
];

const ROLE_LABELS: Record<string, MessageKey> = {
	admin: "role.admin",
	editor: "role.editor",
	reader: "role.reader",
};

export function Shell({ user }: { user: CurrentUser }) {
	const version = useServerVersion();
	const matchRoute = useMatchRoute();
	// The readers and the player take the whole screen.
	const fullScreen =
		!!matchRoute({ to: "/books/$bookId/read/$fileId" }) ||
		!!matchRoute({ to: "/books/$bookId/listen" });
	const [drawer, setDrawer] = useState(false);

	return (
		<PlayerProvider>
			<div className="flex min-h-dvh">
				<a
					href="#content"
					className="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-white focus:px-3 focus:py-2 focus:text-slate-900"
				>
					{t("nav.skip")}
				</a>
				<aside className="sticky top-0 hidden h-dvh w-60 shrink-0 flex-col gap-6 overflow-y-auto border-r border-slate-200 bg-slate-100/70 px-3 py-4 lg:flex dark:border-slate-800 dark:bg-slate-950">
					<Wordmark />
					<Navigation user={user} />
				</aside>
				<div className="flex min-w-0 flex-1 flex-col">
					<header className="z-30 border-b lg:sticky lg:top-0 border-slate-200 bg-slate-50/90 backdrop-blur dark:border-slate-800 dark:bg-ink/90">
						<div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5">
							<button
								type="button"
								aria-label={t("nav.menu")}
								aria-expanded={drawer}
								onClick={() => setDrawer(true)}
								className="rounded-md p-1.5 text-slate-700 hover:bg-slate-200 lg:hidden dark:text-slate-200 dark:hover:bg-slate-800"
							>
								<Icon name="menu" className="h-6 w-6" />
							</button>
							<div className="lg:hidden">
								<Wordmark compact />
							</div>
							{can(user, "library:read") && (
								<div className="order-last w-full sm:order-none sm:w-auto">
									<QuickSearch />
								</div>
							)}
							<div className="ml-auto flex items-center gap-2">
								<Bell />
								<AccountMenu user={user} />
							</div>
						</div>
					</header>
					<main
						id="content"
						className="mx-auto w-full max-w-6xl flex-1 px-4 py-6"
					>
						<Outlet />
					</main>
					{!fullScreen && <VersionFooter {...version} />}
					<MiniPlayer />
				</div>
			</div>
			{drawer && (
				<Dialog title={t("nav.menu")} onClose={() => setDrawer(false)} drawer>
					<Navigation user={user} onNavigate={() => setDrawer(false)} />
				</Dialog>
			)}
		</PlayerProvider>
	);
}

/** The icon and the name as the artwork writes it: "GO" in cyan, "tome" in cream serif. */
function Wordmark({ compact = false }: { compact?: boolean }) {
	return (
		<Link to="/" className="flex items-center gap-2 rounded-md px-1">
			{/* Decorative: the name beside it is the link's text. */}
			<img
				src={mark}
				width={compact ? 28 : 32}
				height={compact ? 28 : 32}
				alt=""
				className="rounded-md"
			/>
			<span className={`${compact ? "text-lg" : "text-xl"} tracking-tight`}>
				<span className="font-sans font-extrabold text-brand-strong dark:text-brand">
					GO
				</span>
				<span className="font-serif font-semibold text-leather dark:text-cream">
					tome
				</span>
			</span>
		</Link>
	);
}

const FOLD_KEY = "gotome.nav.folded";

function storedFolds(): NavGroup[] {
	try {
		return JSON.parse(localStorage.getItem(FOLD_KEY) ?? "[]") as NavGroup[];
	} catch {
		return [];
	}
}

function Navigation({
	user,
	onNavigate,
}: {
	user: CurrentUser;
	onNavigate?: () => void;
}) {
	const items = visibleNavItems(NAV_ITEMS, user);
	const path = useRouterState({ select: (s) => s.location.pathname });
	const [folded, setFolded] = useState<NavGroup[]>(storedFolds);
	const baseId = useId();
	const fold = (group: NavGroup) => {
		const next = folded.includes(group)
			? folded.filter((g) => g !== group)
			: [...folded, group];
		setFolded(next);
		try {
			localStorage.setItem(FOLD_KEY, JSON.stringify(next));
		} catch {
			// Without storage the choice lasts for the visit.
		}
	};

	return (
		<nav aria-label={t("nav.main")} className="flex flex-col gap-5 text-sm">
			{GROUPS.map(({ group, label }) => {
				const entries = items.filter((item) => item.group === group);
				if (entries.length === 0) return null;
				// A group holding the page shown stays open.
				const open =
					!folded.includes(group) || entries.some((item) => item.to === path);
				const listId = `${baseId}-${group}`;
				return (
					<div key={group} className="flex flex-col gap-1">
						{label && (
							<button
								type="button"
								aria-expanded={open}
								aria-controls={listId}
								onClick={() => fold(group)}
								className="flex items-center justify-between rounded-md px-3 py-1 text-xs font-semibold tracking-wider text-slate-600 uppercase hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
							>
								{t(label)}
								<Icon
									name="chevron"
									className={`h-4 w-4 transition-transform ${open ? "" : "-rotate-90"}`}
								/>
							</button>
						)}
						{open && (
							<ul id={listId} className="flex flex-col gap-0.5">
								{entries.map((item) => (
									<li key={item.to}>
										<Link
											to={item.to}
											activeOptions={{ exact: true, includeSearch: false }}
											onClick={onNavigate}
											className="flex items-center gap-3 rounded-md px-3 py-1.5 text-slate-700 hover:bg-slate-200/70 hover:text-slate-900 data-[status=active]:bg-brand-strong/10 data-[status=active]:font-semibold data-[status=active]:text-brand-strong dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white dark:data-[status=active]:bg-brand/10 dark:data-[status=active]:text-brand dark:data-[status=active]:shadow-glow"
										>
											<Icon
												name={item.icon}
												className="h-[18px] w-[18px] shrink-0"
											/>
											{t(item.label)}
										</Link>
									</li>
								))}
							</ul>
						)}
					</div>
				);
			})}
		</nav>
	);
}

/** Up to two letters of the name, for the account button. */
export function initials(name: string): string {
	const words = name.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
	const letters =
		words.length > 1 ? `${words[0]?.[0]}${words[1]?.[0]}` : name.slice(0, 2);
	return letters.toUpperCase();
}

function AccountMenu({ user }: { user: CurrentUser }) {
	const logout = useLogout();
	const navigate = useNavigate();
	const [open, setOpen] = useState(false);
	const box = useRef<HTMLDivElement>(null);
	const toggle = useRef<HTMLButtonElement>(null);
	const panelId = useId();
	const roleLabel = ROLE_LABELS[user.role];

	useEffect(() => {
		if (!open) return;
		const close = (e: Event) => {
			if (
				e instanceof KeyboardEvent
					? e.key === "Escape"
					: !box.current?.contains(e.target as Node)
			) {
				setOpen(false);
				if (e instanceof KeyboardEvent) toggle.current?.focus();
			}
		};
		document.addEventListener("keydown", close);
		document.addEventListener("pointerdown", close);
		return () => {
			document.removeEventListener("keydown", close);
			document.removeEventListener("pointerdown", close);
		};
	}, [open]);

	const item =
		"flex items-center gap-3 rounded-md px-2 py-1.5 text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800";

	return (
		<div ref={box} className="relative">
			<button
				ref={toggle}
				type="button"
				aria-expanded={open}
				aria-controls={panelId}
				aria-label={t("user.menuFor", { name: user.username })}
				onClick={() => setOpen(!open)}
				className="flex items-center gap-2 rounded-full py-0.5 pr-0.5 pl-0.5 hover:bg-slate-200 sm:pr-3 dark:hover:bg-slate-800"
			>
				<span
					aria-hidden="true"
					className="flex h-8 w-8 items-center justify-center rounded-full bg-brand-strong text-xs font-bold text-white dark:bg-brand dark:text-ink"
				>
					{initials(user.username)}
				</span>
				<span
					aria-hidden="true"
					className="hidden max-w-32 truncate text-sm sm:inline"
				>
					{user.username}
				</span>
			</button>
			{open && (
				<div
					id={panelId}
					role="group"
					aria-label={t("user.menu")}
					className="absolute right-0 z-40 mt-2 flex w-72 max-w-[calc(100vw-2rem)] flex-col gap-1 rounded-lg border border-slate-200 bg-white p-2 text-sm shadow-lg dark:border-slate-700 dark:bg-slate-900"
				>
					<p className="px-2 pt-1 pb-2">
						<span className="block font-medium">
							{t("user.signedInAs", { name: user.username })}
						</span>
						<span className="text-slate-600 dark:text-slate-400">
							{roleLabel ? t(roleLabel) : user.role}
						</span>
					</p>
					<Link to="/profile" onClick={() => setOpen(false)} className={item}>
						<Icon name="person" className="h-4 w-4" />
						{t("nav.profile")}
					</Link>
					<Link
						to="/profile"
						hash="devices"
						onClick={() => setOpen(false)}
						className={item}
					>
						<Icon name="device" className="h-4 w-4" />
						{t("devices.title")}
					</Link>
					<div className="my-1 flex flex-col gap-2 border-y border-slate-200 px-2 py-2 dark:border-slate-700">
						<LanguageSelect />
						<ThemeSelect />
					</div>
					<button
						type="button"
						disabled={logout.isPending}
						onClick={() =>
							logout.mutate(undefined, {
								onSuccess: () => navigate({ to: "/login" }),
							})
						}
						className={`${item} disabled:opacity-60`}
					>
						<Icon name="signOut" className="h-4 w-4" />
						{t("user.signOut")}
					</button>
				</div>
			)}
		</div>
	);
}

export type IconName =
	| "library"
	| "search"
	| "shelf"
	| "heart"
	| "spark"
	| "chart"
	| "gauge"
	| "person"
	| "upload"
	| "check"
	| "copy"
	| "wand"
	| "trash"
	| "jobs"
	| "folder"
	| "people"
	| "sliders"
	| "menu"
	| "chevron"
	| "device"
	| "signOut";

const ICONS: Record<IconName, string[]> = {
	library: ["M4 4h3v16H4z", "M9 4h3v16H9z", "M14.5 4.7l2.9-.8 3.9 15.4-2.9.8z"],
	search: [
		"M10.5 4a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13z",
		"M15.5 15.5 20 20",
	],
	shelf: ["M4 20h16", "M5 16V5h4v11", "M10 16V8h4v8", "M15 16V6h4v10"],
	heart: [
		"M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10z",
	],
	spark: [
		"M12 3v4",
		"M12 17v4",
		"M3 12h4",
		"M17 12h4",
		"M12 8l1.5 2.5L16 12l-2.5 1.5L12 16l-1.5-2.5L8 12l2.5-1.5z",
	],
	chart: ["M4 20h16", "M6 16v-5", "M11 16V6", "M16 16v-8"],
	gauge: ["M4 18a8 8 0 1 1 16 0", "M12 18l4-6"],
	person: [
		"M12 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8z",
		"M4 20c1.5-4 4.5-6 8-6s6.5 2 8 6",
	],
	upload: ["M12 16V4", "M7 9l5-5 5 5", "M4 16v4h16v-4"],
	check: ["M12 3a9 9 0 1 1 0 18 9 9 0 0 1 0-18z", "M8 12l3 3 5-6"],
	copy: ["M8 8h11v12H8z", "M5 16V4h11"],
	wand: ["M4 20 15 9", "M17 3v4", "M15 5h4", "M20 10v2", "M19 11h2"],
	trash: ["M4 7h16", "M9 7V4h6v3", "M6 7l1 13h10l1-13"],
	jobs: ["M5 6h14", "M5 12h14", "M5 18h9"],
	folder: ["M3 6h6l2 2h10v11H3z"],
	people: [
		"M9 5a3.5 3.5 0 1 1 0 7 3.5 3.5 0 0 1 0-7z",
		"M3 20c1-3.5 3.3-5.5 6-5.5s5 2 6 5.5",
		"M16 5.5a3 3 0 0 1 0 6",
		"M18 14.5c1.5.8 2.5 2.6 3 5",
	],
	sliders: ["M4 7h10", "M18 7h2", "M16 5v4", "M4 17h4", "M12 17h8", "M10 15v4"],
	menu: ["M4 7h16", "M4 12h16", "M4 17h16"],
	chevron: ["M6 9l6 6 6-6"],
	device: ["M7 3h10v18H7z", "M11 18h2"],
	signOut: ["M10 4H5v16h5", "M15 8l4 4-4 4", "M19 12H9"],
};

export function Icon({
	name,
	className,
}: {
	name: IconName;
	className?: string;
}) {
	return (
		<svg
			aria-hidden="true"
			viewBox="0 0 24 24"
			className={`fill-none stroke-current stroke-[1.75] [stroke-linecap:round] [stroke-linejoin:round] ${className ?? ""}`}
		>
			{ICONS[name].map((d) => (
				<path key={d} d={d} />
			))}
		</svg>
	);
}
