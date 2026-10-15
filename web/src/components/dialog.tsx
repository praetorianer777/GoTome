import { type ReactNode, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { type MessageKey, t } from "@/i18n";
import { FOCUSABLE, type KeyBinding, useModal } from "@/lib/keys";

/**
 * A modal dialog: focus moves into it and stays there, Escape or the close
 * button closes it, and focus goes back to where it was. An alert dialog
 * asks a question its own buttons answer, and has no close button. A drawer
 * is the same, along the left edge at full height, for the phone's menu.
 */
export function Dialog({
	title,
	onClose,
	children,
	alert = false,
	describedBy,
	drawer = false,
}: {
	title: string;
	onClose: () => void;
	children: ReactNode;
	alert?: boolean;
	describedBy?: string;
	drawer?: boolean;
}) {
	useModal();
	const id = useId();
	const box = useRef<HTMLDivElement>(null);
	const close = useRef(onClose);
	close.current = onClose;
	// Taken before the content's own effects can move focus into the dialog.
	const [opener] = useState(() => document.activeElement as HTMLElement | null);

	useEffect(() => {
		const el = box.current;
		if (!el) return;
		if (!el.contains(document.activeElement)) el.focus();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				e.preventDefault();
				close.current();
				return;
			}
			if (e.key !== "Tab") return;
			const items = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)];
			const first = items[0];
			const last = items.at(-1);
			if (!first || !last) {
				e.preventDefault();
				return;
			}
			const active = document.activeElement;
			if (e.shiftKey && (active === first || active === el)) {
				e.preventDefault();
				last.focus();
			} else if (!e.shiftKey && (active === last || !el.contains(active))) {
				e.preventDefault();
				first.focus();
			}
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			opener?.focus?.();
		};
	}, [opener]);

	return createPortal(
		<div
			className={`fixed inset-0 z-50 flex bg-slate-950/50 ${drawer ? "" : "items-center justify-center p-4"}`}
			onPointerDown={(e) => {
				if (drawer && e.target === e.currentTarget) close.current();
			}}
		>
			<div
				ref={box}
				role={alert ? "alertdialog" : "dialog"}
				aria-modal="true"
				aria-labelledby={id}
				aria-describedby={describedBy}
				tabIndex={-1}
				className={`flex max-h-full w-full flex-col gap-3 overflow-y-auto border-slate-200 bg-white p-4 text-slate-900 shadow-xl focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 ${drawer ? "h-full max-w-72 border-r dark:bg-slate-950" : "max-w-lg rounded-lg border"}`}
			>
				<div className="flex items-center gap-2">
					<h2 id={id} className="mr-auto text-lg font-medium">
						{title}
					</h2>
					{!alert && (
						<button
							type="button"
							onClick={onClose}
							className="rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
						>
							{t("dialog.close")}
						</button>
					)}
				</div>
				{children}
			</div>
		</div>,
		document.body,
	);
}

const KEY_NAMES: Partial<Record<string, MessageKey>> = {
	" ": "kbd.space",
	PageUp: "kbd.pageUp",
	PageDown: "kbd.pageDown",
	Home: "kbd.home",
	End: "kbd.end",
	Escape: "kbd.escape",
};

const KEY_SIGNS: Partial<Record<string, string>> = {
	ArrowLeft: "←",
	ArrowRight: "→",
	ArrowUp: "↑",
	ArrowDown: "↓",
};

function keyName(key: string): string {
	const name = KEY_NAMES[key];
	return name ? t(name) : (KEY_SIGNS[key] ?? key);
}

/** The keys a view answers and what each does, as a dialog. */
export function KeyHelp({
	bindings,
	onClose,
}: {
	bindings: KeyBinding[];
	onClose: () => void;
}) {
	return (
		<Dialog title={t("keys.title")} onClose={onClose}>
			<table className="text-sm">
				<thead className="sr-only">
					<tr>
						<th scope="col">{t("keys.key")}</th>
						<th scope="col">{t("keys.does")}</th>
					</tr>
				</thead>
				<tbody>
					{bindings.map((b) => (
						<tr
							key={b.label}
							className="border-t border-slate-200 first:border-t-0 dark:border-slate-700"
						>
							<td className="py-1 pr-4 align-top whitespace-nowrap">
								{(b.display ?? b.keys).map((key, i) => (
									<span key={key}>
										{i > 0 && " "}
										{key.startsWith("Shift+") && key.length > 6 && (
											<>
												<kbd className="rounded border border-slate-300 px-1 font-sans dark:border-slate-600">
													{t("kbd.shift")}
												</kbd>
												+
											</>
										)}
										<kbd className="rounded border border-slate-300 px-1 font-sans dark:border-slate-600">
											{keyName(key.length > 6 ? key.replace(/^Shift\+/, "") : key)}
										</kbd>
									</span>
								))}
							</td>
							<td className="py-1">{t(b.label)}</td>
						</tr>
					))}
				</tbody>
			</table>
		</Dialog>
	);
}
