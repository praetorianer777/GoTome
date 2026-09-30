import { Link } from "@tanstack/react-router";

export function NotFound() {
	return (
		<main className="mx-auto flex min-h-dvh max-w-2xl flex-col items-center justify-center gap-4 px-4 text-center">
			<h1 className="text-3xl font-bold">There is no page here</h1>
			<p className="text-slate-400">
				The address may be mistyped, or the page has moved.
			</p>
			<Link to="/" className="text-brand underline underline-offset-4">
				Back to the library
			</Link>
		</main>
	);
}
