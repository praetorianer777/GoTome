import { useQuery } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import {
	safeRedirect,
	signInMethodsQuery,
	useLogin,
	useSingleSignOn,
} from "@/auth/session";
import { ssoMessage } from "@/auth/sso";
import { AuthPage } from "@/components/auth-page";
import { Field, FormError, SubmitButton } from "@/components/form";
import { t } from "@/i18n";

export function Login({ redirect, sso }: { redirect?: string; sso?: string }) {
	const login = useLogin();
	const singleSignOn = useSingleSignOn();
	const methods = useQuery(signInMethodsQuery);
	const router = useRouter();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const returnTo = safeRedirect(redirect);

	function submit(event: FormEvent) {
		event.preventDefault();
		login.mutate(
			{ username, password },
			// Pushed as an address, not as a route: where the person was going
			// may carry a query string and a fragment.
			{ onSuccess: () => router.history.push(returnTo) },
		);
	}

	const provider = methods.data?.sso;
	// Until the server has said, the form is shown: it is the way in that
	// always existed.
	const passwords = methods.data?.password ?? true;
	const problem = ssoMessage(sso);
	return (
		<AuthPage title={t("login.title")}>
			<div className="flex flex-col gap-4">
				{problem && (
					<p role="alert" className="text-sm text-red-700 dark:text-red-400">
						{problem}
					</p>
				)}
				{provider && (
					<>
						<button
							type="button"
							disabled={singleSignOn.isPending}
							onClick={() => singleSignOn.mutate({ returnTo })}
							className="rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800 disabled:opacity-60"
						>
							{t("login.sso", { name: provider.name })}
						</button>
						<FormError error={singleSignOn.error} />
					</>
				)}
				{provider && passwords && (
					<p className="text-center text-sm text-slate-500">{t("login.or")}</p>
				)}
				{passwords && (
					<form onSubmit={submit} className="flex flex-col gap-4" noValidate>
						<Field
							label={t("field.username")}
							value={username}
							onChange={(e) => setUsername(e.target.value)}
							autoComplete="username"
							autoCapitalize="none"
							spellCheck={false}
							required
							autoFocus={!provider}
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
				)}
			</div>
		</AuthPage>
	);
}
