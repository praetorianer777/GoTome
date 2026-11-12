import { type MessageKey, t } from "@/i18n";

/** What the server's `?sso=` reason means, in words. */
const REASONS: Record<string, MessageKey> = {
	off: "sso.off",
	expired: "sso.expired",
	cancelled: "sso.cancelled",
	"no-account": "sso.noAccount",
	"no-role": "sso.noRole",
	disabled: "sso.disabled",
	taken: "sso.taken",
	failed: "sso.failed",
	linked: "sso.linked",
};

/** The message for a return from the identity provider, if there is one. */
export function ssoMessage(reason: string | undefined): string | undefined {
	return reason ? t(REASONS[reason] ?? "sso.failed") : undefined;
}

/** Leaving the app for the identity provider; tests replace `to`. */
export const leave = {
	to(url: string) {
		window.location.assign(url);
	},
};
