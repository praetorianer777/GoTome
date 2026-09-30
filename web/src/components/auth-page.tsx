import type { ReactNode } from "react";
import artwork from "@/assets/artwork.webp";
import { t } from "@/i18n";

/** The frame the setup and sign-in pages share: the artwork, a title, a form. */
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
			{/* The artwork carries the name. Width and height are the file's own, so
			    the browser keeps the space free before the image arrives. */}
			<img
				src={artwork}
				width={640}
				height={659}
				alt={t("app.name")}
				className="mx-auto h-auto w-48 rounded-2xl sm:w-64"
				decoding="async"
			/>
			<div className="flex flex-col gap-2">
				<h1 className="text-2xl font-semibold">{title}</h1>
				{intro && <p className="text-slate-600 dark:text-slate-400">{intro}</p>}
			</div>
			{children}
		</main>
	);
}
