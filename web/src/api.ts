// The REST contract, mirrored from internal/httpapi/server.go (routes),
// internal/core (actors, roles and scopes) and internal/auth (keys,
// sessions).

type Brand<T, B extends string> = T & { readonly __brand: B };

export type KeyID = Brand<string, "KeyID">;

export type Email = string;

/** RFC 3339. */
export type Timestamp = string;

/** core.Scopes, in the order lists show them. */
export const scopes = ["results:read", "runs:write", "config:write"] as const;
export type Scope = (typeof scopes)[number];

export const scopeInfo: Record<Scope, { title: string; detail: string }> = {
  "results:read": { title: "Read results", detail: "Runs, answers, questions, brands, Search Console data and settings. Never secrets." },
  "runs:write": { title: "Run checks", detail: "Queue a check, and pause or resume the schedule." },
  "config:write": { title: "Change setup", detail: "Edit the brand, competitors, questions, surfaces and schedule." },
};

/** core.Roles. Admin and editor have every scope, viewer reads; only admins manage keys, apps and the team. */
export const roles = ["admin", "editor", "viewer"] as const;
export type Role = (typeof roles)[number];

export const roleInfo: Record<Role, string> = {
  admin: "Everything, including API keys, connected apps and the team",
  editor: "Reads results, runs checks and changes setup",
  viewer: "Reads results",
};

/** Who is calling. "oauth" is an MCP client acting for the person who approved it; its name reads "Claude Code (via Alex Rivera)". */
export interface Actor {
  via: "session" | "api_key" | "oauth";
  id: string;
  user_id: number;
  name: string;
  role: Role;
  /** What the caller may do now. */
  scopes: Scope[];
}

export interface Status {
  version: string;
  /** Whether someone has finished setting up the brand, surfaces and questions. */
  onboarded: boolean;
}

export interface Key {
  id: KeyID;
  name: string;
  /** "byn_ab12", so people can tell keys apart. */
  prefix: string;
  /** What it was granted. A request gets only what its creator's role still has. */
  scopes: Scope[];
  /** When it stops working. Absent for a key that works until revoked. */
  expires_at?: Timestamp;
  created_at: Timestamp;
  last_used_at?: Timestamp;
}

export interface KeyOptions {
  name: string;
  scopes: Scope[];
  expires_at?: Timestamp;
}

/** An MCP client a person approved. Its name is whatever the app registered, unverified. */
export interface ConnectedApp {
  id: string;
  name: string;
  /** The name, or else the email, of the person it acts as. */
  approved_by: string;
  scopes: Scope[];
  created_at: Timestamp;
  last_used_at?: Timestamp;
}

/** A pending OAuth authorization, for the consent page. */
export interface ConsentRequest {
  client_name: string;
  /** Where the code goes on approval. */
  redirect_uri: string;
  client_registered_at: Timestamp;
  /** The scopes to choose from: what the app asked for, or all of them. */
  scopes: Scope[];
}

/** A person who can sign into the UI. */
export interface User {
  id: number;
  email: Email;
  name: string;
  role: Role;
  created_at: Timestamp;
}

export interface Invitation {
  email: Email;
  role: Role;
}

/** POST /v1/keys: the plaintext key is returned exactly once. */
export interface CreatedKey extends Key {
  key: string;
}

export type ProviderID = "google" | "github" | "oidc";

export interface SignInProvider {
  id: ProviderID;
  name: string;
  /** The redirect URI to register at the provider. */
  callback_url: string;
}

export interface AuthOptions {
  providers: SignInProvider[];
  password_login: boolean;
  /** True while nobody can sign in; the server log prints a one-time setup link. */
  setup_required: boolean;
}

export type SecondFactor = "totp" | "recovery_code" | "passkey";

/** Every sign-in step ends in a session, or in a pending login (byname_pending cookie) that owes a second factor. */
export type SignInResponse = { status: "signed_in" } | { status: "second_factor"; second_factors: SecondFactor[] };

/** A WebAuthn ceremony: options carry binary fields as base64url. */
export interface CeremonyStart<O> {
  handle: string;
  options: { publicKey: O };
}
export type AssertionStart = CeremonyStart<PublicKeyCredentialRequestOptionsJSON>;
export type RegistrationStart = CeremonyStart<PublicKeyCredentialCreationOptionsJSON>;

