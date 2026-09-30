import { coverUrl } from "@/books/api";

/**
 * A book's cover, or where it has none a plain card with its title, so that
 * a shelf of books without covers is still a shelf.
 */
export function Cover({
	book,
	size,
	className = "",
}: {
	book: { id: string; title: string; coverKey?: string };
	size: "small" | "large";
	className?: string;
}) {
	const src = coverUrl(book, size);
	if (src) {
		return (
			<img
				src={src}
				alt=""
				loading="lazy"
				decoding="async"
				className={`aspect-[2/3] w-full rounded-md bg-slate-200 object-cover shadow-sm dark:bg-slate-800 ${className}`}
			/>
		);
	}
	return (
		<div
			aria-hidden="true"
			className={`flex aspect-[2/3] w-full items-center justify-center overflow-hidden rounded-md bg-gradient-to-b from-slate-200 to-slate-300 p-3 text-center font-serif text-sm text-slate-700 shadow-sm dark:from-slate-800 dark:to-slate-900 dark:text-slate-300 ${className}`}
		>
			<span className="line-clamp-5">{book.title}</span>
		</div>
	);
}
