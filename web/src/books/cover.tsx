import { coverUrl } from "@/books/api";
import { t } from "@/i18n";

/**
 * A book's cover, or where it has none a plain card with its title, so that
 * a shelf of books without covers is still a shelf. Beside the book's title,
 * as in a list, a cover says nothing more and is passed over by screen
 * readers; named, it is announced as the cover of the book.
 */
export function Cover({
	book,
	size,
	className = "",
	named = false,
}: {
	book: { id: string; title: string; coverKey?: string };
	size: "small" | "large";
	className?: string;
	named?: boolean;
}) {
	const src = coverUrl(book, size);
	if (src) {
		return (
			<img
				src={src}
				alt={named ? t("book.cover", { title: book.title }) : ""}
				loading="lazy"
				decoding="async"
				className={`aspect-[2/3] w-full rounded-[3px] bg-slate-200 object-cover shadow-book dark:bg-slate-800 ${className}`}
			/>
		);
	}
	return (
		<div
			aria-hidden="true"
			className={`flex aspect-[2/3] w-full items-center justify-center overflow-hidden rounded-[3px] border-l-4 border-black/15 bg-gradient-to-br from-slate-100 to-slate-300 p-3 text-center font-serif text-sm text-leather shadow-book dark:border-black/40 dark:from-slate-800 dark:to-slate-950 dark:text-cream ${className}`}
		>
			<span className="line-clamp-5">{book.title}</span>
		</div>
	);
}