export interface CeremonyFinish {
  handle: string;
  /** The PublicKeyCredential as JSON, binary fields base64url. */
  credential: object;
  name?: string;
}

export interface Passkey {
  /** The credential ID, base64url. */
  id: string;
  name: string;
  created_at: Timestamp;
  last_used_at: Timestamp | null;
}

export interface LinkedIdentity {
  provider: string;
  email: Email;
  created_at: Timestamp;
  last_used_at: Timestamp | null;
}

export interface Session {
  id: number;
  /** The factors that opened it, joined with "+": "password+totp", "github". */
  method: string;
  user_agent: string;
  created_at: Timestamp;
  last_seen_at: Timestamp;
  current: boolean;
}

export interface Account {
  user: User;
  passkeys: Passkey[];
  identities: LinkedIdentity[];
  totp: boolean;
  recovery_codes_left: number;
  password: boolean;
  /** Signed in within 15 minutes, so sign-in methods may change without confirming again. */
  recent: boolean;
  sessions: Session[];
}

export interface TOTPEnrolment {
  handle: string;
  /** Base32, for typing into an app by hand. */
  secret: string;
  uri: string;
  /** The otpauth URI as a PNG data URL. */
  qr: string;
}

/**
 * Error body for every non-2xx: {"error": "<message>"}, plus request_id on a
 * 5xx and code on a 403 that wants a fresh sign-in.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code?: "reauth_required";
  constructor(status: number, message: string, code?: "reauth_required") {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export const needsReauth = (err: unknown) => err instanceof ApiError && err.code === "reauth_required";

async function request<T>(method: string, path: string, opts: { body?: unknown } = {}): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: opts.body === undefined ? undefined : { "Content-Type": "application/json" },
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  });
  if (!res.ok) {
    let message = res.statusText || `HTTP ${res.status}`;
    let code: "reauth_required" | undefined;
    try {
      const body: unknown = await res.json();
      if (body && typeof body === "object" && "error" in body && typeof body.error === "string") message = body.error;
      if (body && typeof body === "object" && "code" in body && body.code === "reauth_required") code = body.code;
    } catch {
      // Not JSON (a proxy error page, say); keep the status text.
    }
    throw new ApiError(res.status, message, code);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

const enc = encodeURIComponent;

export const api = {
  auth: {
    options: () => request<AuthOptions>("GET", "/auth/options"),
    /** Usernameless: the passkey names the account. A passkey needs no second factor. */
    passkeyBegin: () => request<AssertionStart>("POST", "/auth/passkey/begin"),
    passkeyFinish: (body: CeremonyFinish) => request<SignInResponse>("POST", "/auth/passkey/finish", { body }),
    /** Only with PASSWORD_LOGIN on. Never a session alone: success asks for a second factor. */
    password: (email: string, password: string) => request<SignInResponse>("POST", "/auth/password", { body: { email, password } }),
    secondFactor: () => request<SignInResponse>("GET", "/auth/second-factor"),
    /** An authenticator code or a recovery code. A wrong code leaves the login pending. */
    secondFactorCode: (code: string) => request<SignInResponse>("POST", "/auth/second-factor/code", { body: { code } }),
    secondFactorPasskeyBegin: () => request<AssertionStart>("POST", "/auth/second-factor/passkey/begin"),
    secondFactorPasskeyFinish: (body: CeremonyFinish) => request<SignInResponse>("POST", "/auth/second-factor/passkey/finish", { body }),
    /** 404 once the link is used, expired, or never existed. recovery is true for a link from `byname recover`. */
    checkSetup: (token: string) => request<{ recovery: boolean }>("GET", `/auth/setup/${enc(token)}`),
    invite: (token: string) => request<Invitation>("GET", `/auth/invites/${enc(token)}`),
    signupBegin: (body: { setup_token: string; email: string; name: string } | { invite_token: string; name: string }) =>
      request<RegistrationStart>("POST", "/auth/signup/passkey/begin", { body }),
    /** Creates the account with its passkey, uses up the link, and signs in. */
    signupFinish: (body: CeremonyFinish) => request<SignInResponse>("POST", "/auth/signup/passkey/finish", { body }),
    logout: () => request<void>("POST", "/auth/logout"),
  },

  me: {
    get: () => request<Actor>("GET", "/v1/me"),
  },

  /** Every change to a sign-in method can fail with reauth_required (see needsReauth). */
  account: {
    get: () => request<Account>("GET", "/v1/account"),
    addPasskeyBegin: () => request<RegistrationStart>("POST", "/v1/account/passkeys/begin"),
    addPasskey: (body: CeremonyFinish) => request<Passkey>("POST", "/v1/account/passkeys", { body }),
    removePasskey: (id: string) => request<void>("DELETE", `/v1/account/passkeys/${enc(id)}`),
    unlink: (provider: string) => request<void>("DELETE", `/v1/account/identities/${enc(provider)}`),
    totpBegin: () => request<TOTPEnrolment>("POST", "/v1/account/totp/begin"),
    /** Returns 10 recovery codes, shown once. */
    totpFinish: (handle: string, code: string) =>
      request<{ recovery_codes: string[] }>("POST", "/v1/account/totp", { body: { handle, code } }),
    totpDisable: () => request<void>("DELETE", "/v1/account/totp"),
    regenerateRecoveryCodes: () => request<{ recovery_codes: string[] }>("POST", "/v1/account/recovery-codes"),
    setPassword: (password: string) => request<void>("PUT", "/v1/account/password", { body: { password } }),
    removePassword: () => request<void>("DELETE", "/v1/account/password"),
    revokeSession: (id: number) => request<void>("DELETE", `/v1/account/sessions/${id}`),
    reauthPasskeyBegin: () => request<AssertionStart>("POST", "/v1/account/reauth/passkey/begin"),
    reauthPasskey: (body: CeremonyFinish) => request<void>("POST", "/v1/account/reauth/passkey", { body }),
    reauthCode: (code: string) => request<void>("POST", "/v1/account/reauth/code", { body: { code } }),
  },

  status: () => request<Status>("GET", "/v1/status"),

  keys: {
    list: () => request<Key[]>("GET", "/v1/keys"),
    create: (body: KeyOptions) => request<CreatedKey>("POST", "/v1/keys", { body }),
    revoke: (id: KeyID) => request<void>("DELETE", `/v1/keys/${enc(id)}`),
  },

  apps: {
    list: () => request<ConnectedApp[]>("GET", "/v1/apps"),
    revoke: (id: string) => request<void>("DELETE", `/v1/apps/${enc(id)}`),
  },

  consent: {
    get: (handle: string) => request<ConsentRequest>("GET", `/v1/oauth/requests/${enc(handle)}`),
    /** Returns the URL to send the browser to: the app's redirect with a code, or with error=access_denied. */
    decide: (handle: string, approve: boolean, scopes: Scope[]) =>
      request<{ redirect: string }>("POST", `/v1/oauth/requests/${enc(handle)}`, { body: { approve, scopes } }),
  },

  users: {
    /** Everyone who can sign in, oldest first. */
    list: () => request<User[]>("GET", "/v1/users"),
    /** The last admin cannot step down (409). */
    setRole: (id: number, role: Role) => request<void>("PATCH", `/v1/users/${id}`, { body: { role } }),
    /** Ends their sessions too. The last admin cannot be removed (409). */
    remove: (id: number) => request<void>("DELETE", `/v1/users/${id}`),
  },

  invites: {
    /** Returns a one-time token valid for 7 days; the UI builds the link. */
    create: (email: Email, role: Role) => request<{ token: string }>("POST", "/v1/invites", { body: { email, role } }),
  },
};

export function inviteURL(token: string): string {
  return `${location.origin}/invite/${enc(token)}`;
}

export type OAuthStart = { intent: "login"; next: string } | { intent: "link" | "reauth" } | { intent: "setup" | "invite"; token: string };

/** A full-page navigation, not a fetch: the provider redirects back with ?error= on failure. */
export function oauthURL(provider: string, start: OAuthStart): string {
  return `/auth/oauth/${enc(provider)}/start?${new URLSearchParams(start)}`;
}
