import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { useCompleteSetup } from "@/auth/session";
import { AuthPage } from "@/components/auth-page";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { t } from "@/i18n";

export function Setup() {
	const setup = useCompleteSetup();
	const navigate = useNavigate();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const [email, setEmail] = useState("");
	const errors = fieldErrors(setup.error);

	function submit(event: FormEvent) {
		event.preventDefault();
		setup.mutate(
			{ username, password, email: email.trim() || undefined },
			{ onSuccess: () => navigate({ to: "/" }) },
		);
	}

	return (
		<AuthPage title={t("setup.title")} intro={t("setup.intro")}>
			<form onSubmit={submit} className="flex flex-col gap-4" noValidate>
				<Field
					label={t("field.username")}
					value={username}
					onChange={(e) => setUsername(e.target.value)}
					error={errors.username}
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
					error={errors.password}
					hint={t("setup.passwordHint")}
					autoComplete="new-password"
					required
				/>
				<Field
					label={t("field.email")}
					type="email"
					value={email}
					onChange={(e) => setEmail(e.target.value)}
					error={errors.email}
					autoComplete="email"
					optional
				/>
				{/* Field messages are shown at their fields; this is for the rest. */}
				{Object.keys(errors).length === 0 && <FormError error={setup.error} />}
				<SubmitButton pending={setup.isPending}>
					{setup.isPending ? t("setup.submitting") : t("setup.submit")}
				</SubmitButton>
			</form>
		</AuthPage>
	);
}
