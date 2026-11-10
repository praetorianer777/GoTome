import { type InputHTMLAttributes, type ReactNode, useId } from "react";
import { ApiError } from "@/api/client";
import { locale, type MessageKey, t } from "@/i18n";
import { button } from "@/components/ui";

interface FieldProps extends InputHTMLAttributes<HTMLInputElement> {
	label: string;
	hint?: string;
	error?: string;
	optional?: boolean;
}

export function Field({ label, hint, error, optional, ...input }: FieldProps) {
	const id = useId();
	return (
		<div className="flex flex-col gap-1">
			<label htmlFor={id} className="text-sm font-medium">
				{label}
				{optional && (
					<span className="font-normal text-slate-500 dark:text-slate-400">
						{" "}
						({t("field.optional")})
					</span>
				)}
			</label>
			<input
				id={id}
				aria-describedby={describedBy(id, hint, error)}
				aria-invalid={error ? true : undefined}
				autoComplete="off"
				className="rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 aria-invalid:border-red-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100 dark:aria-invalid:border-red-400"
				{...input}
			/>
			<Described id={id} hint={hint} error={error} />
		</div>
	);
}

function Described({
	id,
	hint,
	error,
}: {
	id: string;
	hint?: string;
	error?: string;
}) {
	return (
		<>
			{hint && !error && (
				<p
					id={`${id}-hint`}
					className="text-sm text-slate-500 dark:text-slate-400"
				>
					{hint}
				</p>
			)}
			{error && (
				<p
					id={`${id}-error`}
					className="text-sm text-red-700 dark:text-red-300"
				>
					{error}
				</p>
			)}
		</>
	);
}

function describedBy(id: string, hint?: string, error?: string) {
	return (
		[hint && !error && `${id}-hint`, error && `${id}-error`]
			.filter(Boolean)
			.join(" ") || undefined
	);
}

export interface Choice {
	value: string;
	label: string;
}

/** One of a few values, chosen from a list. */
export function SelectField({
	label,
	hint,
	error,
	value,
	choices,
	onChange,
}: {
	label: string;
	hint?: string;
	error?: string;
	value: string;
	choices: Choice[];
	onChange: (value: string) => void;
}) {
	const id = useId();
	return (
		<div className="flex flex-col gap-1">
			<label htmlFor={id} className="text-sm font-medium">
				{label}
			</label>
			<select
				id={id}
				value={value}
				onChange={(e) => onChange(e.target.value)}
				aria-describedby={describedBy(id, hint, error)}
				aria-invalid={error ? true : undefined}
				className="rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 aria-invalid:border-red-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100 dark:aria-invalid:border-red-400"
			>
				{choices.map((c) => (
					<option key={c.value} value={c.value}>
						{c.label}
					</option>
				))}
			</select>
			<Described id={id} hint={hint} error={error} />
		</div>
	);
}

/** Something that is on or off. */
export function Switch({
	label,
	hint,
	error,
	checked,
	onChange,
}: {
	label: string;
	hint?: string;
	error?: string;
	checked: boolean;
	onChange: (checked: boolean) => void;
}) {
	const id = useId();
	return (
		<div className="flex flex-col gap-1">
			<div className="flex items-center justify-between gap-3">
				<label htmlFor={id} className="text-sm font-medium">
					{label}
				</label>
				<button
					id={id}
					type="button"
					role="switch"
					aria-checked={checked}
					aria-describedby={describedBy(id, hint, error)}
					aria-invalid={error ? true : undefined}
					onClick={() => onChange(!checked)}
					className="group relative flex h-11 w-14 shrink-0 items-center justify-center"
				>
					<span
						aria-hidden="true"
						className="flex h-6 w-11 items-center rounded-full border border-slate-600 bg-slate-200 group-aria-checked:border-brand-strong group-aria-checked:bg-brand-strong dark:border-slate-600 dark:bg-slate-800 dark:group-aria-checked:border-brand dark:group-aria-checked:bg-brand"
					>
						<span className="inline-block size-4 translate-x-1 rounded-full bg-slate-600 transition-transform group-aria-checked:translate-x-6 group-aria-checked:bg-white dark:bg-slate-300 dark:group-aria-checked:bg-ink" />
					</span>
				</button>
			</div>
			<Described id={id} hint={hint} error={error} />
		</div>
	);
}

