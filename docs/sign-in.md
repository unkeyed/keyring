# Sign-in

People sign in to the uncrm UI with a passkey or with an account at a configured provider. A password exists only when you turn it on, and it is never enough alone. API keys for agents and scripts are separate and unchanged; see [Configuration](configuration.md#unkey_root_key-and-unkey_api_id).

## Sign-in methods

| Method | Turned on by | Second step |
|---|---|---|
| Passkey | Always on | None. The authenticator checks a fingerprint, face, or PIN. |
| Google | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | An authenticator code, if the person set up an app. A passkey too, the first time the account is matched by email. |
| GitHub | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | An authenticator code, if the person set up an app. A passkey too, the first time the account is matched by email. |
| OIDC | `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME` | An authenticator code, if the person set up an app. A passkey too, the first time the account is matched by email. |
| Email and password | `PASSWORD_LOGIN=true` | Always: an authenticator code, a recovery code, or a passkey |

The generic OIDC provider works with any issuer that publishes `/.well-known/openid-configuration`, such as Okta, Microsoft Entra ID, Keycloak, Authentik, WorkOS, Clerk, Auth0, and Better Auth. `OIDC_NAME` is the label on the sign-in button, for example `Okta`. It defaults to `SSO`.

## Register the callback URL

Each provider redirects back to `PUBLIC_URL/auth/oauth/<provider>/callback`, where `<provider>` is `google`, `github`, or `oidc`. Register that exact URL at the provider. **Settings > Team** shows it for every configured provider.

| Provider | Where to create the client | Scopes uncrm asks for |
|---|---|---|
| Google | Google Cloud console, **APIs & Services > Credentials > OAuth client ID**, type **Web application** | `openid email profile` |
| GitHub | **Settings > Developer settings > OAuth Apps** | `read:user user:email` |
| OIDC | Your identity provider's application settings, type web application with a client secret | `openid email profile` |

## Who a provider can sign in

A provider sign-in never creates an account for a stranger. uncrm applies these rules in order:

1. A provider account already linked to a teammate signs in that teammate.
2. Otherwise the provider must have verified the email address. For OIDC and Google that is the `email_verified` claim. For GitHub it is the primary address, and only when GitHub marks it verified.
3. A teammate with that email gets the provider account linked and is signed in. If the teammate has a passkey or an authenticator app, uncrm first asks for one of them, and links the account only after it passes. A verified address proves the provider account can read that mail, not that it belongs to the teammate. For a Google account whose address is at a domain in `ALLOWED_EMAIL_DOMAINS`, the `hd` claim must name that domain here too.
4. A pending invite for that email is accepted.
5. An address at a domain in `ALLOWED_EMAIL_DOMAINS` joins the team, but only through a provider that proves the person belongs to the organization. For Google, the Workspace domain in the `hd` claim must match, so a personal Google account with that address does not qualify. The OIDC issuer qualifies, since you chose it. GitHub never does: a personal GitHub account keeps a verified company address after its owner leaves the company. A GitHub user at an allowed domain needs an invite, or can link GitHub in **Settings > Account** after signing in another way.
6. Anyone else is turned away with a message to ask for an invite.

An issuer that sends no `email_verified` claim, such as Microsoft Entra ID by default, cannot match a teammate by email. Teammates can still connect that provider in **Settings > Account** after they sign in another way, and rule 1 then signs them in.

## Sign in through Better Auth

A team that runs [Better Auth](https://www.better-auth.com) can make it uncrm's identity provider with Better Auth's OAuth 2.1 Provider plugin, which publishes OIDC discovery.

1. Add the plugin to your Better Auth server and register uncrm as a confidential client with the redirect URI `PUBLIC_URL/auth/oauth/oidc/callback`.
2. Set these variables on uncrm and restart it:

   ```sh
   OIDC_ISSUER=https://auth.example.com/api/auth
   OIDC_CLIENT_ID=<the client id>
   OIDC_CLIENT_SECRET=<the client secret>
   OIDC_NAME="Example account"
   ```

   `OIDC_ISSUER` is the issuer in your Better Auth discovery document. Check it with `curl $OIDC_ISSUER/.well-known/openid-configuration`.

uncrm matches a Better Auth user to a teammate by email only when the ID token or userinfo response carries `email_verified: true`. This is separate from the Better Auth webhook that records your product's sign-ups as contacts; see [Better Auth](providers/better-auth.md).

## Two-factor and recovery codes

Each person can set up an authenticator app in **Settings > Account**. uncrm shows a QR code and the key to type by hand, and turns the app on once it sees a valid code. It then shows 10 recovery codes once. Each recovery code works one time in place of an app code.

With an app set up, a provider sign-in and a password sign-in both ask for a code. A passkey sign-in does not, because the passkey already checked the person.

Codes follow RFC 6238: SHA-1, 6 digits, 30-second steps. uncrm accepts the code for the current step and for one step either side, and accepts each step only once, so a code someone saw over your shoulder does not work again.

## Passwords

With `PASSWORD_LOGIN=true`, a person who has a passkey or an authenticator app can set a password in **Settings > Account**. After the password, uncrm always asks for an app code, a recovery code, or a passkey. A person with neither cannot sign in with a password.

## Change sign-in methods

Adding or removing a passkey, the authenticator app, a password, or a provider login needs a sign-in within the last 15 minutes. So do creating an API key, inviting or removing a teammate, and connecting or disconnecting a webhook provider. After that, **Settings > Account** asks the person to confirm with a passkey or an app code. A person with neither confirms through a linked provider, which uncrm asks to check them again (`prompt=login`). Linking and confirming through a provider work only when started from uncrm's own pages.

uncrm refuses to remove a person's last way to sign in. A passkey and a linked login at a configured provider each count. A password counts only with an authenticator app and `PASSWORD_LOGIN=true`.

When a person changes or removes a password, turns the app on or off, or removes a passkey or a provider login, uncrm ends their other sessions. Adding a passkey or linking a provider login does not.

## Sessions

**Settings > Account** lists each signed-in browser with its device, how it signed in, when it signed in, and when it was last used. A person can sign out any of them.

A session ends after 30 days without use. Removing a teammate in **Settings > Team** deletes their sessions, passkeys, and provider logins, and revokes the API keys they created. With Unkey keys, uncrm finds a person's keys through the `created_by` metadata it writes when it creates a key, so a key created before that metadata existed stays until someone revokes it in **Settings > API keys**. The last person on the team cannot be removed. With `ALLOWED_EMAIL_DOMAINS` set, a removed person whose address is at one of those domains can join again through Google Workspace or your OIDC issuer, so take them out of your identity provider too.

Over `https`, the session cookie is named `__Host-uncrm_session`. Browsers accept a `__Host-` cookie only from uncrm's exact host, so another site on a sibling subdomain cannot plant a session. The cookies that hold a sign-in in progress carry the same prefix. An upgrade to a version that sets these names signs everyone out once.

## Failed attempts

uncrm counts failed passwords per email, failed codes per person, and both per client IP address, in Postgres, so every replica sees the same count. Attempts on one email, person, or address take turns, so a burst of parallel guesses gets no more tries than one at a time. An email or a person gets 5 failures, and an IP address gets 30, before each further failure doubles the wait, from 1 second up to 15 minutes. An hour without failures resets the count. uncrm answers `429` with `Retry-After` while a wait runs. IPv6 addresses count per `/64`.

uncrm takes the client IP from the TCP connection. Behind a proxy, every request comes from the proxy, so set `CLIENT_IP_HEADER` to the header your proxy writes, for example `X-Forwarded-For`. uncrm reads the last address in that header, which is the one your nearest proxy added. Set it only when a proxy always overwrites or appends to that header, or clients can pick their own address.

## The setup link

While nobody can sign in, each start of uncrm prints a setup link to its log. A provider login counts only while its provider is configured, and a password only with `PASSWORD_LOGIN=true`:

```text
  uncrm has no account that can sign in yet. Open this link to create the first one:

    https://crm.example.com/setup/<token>
```

That is the case on a new database. It is also the case after an upgrade from a version that seeded `ADMIN_EMAIL` and `ADMIN_PASSWORD`, since a password alone no longer signs anyone in.

The person who opens the link enters a name and an email and registers a passkey, or signs in with a configured provider. They become the first user. If a user with that email already exists, they take over that account. The link works once and stays valid until someone uses it. Every link printed before then stops working at the same moment.

Anyone who can read the log can use the link, so treat the log as you would a password. To choose the token yourself, for example on a platform where logs are awkward to read, set `SETUP_TOKEN` to 32 or more random characters, such as the output of `openssl rand -hex 32`, and open `PUBLIC_URL/setup/<SETUP_TOKEN>`.
