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
import mascotCorner from "@/assets/mascot-corner.webp";
import mascotSidebar from "@/assets/mascot-sidebar.webp";
import { QuickSearch } from "@/books/quick-search";
import { Dialog } from "@/components/dialog";
import { Celebration } from "@/components/gopher";
import { panel } from "@/components/ui";
import { LanguageSelect, ThemeSelect } from "@/components/preferences";
import { useServerVersion, VersionFooter } from "@/components/version-footer";
import { Bell } from "@/notifications/bell";
import { type MessageKey, t } from "@/i18n";
import { MiniPlayer } from "@/player/controls";
import { PlayerProvider } from "@/player/player";

export type NavGroup =
	| "sections"
	| "ebooks"
	| "audiobooks"
	| "more"
	| "catalogue"
	| "admin";

const EBOOK_FORMATS = ["epub", "pdf", "mobi", "azw3", "azw"];
const AUDIO_FORMATS = ["m4b", "m4a", "mp3", "flac", "ogg", "opus"];

export interface NavItem {
	to:
		| "/"
		| "/authors"
		| "/series"
		| "/search"
		| "/duplicates"
		| "/upload"
		| "/review"
		| "/cleanup"
		| "/genres"
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
	/** The library's filter the entry opens it with: all of one medium. */
	search?: { format: string[] };
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
		group: "sections",
		icon: "library",
	},
	{
		to: "/wishlist",
		label: "nav.wishlist",
		permission: "personal:manage",
		group: "sections",
		icon: "heart",
	},
	{
		to: "/releases",
		label: "nav.releases",
		permission: "personal:manage",
		group: "sections",
		icon: "spark",
	},
	{
		to: "/stats",
		label: "nav.stats",
		permission: "personal:manage",
		group: "sections",
		icon: "chart",
	},
	{
		to: "/",
		label: "nav.allEbooks",
		search: { format: EBOOK_FORMATS },
		permission: "library:read",
		group: "ebooks",
		icon: "book",
	},
	{
		to: "/authors",
		label: "nav.authors",
		permission: "library:read",
		group: "ebooks",
		icon: "person",
	},
	{
		to: "/series",
		label: "nav.series",
		permission: "library:read",
		group: "ebooks",
		icon: "shelf",
	},
	{
		to: "/collections",
		label: "nav.collections",
		permission: "library:read",
		group: "ebooks",
		icon: "folder",
	},
	{
		to: "/search",
		label: "nav.search",
		permission: "library:read",
		group: "ebooks",
		icon: "search",
	},
	{
		to: "/",
		label: "nav.allAudiobooks",
		search: { format: AUDIO_FORMATS },
		permission: "library:read",
		group: "audiobooks",
		icon: "headphones",
	},
	{
		to: "/overview",
		label: "nav.overview",
		permission: "library:read",
		group: "more",
		icon: "gauge",
	},
	{ to: "/profile", label: "nav.profile", group: "more", icon: "person" },
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
		to: "/genres",
		label: "nav.genres",
		permission: "metadata:edit",
		group: "catalogue",
		icon: "tag",
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

/**
 * The sidebar's groups in order. The sections are the top bar's tabs on a
 * desktop and appear here only in the drawer; a medium's group has a
 * heading with its icon; the last two fold.
 */
const GROUPS: {
	group: NavGroup;
	label?: MessageKey;
	icon?: IconName;
	folds?: boolean;
}[] = [
	{ group: "sections" },
	{ group: "ebooks", label: "nav.group.ebooks", icon: "book" },
	{ group: "audiobooks", label: "nav.group.audiobooks", icon: "headphones" },
	{ group: "more", label: "nav.group.more" },
	{ group: "catalogue", label: "nav.group.catalogue", folds: true },
	{ group: "admin", label: "nav.group.admin", folds: true },
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
			<div className="flex min-h-dvh flex-col lg:gap-4 lg:p-4">
				<a
					href="#content"
					className="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-white focus:px-3 focus:py-2 focus:text-slate-900"
				>
					{t("nav.skip")}
				</a>
				<header
					className={`${panel} z-30 max-lg:rounded-none max-lg:border-x-0 max-lg:border-t-0 lg:sticky lg:top-4`}
				>
					<div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2">
						<button
							type="button"
							aria-label={t("nav.menu")}
							aria-expanded={drawer}
							onClick={() => setDrawer(true)}
							className="rounded-lg p-1.5 text-slate-700 hover:bg-slate-200 lg:hidden dark:text-slate-200 dark:hover:bg-slate-800"
						>
							<Icon name="menu" className="h-6 w-6" />
						</button>
						<Wordmark />
						<Sections user={user} />
						<div className="ml-auto flex items-center gap-2 lg:order-last">
							<Bell />
							<AccountMenu user={user} />
						</div>
						{can(user, "library:read") && (
							<div className="order-last w-full sm:order-none sm:ml-auto sm:w-auto lg:ml-0">
								<QuickSearch />
							</div>
						)}
					</div>
				</header>
				<div className="flex min-w-0 flex-1 lg:gap-4">
					<aside
						className={`${panel} sticky top-22 hidden h-[calc(100dvh-6.5rem)] w-64 shrink-0 flex-col gap-4 overflow-y-auto px-3 py-4 lg:flex`}
					>
						<Navigation user={user} />
						<img
							src={mascotSidebar}
							alt=""
							width={80}
							height={144}
							className="mt-auto h-36 w-20 shrink-0 self-center object-contain"
						/>
					</aside>
					<div
						className={`${fullScreen ? "panel-solid rounded-2xl" : panel} flex min-w-0 flex-1 flex-col max-lg:rounded-none max-lg:border-x-0 lg:rounded-2xl`}
					>
						<main
							id="content"
							className="mx-auto w-full max-w-6xl flex-1 px-4 py-6"
						>
							<Outlet />
						</main>
						{!fullScreen && (
							<div className="relative">
								<VersionFooter {...version} />
								<img
									src={mascotCorner}
									alt=""
									width={64}
									height={64}
									className="pointer-events-none absolute right-6 bottom-0 hidden size-16 lg:block"
								/>
							</div>
						)}
						<MiniPlayer />
					</div>
				</div>
			</div>
			<Celebration />
			{drawer && (
				<Dialog title={t("nav.menu")} onClose={() => setDrawer(false)} drawer>
					<Navigation
						user={user}
						inDrawer
						onNavigate={() => setDrawer(false)}
					/>
				</Dialog>
			)}
		</PlayerProvider>
	);
}

