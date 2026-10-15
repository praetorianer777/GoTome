import type { ReactNode } from "react";

/**
 * The parts every page is made of, as docs/design.md describes them. Pages
 * take their classes from here rather than spelling them out again.
 */

export type ButtonKind = "primary" | "secondary" | "danger" | "toggle";

const BUTTONS: Record<ButtonKind, string> = {
	primary:
		"bg-brand-strong font-medium text-white hover:bg-sky-800 dark:bg-brand dark:text-ink dark:hover:bg-sky-300",
	secondary:
		"border border-slate-300 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800",
	danger:
		"border border-red-300 text-red-700 hover:bg-red-50 dark:border-red-800 dark:text-red-300 dark:hover:bg-red-950",
	toggle:
		"border border-slate-300 hover:bg-slate-100 aria-pressed:border-brand-strong aria-pressed:bg-brand-strong aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:border-brand dark:aria-pressed:bg-brand dark:aria-pressed:text-ink",
};

/** The classes of a button, or of a link that acts as one. */
export function button(
	kind: ButtonKind = "secondary",
	size: "sm" | "md" = "sm",
): string {
	const pad = size === "sm" ? "px-3 py-1" : "px-4 py-2";
	return `inline-flex items-center justify-center gap-2 rounded-md ${pad} disabled:opacity-60 ${BUTTONS[kind]}`;
}

/** The classes of a select or a text field. */
export const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

/**
 * A page's title, with how many it lists where that is known, a sentence
 * about it, and its primary action at the right.
 */
export function PageHeader({
	title,
	count,
	intro,
	actions,
}: {
	title: string;
	count?: string;
	intro?: ReactNode;
	actions?: ReactNode;
}) {
	return (
		<div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
			<div className="flex min-w-0 flex-col gap-1">
				<div className="flex flex-wrap items-baseline gap-x-3">
					<h1 className="text-2xl font-semibold">{title}</h1>
					{count && (
						<span className="text-sm text-slate-600 dark:text-slate-400">
							{count}
						</span>
					)}
				</div>
				{intro && (
					<p className="max-w-2xl text-slate-600 dark:text-slate-400">
						{intro}
					</p>
				)}
			</div>
			{actions && (
				<div className="flex flex-wrap items-center gap-2">{actions}</div>
			)}
		</div>
	);
}

/** What a page or a list says when it has nothing to show, and what fills it. */
export function EmptyState({
	title,
	children,
	action,
	level = 2,
}: {
	title?: string;
	children?: ReactNode;
	action?: ReactNode;
	level?: 2 | 3;
}) {
	const Heading = level === 2 ? "h2" : "h3";
	return (
		<div className="rounded-lg border border-dashed border-slate-300 px-6 py-12 text-center dark:border-slate-700">
			{title && <Heading className="text-lg font-medium">{title}</Heading>}
			{children && (
				<div className="mx-auto mt-2 max-w-md text-slate-600 dark:text-slate-400">
					{children}
				</div>
			)}
			{action && <div className="mt-4 flex justify-center">{action}</div>}
		</div>
	);
}

/** A grey block standing for something still loading. */
export function Skeleton({ className = "" }: { className?: string }) {
	return (
		<div
			aria-hidden="true"
			className={`animate-pulse rounded-md bg-slate-200 motion-reduce:animate-none dark:bg-slate-800 ${className}`}
		/>
	);
}
