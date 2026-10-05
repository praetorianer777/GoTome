import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { type SearchHit, searchQuery } from "@/books/api";
import { t } from "@/i18n";

/** How long typing has to pause before the search is asked. */
const DEBOUNCE_MS = 200;
/** The server finds nothing for fewer letters, so it is not asked. */
const MIN_LETTERS = 3;

function letters(words: string): number {
	return (words.match(/[\p{L}\p{N}]/gu) ?? []).length;
}

/**
 * The search box in the header: titles, authors and series, misspelt or only
 * begun, and last a way to search the text of the books for the words. It is
 * a combobox: the arrow keys move through what was found, Enter opens a book
 * or the text search, Escape closes the list, and "/" anywhere comes back to
 * it.
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
	const hits = enough ? (search.data ?? []) : [];
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

	function searchText() {
		close();
		navigate({ to: "/search", search: { q: words.trim() } });
	}

	function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
		switch (event.key) {
			case "ArrowDown":
				event.preventDefault();
				setOpen(true);
				setActive((i) => Math.min(i + 1, hits.length));
				break;
			case "ArrowUp":
				event.preventDefault();
				setActive((i) => Math.max(i - 1, -1));
				break;
			case "Enter": {
				if (!shown) break;
				event.preventDefault();
				const hit = hits[Math.max(active, 0)];
				if (hit && active < hits.length) {
					go(hit);
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
		<div className="relative w-full sm:w-64">
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
				className="w-full rounded-md border border-slate-300 bg-white px-3 py-1 text-sm text-slate-900 placeholder:text-slate-500 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100"
			/>
			<div
				hidden={!shown}
				className="absolute left-0 right-0 top-full z-20 mt-1 overflow-hidden rounded-md border border-slate-200 bg-white shadow-lg dark:border-slate-700 dark:bg-slate-900"
			>
				{search.isSuccess && hits.length === 0 && (
					<p
						role="status"
						className="px-3 py-2 text-sm text-slate-600 dark:text-slate-400"
					>
						{t("search.none")}
					</p>
				)}
				<div id={listId} role="listbox" aria-label={t("search.results")}>
					{hits.map((hit, i) => (
						// biome-ignore lint/a11y/useKeyWithClickEvents: the keys work on the input, which keeps the focus; this is the combobox pattern with aria-activedescendant.
						<div
							key={hit.id}
							id={`${listId}-${i}`}
							role="option"
							tabIndex={-1}
							aria-selected={i === active}
							// Before the input loses focus, which would close the list.
							onMouseDown={(e) => e.preventDefault()}
							onClick={() => go(hit)}
							onMouseEnter={() => setActive(i)}
							className="cursor-pointer px-3 py-2 text-sm aria-selected:bg-sky-100 dark:aria-selected:bg-sky-900"
						>
							<span className="block truncate font-medium">{hit.title}</span>
							{hit.authors.length > 0 && (
								<span className="block truncate text-slate-600 dark:text-slate-400">
									{hit.authors.join(", ")}
									{hit.match === "series" && hit.series && ` · ${hit.series}`}
								</span>
							)}
						</div>
					))}
					{/* biome-ignore lint/a11y/useKeyWithClickEvents: as above. */}
					<div
						id={`${listId}-${hits.length}`}
						role="option"
						tabIndex={-1}
						aria-selected={active === hits.length}
						onMouseDown={(e) => e.preventDefault()}
						onClick={searchText}
						onMouseEnter={() => setActive(hits.length)}
						className="cursor-pointer border-t border-slate-200 px-3 py-2 text-sm text-brand-strong aria-selected:bg-sky-100 dark:border-slate-700 dark:text-brand dark:aria-selected:bg-sky-900"
					>
						{t("search.fullText", { words: words.trim() })}
					</div>
				</div>
			</div>
		</div>
	);
}
