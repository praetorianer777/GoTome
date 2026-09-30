import { useRouter } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { safeRedirect, useLogin } from "@/auth/session";
import { AuthPage } from "@/components/auth-page";
import { Field, FormError, SubmitButton } from "@/components/form";
import { t } from "@/i18n";

export function Login({ redirect }: { redirect?: string }) {
	const login = useLogin();
	const router = useRouter();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");

	function submit(event: FormEvent) {
		event.preventDefault();
		login.mutate(
			{ username, password },
			// Pushed as an address, not as a route: where the person was going
			// may carry a query string and a fragment.
			{ onSuccess: () => router.history.push(safeRedirect(redirect)) },
		);
	}

	return (
		<AuthPage title={t("login.title")}>
			<form onSubmit={submit} className="flex flex-col gap-4" noValidate>
				<Field
					label={t("field.username")}
					value={username}
					onChange={(e) => setUsername(e.target.value)}
					autoComplete="username"
					autoCapitalize="none"
					spellCheck={false}
					required
					autoFocus
				/>
				<Field
					label={t("field.password")}
					type="password"
					value={password}
					onChange={(e) => setPassword(e.target.value)}
					autoComplete="current-password"
					required
				/>
				<FormError error={login.error} />
				<SubmitButton pending={login.isPending}>
					{login.isPending ? t("login.submitting") : t("login.submit")}
				</SubmitButton>
			</form>
		</AuthPage>
	);
}
