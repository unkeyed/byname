# Sign-in

People sign in to the Byname UI with a passkey or with an account at a configured provider. A password exists only when you turn it on, and it is never enough alone. Each person has a role that decides what they can see and change. Agents and scripts sign in differently, with OAuth or API keys, and [Agents](agents.md) covers them.

## Sign-in methods

| Method | Turned on by | Second step |
|---|---|---|
| Passkey | Always on | None. The authenticator checks a fingerprint, face or PIN. |
| Google | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | An authenticator code, if the person set up an app. A passkey too, the first time the account is matched by email. |
| GitHub | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | An authenticator code, if the person set up an app. A passkey too, the first time the account is matched by email. |
| OIDC | `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME` | An authenticator code, if the person set up an app. A passkey too, the first time the account is matched by email. |
| Email and password | `PASSWORD_LOGIN=true` | Always: an authenticator code, a recovery code or a passkey |

The generic OIDC provider works with any issuer that publishes `/.well-known/openid-configuration`, such as Okta, Microsoft Entra ID, Keycloak, Authentik, WorkOS, Clerk, Auth0 and Better Auth. `OIDC_NAME` is the label on the sign-in button, for example `Okta`. It defaults to `SSO`.

Google sign-in is separate from the Search Console connection. `GOOGLE_CLIENT_ID` signs people in to Byname. `GSC_GOOGLE_CLIENT_ID` reads Search Console. See [Search engines](search.md#create-the-google-oauth-client).

## Register the callback URL

Each provider redirects back to `PUBLIC_URL/auth/oauth/<provider>/callback`, where `<provider>` is `google`, `github` or `oidc`. Register that exact URL at the provider. **Settings > Team** shows it, with a copy button, under **Sign-in methods**.

| Provider | Where to create the client | Scopes Byname asks for |
|---|---|---|
| Google | Google Cloud console, **APIs & Services > Credentials > OAuth client ID**, type **Web application** | `openid email profile` |
| GitHub | **Settings > Developer settings > OAuth Apps** | `read:user user:email` |
| OIDC | Your identity provider's application settings, type web application with a client secret | `openid email profile` |

## Roles and what they allow

Every person has one of three roles. A role grants scopes, and the same scopes apply to the API keys and MCP connections the person creates.

| Role | Scopes | Also manages |
|---|---|---|
| Admin | `results:read`, `runs:write`, `config:write` | The Treg token and provider keys, the search engine connections, the GitHub repo for site changes, where notifications go, API keys, connected apps and the team |
| Editor | `results:read`, `runs:write`, `config:write` | Nothing beyond the scopes |
| Viewer | `results:read` | Nothing beyond the scope |

The scopes mean:

| Scope | Allows |
|---|---|
| `results:read` | Read checks, answers, raw responses, questions, brands, search engine data (Google and Bing), goals, site changes, comparison pages, the repo connection, notifications and settings. Never a secret. |
| `runs:write` | Queue a check with **Check now**, and pause and resume scheduled checks. |
| `config:write` | Edit the brand, competitors, questions and question sets, AI apps and models, the schedule, spend caps and check limits, goals, branded search terms, site changes logged by hand, comparison pages, path rules, and when change results fall and what counts as a move. |

The admin-only parts are never reachable with a key or an MCP token, whatever its scopes. Only a signed-in Admin can set them, and Byname never returns a stored secret through any API. Viewers see no write actions in the UI, and the API refuses them with `403`.

An API key or MCP connection can never carry a scope its creator's role does not have. If an Admin later lowers a person's role, that person's keys and connections lose the scopes the new role lacks on their next request. The last Admin cannot be removed or lose the Admin role.

## Invite teammates

An Admin invites people under **Settings > Team** with an email and a role. Byname makes a one-time invite link, which the Admin sends. The link works for 7 days. The person opens it and registers a passkey or signs in with a configured provider.

An Admin can change anyone's role in **Settings > Team**. Inviting, changing a role and removing a teammate need a sign-in within the last 15 minutes. See [Change sign-in methods](#change-sign-in-methods).

## Who a provider can sign in

A provider sign-in never creates an account for a stranger. Byname applies these rules in order:

1. A provider account already linked to a teammate signs in that teammate.
2. Otherwise the provider must have verified the email address. For OIDC and Google that is the `email_verified` claim. For GitHub it is the primary address, and only when GitHub marks it verified.
3. A teammate with that email gets the provider account linked and is signed in. If the teammate has a passkey or an authenticator app, Byname first asks for one of them, and links the account only after it passes. A verified address proves the provider account can read that mail, not that it belongs to the teammate. For a Google account whose address is at a domain in `ALLOWED_EMAIL_DOMAINS`, the `hd` claim must name that domain here too.
4. A pending invite for that email is accepted, with the invite's role.
5. An address at a domain in `ALLOWED_EMAIL_DOMAINS` joins the team as a Viewer, or as an Editor when `ALLOWED_EMAIL_DOMAINS_ROLE=editor`, but only through a provider that proves the person belongs to the organization. For Google, the Workspace domain in the `hd` claim must match, so a personal Google account with that address does not qualify. The OIDC issuer qualifies, since you chose it. GitHub never does: a personal GitHub account keeps a verified company address after its owner leaves the company. A GitHub user at an allowed domain needs an invite, or can link GitHub in **Settings > Account** after signing in another way.
6. Anyone else is turned away with a message to ask for an invite.

An issuer that sends no `email_verified` claim, such as Microsoft Entra ID by default, cannot match a teammate by email. Teammates can still connect that provider in **Settings > Account** after they sign in another way, and rule 1 then signs them in.

## Two-factor and recovery codes

Each person can set up an authenticator app in **Settings > Account**. Byname shows a QR code and the key to type by hand, and turns the app on once it sees a valid code. It then shows 10 recovery codes once. Each recovery code works one time in place of an app code.

With an app set up, a provider sign-in and a password sign-in both ask for a code. A passkey sign-in does not, because the passkey already checked the person.

Codes follow RFC 6238: SHA-1, 6 digits, 30-second steps. Byname accepts the code for the current step and for one step either side, and accepts each step only once, so a code someone saw over your shoulder does not work again.

## Passwords

With `PASSWORD_LOGIN=true`, a person who has a passkey or an authenticator app can set a password in **Settings > Account**. After the password, Byname always asks for an app code, a recovery code or a passkey. A person with neither cannot sign in with a password.

## Change sign-in methods

Some changes need a sign-in within the last 15 minutes:

- adding or removing a passkey, the authenticator app, a password or a provider login
- creating an API key
- approving an MCP client
- inviting a teammate, changing a role or removing a teammate
- setting or deleting a provider key or the Treg token
- connecting a search engine
- adding or editing where notifications go

After 15 minutes, Byname asks the person to confirm with a passkey or an app code. A person with neither confirms through a linked provider, which Byname asks to check them again (`prompt=login`).

Byname refuses to remove a person's last way to sign in. A passkey and a linked login at a configured provider each count. A password counts only with an authenticator app and `PASSWORD_LOGIN=true`.

When a person changes or removes a password, turns the app on or off, or removes a passkey or a provider login, Byname ends their other sessions. Adding a passkey or linking a provider login does not.

## Sessions

**Settings > Account** lists each signed-in browser with its device, how it signed in, when it signed in, and when it was last used. A person can sign out any of them.

A session ends after 30 days without use. Removing a teammate in **Settings > Team** deletes their sessions, passkeys, provider logins, authenticator and the MCP connections they approved, and revokes the API keys they created. With `ALLOWED_EMAIL_DOMAINS` set, a removed person whose address is at one of those domains can join again through Google Workspace or your OIDC issuer, so take them out of your identity provider too.

Over `https`, the session cookie is named `__Host-byname_session`. Browsers accept a `__Host-` cookie only from Byname's exact host, so another site on a sibling subdomain cannot plant a session. The cookies that hold a sign-in in progress carry the same prefix.

## Failed attempts

Byname counts failed passwords per email, failed codes per person, and both per client IP address, in Postgres, so every replica sees the same count. An email or a person gets 5 failures, and an IP address gets 30, before each further failure doubles the wait, from 1 second up to 15 minutes. An hour without failures resets the count. Byname answers `429` with `Retry-After` while a wait runs. IPv6 addresses count per `/64`.

Byname takes the client IP from the TCP connection. Behind a proxy, every request comes from the proxy, so set `CLIENT_IP_HEADER` to the header your proxy writes, for example `X-Forwarded-For`. Byname reads the last address in that header, which is the one your nearest proxy added. Set it only when a proxy always overwrites or appends to that header, or clients can pick their own address.

## The setup link

While nobody can sign in, each start of Byname prints a setup link to its log. A provider login counts only while its provider is configured, and a password only with `PASSWORD_LOGIN=true`:

```text
  Byname has no account that can sign in yet. Open this link to create the first one, an admin:

    https://byname.example.com/setup/<token>
```

The person who opens the link enters a name and an email and registers a passkey, or signs in with a configured provider. They become the first user, an Admin. If a user with that email already exists, they take over that account. The link works once and stays valid until someone uses it. Every link printed before then stops working at the same moment.

Anyone who can read the log can use the link, so treat the log as you would a password. To choose the token yourself, for example on a platform where logs are awkward to read, set `SETUP_TOKEN` to 32 or more random characters, such as the output of `openssl rand -hex 32`, and open `PUBLIC_URL/setup/<SETUP_TOKEN>`. `GET PUBLIC_URL/auth/setup/<SETUP_TOKEN>` answers 200 while the link is unused.

After the first account, Byname opens onboarding.

## Recovery links

Someone who lost every way to sign in, such as their only passkey, cannot recover on their own. Whoever runs Byname can issue a recovery link:

```sh
byname recover ada@example.com
```

The link opens the setup page, which asks for the account's email and registers a new passkey. It works once and expires after an hour. Using it signs the account out of every other session, and keeps its other passkeys and logins. Remove the lost passkey afterwards under **Settings > Account**. [Self-host Byname](self-hosting.md#recover-a-locked-out-account) shows how to run the command.
