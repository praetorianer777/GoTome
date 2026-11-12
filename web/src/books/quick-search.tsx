import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { type NameHit, type SearchHit, searchQuery } from "@/books/api";
import mark from "@/assets/mark.webp";
import { t } from "@/i18n";
import { bookCount } from "@/lib/format";

/** How long typing has to pause before the search is asked. */
const DEBOUNCE_MS = 200;
/** The server finds nothing for fewer letters, so it is not asked. */
const MIN_LETTERS = 3;

function letters(words: string): number {
	return (words.match(/[\p{L}\p{N}]/gu) ?? []).length;
}

/**
 * The search box in the header: titles, authors and series, misspelt or only
 * begun, the authors and series first, then a way to list every book found
 * and last a way to search the text of the books for the words. It is a
 * combobox: the arrow keys move through what was found, Enter opens what is
 * chosen, or with nothing chosen the list of every book found (the text
 * search when there is none), Escape closes the list, and "/" anywhere comes
 * back to it.
 */
export function QuickSearch() {
	const navigate = useNavigate();
	const [words, setWords] = useState("");
	const [asked, setAsked] = useState("");
	const [open, setOpen] = useState(false);
	const [active, setActive] = useState(-1);
	const input = useRef<HTMLInputElement>(null);
	const listId = useId();

	useEffect(() => {
		const timer = setTimeout(() => setAsked(words.trim()), DEBOUNCE_MS);
		return () => clearTimeout(timer);
	}, [words]);

	useEffect(() => {
		const focus = (event: globalThis.KeyboardEvent) => {
			const target = event.target as HTMLElement;
			if (
				event.key !== "/" ||
				event.ctrlKey ||
				event.metaKey ||
				event.altKey ||
				target.isContentEditable ||
				["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName)
			) {
				return;
			}
			event.preventDefault();
			input.current?.focus();
		};
		window.addEventListener("keydown", focus);
		return () => window.removeEventListener("keydown", focus);
	}, []);

	const enough = letters(asked) >= MIN_LETTERS;
	const search = useQuery({ ...searchQuery(asked), enabled: enough });
	const names = enough ? (search.data?.names ?? []) : [];
	const hits = enough ? (search.data?.books ?? []) : [];
	const options = names.length + hits.length;
	// Where the two last options stand; the list of every book only when
	// there is a book in it.
	const allAt = hits.length > 0 ? options : -1;
	const textAt = hits.length > 0 ? options + 1 : options;
	const shown = open && letters(words) >= MIN_LETTERS && enough;

	function close() {
		setOpen(false);
		setWords("");
		setActive(-1);
	}

	function go(hit: SearchHit) {
		close();
		navigate({ to: "/books/$bookId", params: { bookId: hit.id } });
	}

	function goToName(name: NameHit) {
		close();
		if (name.kind === "author") {
			navigate({ to: "/authors/$authorKey", params: { authorKey: name.key } });
		} else {
			navigate({ to: "/series/$seriesKey", params: { seriesKey: name.key } });
		}
	}

	function listAll() {
		close();
		navigate({ to: "/", search: { q: words.trim() } });
	}

	function searchText() {
		close();
		navigate({ to: "/search", search: { q: words.trim() } });
	}

	function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
		switch (event.key) {
			case "ArrowDown":
				event.preventDefault();
				setOpen(true);
				setActive((i) => Math.min(i + 1, textAt));
				break;
			case "ArrowUp":
				event.preventDefault();
				setActive((i) => Math.max(i - 1, -1));
				break;
			case "Enter": {
				if (!shown) break;
				event.preventDefault();
				const name = names[active];
				const hit = hits[active - names.length];
				if (name) {
					goToName(name);
				} else if (hit) {
					go(hit);
				} else if (active === allAt || (active < 0 && allAt >= 0)) {
					listAll();
				} else {
					searchText();
				}
				break;
			}
			case "Escape":
				if (shown) {
					event.preventDefault();
					setOpen(false);
					setActive(-1);
				}
				break;
		}
	}

	return (
		<div className="relative w-full sm:w-64 lg:w-72">
			<input
				ref={input}
				type="search"
				role="combobox"
				aria-label={t("search.label")}
				aria-expanded={shown}
				aria-controls={listId}
				aria-autocomplete="list"
				aria-activedescendant={
					shown && active >= 0 ? `${listId}-${active}` : undefined
				}
				placeholder={t("search.placeholder")}
				value={words}
				onChange={(e) => {
					setWords(e.target.value);
					setOpen(true);
					setActive(-1);
				}}
				onFocus={() => setOpen(true)}
				onBlur={() => setOpen(false)}
				onKeyDown={onKeyDown}
				className="w-full rounded-full border border-slate-300 bg-white py-1.5 pr-10 pl-4 text-sm text-slate-900 placeholder:text-slate-500 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100"
			/>
			{/* Decorative: Enter in the field is what searches. */}
			<img
				src={mark}
				alt=""
				width={24}
				height={24}
				className="pointer-events-none absolute top-1/2 right-2 h-6 w-6 -translate-y-1/2 rounded-full"
			/>
			<div
				hidden={!shown}
				className="absolute left-0 right-0 top-full z-20 mt-1 max-h-[calc(100dvh-6rem)] overflow-y-auto rounded-xl border sm:right-auto sm:w-[28rem] lg:w-[36rem] border-slate-200 bg-white shadow-lg dark:border-slate-700 dark:bg-slate-900"
			>
				{search.isSuccess && options === 0 && (
					<p
						role="status"
						className="px-3 py-2 text-sm text-slate-600 dark:text-slate-400"
					>
						{t("search.none")}
					</p>
				)}
				<div id={listId} role="listbox" aria-label={t("search.results")}>
					{names.map((name, i) => (
						// biome-ignore lint/a11y/useKeyWithClickEvents: the keys work on the input, which keeps the focus; this is the combobox pattern with aria-activedescendant.
						<div
							key={`${name.kind} ${name.key}`}
							id={`${listId}-${i}`}
							role="option"
							tabIndex={-1}
							aria-selected={i === active}
							onMouseDown={(e) => e.preventDefault()}
							onClick={() => goToName(name)}
							onMouseEnter={() => setActive(i)}
							className="cursor-pointer px-3 py-2 text-sm aria-selected:bg-brand-strong/10 dark:aria-selected:bg-brand/15"
						>
							<span className="line-clamp-2 font-medium">{name.name}</span>
							<span className="block truncate text-slate-600 dark:text-slate-400">
								{t(name.kind === "author" ? "search.name.author" : "search.name.series", {
									books: bookCount(name.books),
								})}
							</span>
						</div>
					))}
					{hits.map((hit, i) => (
						// biome-ignore lint/a11y/useKeyWithClickEvents: as above.
						<div
							key={hit.id}
							id={`${listId}-${names.length + i}`}
							role="option"
							tabIndex={-1}
							aria-selected={names.length + i === active}
							// Before the input loses focus, which would close the list.
							onMouseDown={(e) => e.preventDefault()}
							onClick={() => go(hit)}
							onMouseEnter={() => setActive(names.length + i)}
							className="cursor-pointer px-3 py-2 text-sm aria-selected:bg-brand-strong/10 dark:aria-selected:bg-brand/15"
						>
							<span className="line-clamp-2 font-medium">{hit.title}</span>
							{hit.authors.length > 0 && (
								<span className="block truncate text-slate-600 dark:text-slate-400">
									{hit.authors.join(", ")}
									{hit.match === "series" && hit.series && ` · ${hit.series}`}
								</span>
							)}
						</div>
					))}
					{allAt >= 0 && (
						// biome-ignore lint/a11y/useKeyWithClickEvents: as above.
						<div
							id={`${listId}-${allAt}`}
							role="option"
							tabIndex={-1}
							aria-selected={active === allAt}
							onMouseDown={(e) => e.preventDefault()}
							onClick={listAll}
							onMouseEnter={() => setActive(allAt)}
							className="cursor-pointer border-t border-slate-200 px-3 py-2 text-sm text-brand-strong aria-selected:bg-sky-100 dark:border-slate-700 dark:text-brand dark:aria-selected:bg-sky-900"
						>
							{t("search.all", { words: words.trim() })}
						</div>
					)}
					{/* biome-ignore lint/a11y/useKeyWithClickEvents: as above. */}
					<div
						id={`${listId}-${textAt}`}
						role="option"
						tabIndex={-1}
						aria-selected={active === textAt}
						onMouseDown={(e) => e.preventDefault()}
						onClick={searchText}
						onMouseEnter={() => setActive(textAt)}
						className="cursor-pointer border-t border-slate-200 px-3 py-2 text-sm text-brand-strong aria-selected:bg-sky-100 dark:border-slate-700 dark:text-brand dark:aria-selected:bg-sky-900"
					>
						{t("search.fullText", { words: words.trim() })}
					</div>
				</div>
			</div>
		</div>
	);
}