/** Any of a few values, each ticked or not; at least one stays ticked. */
export function ChoicesField({
	label,
	hint,
	error,
	value,
	choices,
	onChange,
}: {
	label: string;
	hint?: string;
	error?: string;
	value: string[];
	choices: Choice[];
	onChange: (value: string[]) => void;
}) {
	const id = useId();
	return (
		<fieldset
			aria-describedby={describedBy(id, hint, error)}
			className="flex flex-col gap-1"
		>
			<legend className="mb-1 text-sm font-medium">{label}</legend>
			<div className="flex flex-wrap gap-x-4 gap-y-1">
				{choices.map((c) => {
					const checked = value.includes(c.value);
					return (
						<label key={c.value} className="flex items-center gap-2">
							<input
								type="checkbox"
								checked={checked}
								disabled={checked && value.length === 1}
								onChange={(e) =>
									onChange(
										choices
											.map((o) => o.value)
											.filter((v) =>
												v === c.value ? e.target.checked : value.includes(v),
											),
									)
								}
							/>
							{c.label}
						</label>
					);
				})}
			</div>
			<Described id={id} hint={hint} error={error} />
		</fieldset>
	);
}

/** A page with several forms has one primary button; the others are secondary. */
export function SubmitButton({
	pending,
	children,
	kind = "primary",
}: {
	pending: boolean;
	children: ReactNode;
	kind?: "primary" | "secondary";
}) {
	return (
		<button
			type="submit"
			disabled={pending}
			className={
				kind === "primary"
					? "rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 dark:bg-brand dark:text-ink dark:hover:bg-sky-300 disabled:opacity-60"
					: button("secondary", "md")
			}
		>
			{children}
		</button>
	);
}

/** The message of a failed request, announced when it appears. */
export function FormError({ error }: { error: unknown }) {
	if (!error) {
		return null;
	}
	return (
		<p
			role="alert"
			className="rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-200"
		>
			{errorMessage(error)}
			{error instanceof ApiError && error.status >= 500 && error.requestId && (
				<span className="mt-1 block font-mono text-xs">
					{t("error.requestId", { id: error.requestId })}
				</span>
			)}
		</p>
	);
}

// The server writes its messages in English, most of them about the case
// at hand. Those that are always the same sentence are known here by their
// code or their text, so that another language can say them; the rest are
// shown as the server wrote them.
const MESSAGE_BY_CODE: Partial<Record<string, MessageKey>> = {
	validation_failed: "error.validation",
	internal: "error.internal",
	method_not_allowed: "error.generic",
};
const MESSAGE_BY_TEXT: Partial<Record<string, MessageKey>> = {
	"Sign in to continue.": "error.signIn",
	"You do not have permission to do that.": "error.forbidden",
	"There is nothing at this address.": "error.notFound",
	"The user name or the password is wrong.": "error.wrongPassword",
	"Too many failed sign-in attempts. Wait a little and try again.": "error.tooManySignIns",
};

/**
 * What to tell a person about a failed request: the server's own sentence when
 * it sent one, otherwise whether the server was reached at all.
 */
export function errorMessage(error: unknown): string {
	if (error instanceof ApiError) {
		if (locale() === "en") return error.message;
		if (error.code === "unexpected_response") {
			return t("error.unexpected", { status: error.status });
		}
		const key = MESSAGE_BY_CODE[error.code] ?? MESSAGE_BY_TEXT[error.message];
		return key ? t(key) : error.message;
	}
	if (error instanceof TypeError) {
		// fetch rejects with a TypeError when the request never got an answer.
		return t("error.unreachable");
	}
	return t("error.generic");
}

/** The server's per-field messages, when the failure was a validation. */
export function fieldErrors(error: unknown): Record<string, string> {
	return error instanceof ApiError ? error.fields : {};
}
