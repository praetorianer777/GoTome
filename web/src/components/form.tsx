import { type InputHTMLAttributes, type ReactNode, useId } from "react";
import { ApiError } from "@/api/client";
import { t } from "@/i18n";

interface FieldProps extends InputHTMLAttributes<HTMLInputElement> {
	label: string;
	hint?: string;
	error?: string;
	optional?: boolean;
}

export function Field({ label, hint, error, optional, ...input }: FieldProps) {
	const id = useId();
	const described =
		[hint && `${id}-hint`, error && `${id}-error`].filter(Boolean).join(" ") ||
		undefined;
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
				aria-describedby={described}
				aria-invalid={error ? true : undefined}
				className="rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 aria-invalid:border-red-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100 dark:aria-invalid:border-red-400"
				{...input}
			/>
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
		</div>
	);
}

export function SubmitButton({
	pending,
	children,
}: {
	pending: boolean;
	children: ReactNode;
}) {
	return (
		<button
			type="submit"
			disabled={pending}
			className="rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 disabled:opacity-60"
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
		</p>
	);
}

/**
 * What to tell a person about a failed request: the server's own sentence when
 * it sent one, otherwise whether the server was reached at all.
 */
export function errorMessage(error: unknown): string {
	if (error instanceof ApiError) {
		return error.message;
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