/** The main sections as tabs in the top bar, on a desktop. */
function Sections({ user }: { user: CurrentUser }) {
	const items = visibleNavItems(NAV_ITEMS, user).filter(
		(item) => item.group === "sections",
	);
	return (
		<nav aria-label={t("nav.sections")} className="hidden lg:block">
			<ul className="flex items-center gap-1">
				{items.map((item) => (
					<li key={item.to}>
						<Link
							to={item.to}
							activeOptions={{ exact: true, includeSearch: false }}
							className="block rounded-full px-4 py-1.5 text-slate-700 hover:bg-slate-200/70 hover:text-slate-900 data-[status=active]:bg-brand-strong/10 data-[status=active]:font-semibold data-[status=active]:text-brand-strong dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white dark:data-[status=active]:bg-slate-800 dark:data-[status=active]:text-white"
						>
							{t(item.label)}
						</Link>
					</li>
				))}
			</ul>
		</nav>
	);
}

/** The icon and the name as the artwork writes it: "GO" in cyan, "tome" in cream serif. */
function Wordmark() {
	return (
		<Link to="/" className="flex items-center gap-2 rounded-lg px-1">
			{/* Decorative: the name beside it is the link's text. */}
			<img
				src={mark}
				width={32}
				height={32}
				alt=""
				className="rounded-md"
			/>
			<span className="text-xl tracking-tight">
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
	inDrawer = false,
	onNavigate,
}: {
	user: CurrentUser;
	inDrawer?: boolean;
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
			{GROUPS.map(({ group, label, icon, folds }) => {
				if (group === "sections" && !inDrawer) return null;
				const entries = items.filter((item) => item.group === group);
				if (entries.length === 0) return null;
				// A group holding the page shown stays open.
				const open =
					!folds ||
					!folded.includes(group) ||
					entries.some((item) => item.to === path);
				const listId = `${baseId}-${group}`;
				return (
					<div key={group} className="flex flex-col gap-1">
						{label && folds && (
							<button
								type="button"
								aria-expanded={open}
								aria-controls={listId}
								onClick={() => fold(group)}
								className="flex items-center justify-between rounded-lg px-3 py-1 text-xs font-semibold tracking-wider text-slate-600 uppercase hover:text-slate-900 dark:text-white dark:hover:text-slate-100"
							>
								{t(label)}
								<Icon
									name="chevron"
									className={`h-4 w-4 transition-transform ${open ? "" : "-rotate-90"}`}
								/>
							</button>
						)}
						{label && !folds && (
							<p className="flex items-center justify-between px-3 py-1 text-xs font-semibold tracking-wider text-slate-600 uppercase dark:text-white">
								{t(label)}
								{icon && (
									<Icon
										name={icon}
										className="h-5 w-5 text-candle-strong dark:text-candle"
									/>
								)}
							</p>
						)}
						{open && (
							<ul id={listId} className="flex flex-col gap-0.5">
								{entries.map((item) => (
									<li key={`${item.to}${item.search?.format[0] ?? ""}`}>
										<Link
											to={item.to}
											search={item.search}
											activeOptions={{
												exact: true,
												includeSearch: !!item.search,
											}}
											onClick={onNavigate}
											className="flex items-center gap-3 rounded-lg px-3 py-1.5 text-slate-700 hover:bg-slate-200/70 hover:text-slate-900 data-[status=active]:bg-brand-strong/10 data-[status=active]:font-semibold data-[status=active]:text-brand-strong dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white dark:data-[status=active]:bg-slate-800 dark:data-[status=active]:text-white"
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
					className="absolute right-0 z-40 mt-2 flex w-72 max-w-[calc(100vw-2rem)] flex-col gap-1 rounded-xl border border-slate-200 bg-white p-2 text-sm shadow-lg dark:border-slate-700 dark:bg-slate-900"
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
	| "book"
	| "headphones"
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
	| "tag"
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
	tag: ["M4 4h7l9 9-7 7-9-9z", "M8 8h.01"],
	book: ["M12 6c-2-1.5-5-2-8-2v14c3 0 6 .5 8 2 2-1.5 5-2 8-2V4c-3 0-6 .5-8 2z", "M12 6v14"],
	headphones: [
		"M4 15v-3a8 8 0 0 1 16 0v3",
		"M4 15h3v5H5a1 1 0 0 1-1-1z",
		"M20 15h-3v5h2a1 1 0 0 0 1-1z",
	],
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
