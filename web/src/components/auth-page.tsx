import type { ReactNode } from "react";

/** The frame the setup and sign-in pages share: the name, a title, a form. */
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
		<main className="mx-auto flex min-h-dvh w-full max-w-sm flex-col justify-center gap-6 px-4 py-8">
			<p
				className="text-center text-4xl font-bold tracking-tight"
				aria-hidden="true"
			>
				<span className="text-brand-strong dark:text-brand">GO</span>tome
			</p>
			<div className="flex flex-col gap-2">
				<h1 className="text-2xl font-semibold">{title}</h1>
				{intro && <p className="text-slate-600 dark:text-slate-400">{intro}</p>}
			</div>
			{children}
		</main>
	);
}
