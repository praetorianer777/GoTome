import type { ReactNode } from "react";

/**
 * The frame the setup and sign-in pages share: the hero, the name as text, a
 * title and a form on a panel. The art is decoration, so the name beside it
 * is text, not part of the image.
 */
export function AuthPage({
	title,
	intro,
	children,
}: {
	title: string;
	intro?: string;
	children: ReactNode;
}) {
	return (
		<main className="mx-auto flex min-h-dvh w-full max-w-xl flex-col justify-center gap-4 px-4 py-8">
			<div
				className="hero-art relative h-40 overflow-hidden rounded-2xl border border-slate-200 sm:h-56 dark:border-slate-700"
			>
				<span className="panel absolute right-3 bottom-3 rounded-xl px-3 py-1 text-xl tracking-tight sm:top-1/2 sm:right-4 sm:bottom-auto sm:-translate-y-1/2 sm:px-5 sm:py-3 sm:text-3xl">
					<span className="font-sans font-extrabold text-brand-strong dark:text-brand">
						GO
					</span>
					<span className="font-serif font-semibold text-leather dark:text-cream">
						tome
					</span>
				</span>
			</div>
			<div className="panel flex flex-col gap-6 rounded-2xl p-6">
				<div className="flex flex-col gap-2">
					<h1 className="text-2xl font-semibold">{title}</h1>
					{intro && (
						<p className="text-slate-600 dark:text-slate-400">{intro}</p>
					)}
				</div>
				{children}
			</div>
		</main>
	);
}
