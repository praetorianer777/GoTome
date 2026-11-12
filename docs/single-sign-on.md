# Single sign-on (OpenID Connect)

GOtome can let people sign in through an identity provider that speaks
OpenID Connect, such as Authentik, Authelia, Keycloak, Zitadel or
Pocket ID. Passwords keep working beside it unless you turn them off.

## At the identity provider

Register GOtome as an application (a client) with:

- **Client type**: confidential, with a client secret (a public client
  works too; GOtome always uses PKCE).
- **Redirect URI**: `https://<GOtome's address>/api/v1/auth/oidc/callback`,
  with the host name people open GOtome under. The Settings page shows it
  under **Identity provider: issuer URL**. GOtome builds it from the
  address the browser used, so behind a reverse proxy the proxy must pass
  the host name on ([getting-started.md](getting-started.md#a-reverse-proxy-and-https)).
- **Scopes**: `openid`, `profile`, `email`, and the one that brings the
  groups if you want roles from them.

Note the **issuer URL** (the address whose
`/.well-known/openid-configuration` the provider serves), the **client
ID** and the **client secret**.

## In GOtome

Administration, **Settings**, the **Identity provider** fields
([configuration.md](configuration.md#signing-in)):

1. **Issuer URL**, **client ID** and **client secret**.
2. **Name on the button**: the sign-in page then offers "Sign in with"
   that name.
3. Save. The sign-in page shows the button at once.

The provider may be on your own network: GOtome otherwise refuses to
reach private addresses, but trusts the host and port of the issuer
named here. Plain `http://` is accepted for it.

## Accounts and roles

When someone signs in through the provider for the first time, GOtome
links their account at the provider to an account here:

- With **Make accounts at first sign-in** on (the default), it makes a new
  one, with the user name (`preferred_username`) and e-mail address the
  provider sends.
- With **Find accounts by e-mail address** on, it takes the existing
  account with the address the provider says it has verified. Leave it
  off unless the provider verifies addresses.
- Otherwise, the person signs in with their password once and links the
  two on their profile, under **Single sign-on**.

Roles are given in GOtome (on the Users page) until one of **Groups of
administrators**, **Groups of editors** or **Groups of readers** is set.
From then on, the groups decide each person's role at every sign-in:
the highest role any of their groups gives, or **Role of everyone else**
(`reader`; `none` turns away anyone in none of the groups). The last
active administrator is never demoted so. The groups come from the ID
token's claim named in **groups claim** (`groups`).

## Turning passwords off

**Sign in with a password** off, once single sign-on works, leaves the
identity provider as the only way in. Two ways back stay open:

- Administrators may still use their password while none of them has
  linked an account at the provider.
- Starting GOtome with `GOTOME_FORCE_PASSWORD_LOGIN=true` lets everyone
  use their password again, for when the provider is gone. Set it in
  `.env`, `docker compose up -d`, sign in, fix the settings, and take it
  out again.

E-readers sign in with their own tokens and are not affected.

## When it fails

A sign-in that fails ends on GOtome's sign-in page with a reason:

| What it says | What to do |
|---|---|
| Single sign-on is not set up here. | Issuer and client ID are not both set. |
| That sign-in was not started here, or took too long. | Sign-ins must finish within ten minutes, in the browser that started them. Also seen when the proxy changes the host name between the start and the callback. |
| There is no account here for you. | Making accounts is off and none was found by address: an administrator makes one, and the person links it on their profile. |
| Your groups at the identity provider give you no access here. | **Role of everyone else** is `none`, and none of their groups is listed. |
| That account at the identity provider is linked to another account here. | Unlink it on the other account's profile first. |
| Signing in through the identity provider failed. | The log (`docker compose logs app`) says why: often a wrong client secret, a redirect URI the provider does not know, or an issuer URL that differs from what the provider calls itself (with or without the trailing `/`). |
