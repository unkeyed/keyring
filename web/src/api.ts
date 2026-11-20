// The REST contract, mirrored from internal/httpapi/server.go (routes),
// internal/crm/types.go (shapes) and internal/auth (keys, sessions).

type Brand<T, B extends string> = T & { readonly __brand: B };

export type ContactID = Brand<number, "ContactID">;
export type CompanyID = Brand<number, "CompanyID">;
export type NoteID = Brand<number, "NoteID">;
export type KeyID = Brand<string, "KeyID">;

export function parseID<T extends ContactID | CompanyID>(s: string | undefined): T | null {
  const n = Number(s);
  return Number.isSafeInteger(n) && n > 0 ? (n as T) : null;
}

/** crm.Email and crm.Domain marshal as plain strings; "" is the zero value. */
export type Email = string;
export type Domain = string;
export type Tag = string;
export type Stage = string;
/** RFC 3339 timestamp, as Go's time.Time marshals. */
export type Timestamp = string;

export const jobStates = ["pending", "running", "done", "failed", "skipped"] as const;
export type JobState = (typeof jobStates)[number];

export type ContactField = "name" | "title" | "location" | "bio" | "linkedin_url" | "company";
export type CompanyField = "name" | "description" | "employee_range" | "industry" | "country" | "linkedin_url" | "tagline" | "headquarters";
export type Field = ContactField | CompanyField;

/** "app" is an MCP client acting for the person who approved it; its label reads "Claude Code (via Alex Rivera)". */
export type ActorKind = "user" | "api_key" | "app" | "provider" | "system";
export interface Actor {
  kind: ActorKind;
  id: string;
  label: string;
}

export interface CompanyRef {
  id: CompanyID;
  domain: Domain;
  /** "" until enriched or edited; show domain then. */
  name: string;
}

export interface ContactRef {
  id: ContactID;
  email: Email;
  name: string;
}

/** Profile type ("x", "github", "website", or a host name) to URL. LinkedIn is linkedin_url. */
export type Socials = Record<string, string>;

export interface Contact {
  id: ContactID;
  email: Email;
  name: string;
  avatar_url: string;
  title: string;
  location: string;
  bio: string;
  linkedin_url: string;
  socials: Socials;
  company: CompanyRef | null;
  stage: Stage;
  tags: Tag[];
  providers: string[];
  enrichment: JobState;
  manual_fields: ContactField[];
  first_seen_at: Timestamp;
  last_seen_at: Timestamp;
  created_at: Timestamp;
  updated_at: Timestamp;
}

export interface Identity {
  provider: string;
  external_id: string;
  email: Email;
  email_verified: boolean;
  name: string;
  avatar_url: string;
  occurred_at: Timestamp;
  deleted_at?: Timestamp;
}

export interface Note {
  id: NoteID;
  contact_id: ContactID;
  body: string;
  author: Actor;
  created_at: Timestamp;
}

export type FieldDiff = { from: unknown; to: unknown };
/** A tag edit is a set change, recorded under the "tags" key of contact_updated. */
export type TagChange = { added?: Tag[]; removed?: Tag[] };
/** Changed field to its diff; "tags" holds a TagChange instead. */
export type FieldChanges = { [field: string]: FieldDiff | TagChange };

/** One variant per crm.ActivityKind, with the data shape its comment documents. */
export type ActivityData =
  | { kind: "signed_up"; data: { provider: string; external_id: string } }
  | { kind: "provider_deleted"; data: { provider: string; external_id: string } }
  | { kind: "contact_updated"; data: { reset: Field } | FieldChanges }
  | { kind: "company_updated"; data: { reset: Field } | FieldChanges }
  | { kind: "note_added"; data: { note_id: NoteID } }
  | { kind: "note_deleted"; data: { note_id: NoteID } }
  | { kind: "contacts_merged"; data: { drop_id: ContactID; drop_email: Email } }
  | { kind: "contact_created"; data: Record<string, never> }
  | { kind: "company_created"; data: Record<string, never> }
  | { kind: "enriched"; data: { changed: Field[] | null } };

export type ActivityKind = ActivityData["kind"];

export type Activity = ActivityData & {
  id: number;
  contact?: ContactRef;
  company?: CompanyRef;
  actor: Actor;
  occurred_at: Timestamp;
};

export interface ContactDetail extends Contact {
  identities: Identity[];
  notes: Note[];
  /** Newest first, capped at 50. */
  activity: Activity[];
}

/**
 * One company image. A company's logos come best first: icons before
 * wordmarks, then own background, then art that reads on both themes, then
 * sharp before blurry.
 */
export interface Logo {
  url: string;
  kind: "icon" | "wordmark";
  /** The image is opaque and fills its own box. */
  own_background: boolean;
  /** The art reads on a light background. */
  on_light: boolean;
  /** The art reads on a dark background. */
  on_dark: boolean;
  /** Width over height; 0 when unknown. */
  aspect: number;
  width: number;
}

export interface Company {
  id: CompanyID;
  domain: Domain;
  name: string;
  description: string;
  /** The first of logos, for clients that show one image. */
  logo_url: string;
  employee_range: string;
  industry: string;
  country: string;
  linkedin_url: string;
  tagline: string;
  /** "South San Francisco, California"; the country is in country. */
  headquarters: string;
  socials: Socials;
  links: Partial<Record<"careers" | "pricing" | "blog" | "contact", string>>;
  /** Sub-industries, most relevant first. */
  segments: string[];
  /** "NASDAQ: XYZ", or "". */
  stock: string;
  logos: Logo[];
  contact_count: number;
  enrichment: JobState;
  manual_fields: CompanyField[];
  created_at: Timestamp;
  updated_at: Timestamp;
}

export interface CompanyDetail extends Company {
  contacts: Contact[];
  /** The company's and its contacts' entries, newest first, capped at 50. */
  activity: Activity[];
}

/** A keyset page, newest first. No next_cursor means the last page. */
export interface Page<T> {
  items: T[];
  next_cursor?: string;
}

export interface ContactQuery {
  q?: string;
  tag?: Tag;
  stage?: Stage;
  provider?: string;
  company_id?: CompanyID;
  signed_up_after?: Timestamp;
  signed_up_before?: Timestamp;
  cursor?: string;
  limit?: number;
}

export interface CompanyQuery {
  q?: string;
  cursor?: string;
  limit?: number;
}

export interface ActivityQuery {
  kind?: ActivityKind;
  contact_id?: ContactID;
  company_id?: CompanyID;
  since?: Timestamp;
  before?: Timestamp;
  cursor?: string;
  limit?: number;
}

/** Omitted fields are left alone; every present field becomes manual. */
export interface ContactPatch {
  name?: string;
  title?: string;
  location?: string;
  bio?: string;
  linkedin_url?: string;
  stage?: Stage;
  /** "" detaches the contact from its company. */
  company_domain?: Domain;
  add_tags?: Tag[];
  remove_tags?: Tag[];
}

export type CompanyPatch = Partial<Record<CompanyField, string>>;

/** POST /v1/contacts: a person added by hand. Every field given is pinned. */
export interface NewContact {
  email: Email;
  name?: string;
  title?: string;
  location?: string;
  linkedin_url?: string;
  stage?: Stage;
  tags?: Tag[];
}

export interface NewCompany {
  domain: Domain;
  name?: string;
}

export interface SignupBody {
  email: Email;
  name?: string;
  avatar_url?: string;
  email_verified?: boolean;
  source?: string;
  external_id?: string;
  occurred_at?: Timestamp;
}

export interface IngestResult {
  contact_id: ContactID;
  created: boolean;
  duplicate: boolean;
}

export interface ProviderStatus {
  name: string;
  title: string;
  /** "/webhooks/clerk"; prefix with the origin. */
  webhook_path: string;
  /** True: Keyring generates the secret. False: paste the provider's secret. */
  issue_secret: boolean;
  /** Markdown with {{webhook_url}} and {{secret}} placeholders. */
  setup: string;
  connected: boolean;
  last_delivery_at?: Timestamp;
  last_error?: string;
  last_error_at?: Timestamp;
}

export interface EnrichmentHalt {
  reason: string;
  until: Timestamp;
}

export interface Status {
  enrichment_enabled: boolean;
  enrichment_halt?: EnrichmentHalt;
  /** Every state, 0 when no job is in it. */
  jobs: Record<JobState, number>;
  contacts: number;
  companies: number;
  /** Where API keys live: Postgres, or Unkey when UNKEY_ROOT_KEY is set. */
  key_store: "builtin" | "unkey";
}

export interface Key {
  id: KeyID;
  name: string;
  /** "keyring_ab12", so people can tell keys apart. */
  prefix: string;
  created_at: Timestamp;
  last_used_at?: Timestamp;
}

/** An MCP client a person approved. Its name is whatever the app registered, unverified. */
export interface ConnectedApp {
  id: string;
  name: string;
  /** The name, or else the email, of the person it acts as. */
  approved_by: string;
  created_at: Timestamp;
  last_used_at?: Timestamp;
}

/** A pending OAuth authorization, for the consent page. */
export interface ConsentRequest {
  client_name: string;
  /** Where the code goes on approval. */
  redirect_uri: string;
  client_registered_at: Timestamp;
}

/** A person who can sign into the UI. */
export interface User {
  id: number;
  email: Email;
  name: string;
  created_at: Timestamp;
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

/** Every sign-in step ends in a session, or in a pending login (keyring_pending cookie) that owes a second factor. */
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

/** The job state after an enrich request: usually "pending", "skipped" for free mail. */
export interface EnrichResult {
  state: JobState;
}

/**
 * Error body for every non-2xx: {"error": "<message>"}, plus request_id on a
 * 5xx, code on a 403 that wants a fresh sign-in, and the existing record's id
 * on a 409 from a create.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code?: "reauth_required";
  readonly existingId?: number;
  constructor(status: number, message: string, code?: "reauth_required", existingId?: number) {
    super(message);
    this.status = status;
    this.code = code;
    this.existingId = existingId;
  }
}

export const needsReauth = (err: unknown) => err instanceof ApiError && err.code === "reauth_required";

type Query = Record<string, string | number | undefined>;

async function request<T>(method: string, path: string, opts: { query?: Query; body?: unknown } = {}): Promise<T> {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(opts.query ?? {})) {
    if (v !== undefined && v !== "") params.set(k, String(v));
  }
  const qs = params.toString() ? `?${params}` : "";
  const res = await fetch(path + qs, {
    method,
    credentials: "same-origin",
    headers: opts.body === undefined ? undefined : { "Content-Type": "application/json" },
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  });
  if (!res.ok) {
    let message = res.statusText || `HTTP ${res.status}`;
    let code: "reauth_required" | undefined;
    let existingId: number | undefined;
    try {
      const body: unknown = await res.json();
      if (body && typeof body === "object" && "error" in body && typeof body.error === "string") message = body.error;
      if (body && typeof body === "object" && "code" in body && body.code === "reauth_required") code = body.code;
      if (body && typeof body === "object" && "id" in body && typeof body.id === "number") existingId = body.id;
    } catch {
      // Not JSON (a proxy error page, say); keep the status text.
    }
    throw new ApiError(res.status, message, code, existingId);
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
    /** 404 once the link is used, or when it never existed. */
    checkSetup: (token: string) => request<void>("GET", `/auth/setup/${enc(token)}`),
    invite: (token: string) => request<{ email: Email }>("GET", `/auth/invites/${enc(token)}`),
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

  signups: {
    create: (body: SignupBody) => request<IngestResult>("POST", "/v1/signups", { body }),
  },

  contacts: {
    search: (q: ContactQuery) => request<Page<Contact>>("GET", "/v1/contacts", { query: { ...q } }),
    /** 409 with ApiError.existingId when the email is already a contact. */
    create: (body: NewContact) => request<Contact>("POST", "/v1/contacts", { body }),
    get: (id: ContactID) => request<ContactDetail>("GET", `/v1/contacts/${id}`),
    update: (id: ContactID, patch: ContactPatch) => request<Contact>("PATCH", `/v1/contacts/${id}`, { body: patch }),
    resetField: (id: ContactID, field: ContactField) =>
      request<Contact>("DELETE", `/v1/contacts/${id}/manual_fields/${field}`),
    /** Folds drop into keep, deletes drop, and returns keep. */
    merge: (keep: ContactID, drop: ContactID) => request<Contact>("POST", `/v1/contacts/${keep}/merge`, { body: { drop } }),
    addNote: (id: ContactID, body: string) => request<Note>("POST", `/v1/contacts/${id}/notes`, { body: { body } }),
    enrich: (id: ContactID) => request<EnrichResult>("POST", `/v1/contacts/${id}/enrich`),
    /** Deletes the contact with its sources, notes and tags. */
    delete: (id: ContactID) => request<void>("DELETE", `/v1/contacts/${id}`),
  },

  notes: {
    delete: (id: NoteID) => request<void>("DELETE", `/v1/notes/${id}`),
  },

  companies: {
    search: (q: CompanyQuery) => request<Page<Company>>("GET", "/v1/companies", { query: { ...q } }),
    /** 400 for a free-mail domain; 409 with ApiError.existingId when it is already a company. */
    create: (body: NewCompany) => request<Company>("POST", "/v1/companies", { body }),
    get: (id: CompanyID) => request<CompanyDetail>("GET", `/v1/companies/${id}`),
    update: (id: CompanyID, patch: CompanyPatch) => request<Company>("PATCH", `/v1/companies/${id}`, { body: patch }),
    resetField: (id: CompanyID, field: CompanyField) =>
      request<Company>("DELETE", `/v1/companies/${id}/manual_fields/${field}`),
    enrich: (id: CompanyID) => request<EnrichResult>("POST", `/v1/companies/${id}/enrich`),
    /** Deletes the company; its contacts stay, without a company. */
    delete: (id: CompanyID) => request<void>("DELETE", `/v1/companies/${id}`),
  },

  activity: {
    list: (q: ActivityQuery) => request<Page<Activity>>("GET", "/v1/activity", { query: { ...q } }),
  },

  /** Every tag in use, with its contact count. */
  tags: () => request<Record<Tag, number>>("GET", "/v1/tags"),

  stages: {
    /** In position order. */
    list: () => request<Stage[]>("GET", "/v1/stages"),
    /** Replaces the ordered list; renames maps old name to new so contacts follow. */
    set: (stages: Stage[], renames: Record<Stage, Stage>) => request<void>("PUT", "/v1/stages", { body: { stages, renames } }),
  },

  status: () => request<Status>("GET", "/v1/status"),

  providers: {
    list: () => request<ProviderStatus[]>("GET", "/v1/providers"),
    /** An empty secret asks Keyring to issue one; the response holds it once. */
    connect: (name: string, secret: string) =>
      request<{ secret: string }>("PUT", `/v1/providers/${enc(name)}`, { body: { secret } }),
    disconnect: (name: string) => request<void>("DELETE", `/v1/providers/${enc(name)}`),
  },

  keys: {
    list: () => request<Key[]>("GET", "/v1/keys"),
    create: (name: string) => request<CreatedKey>("POST", "/v1/keys", { body: { name } }),
    revoke: (id: KeyID) => request<void>("DELETE", `/v1/keys/${enc(id)}`),
  },

  apps: {
    list: () => request<ConnectedApp[]>("GET", "/v1/apps"),
    revoke: (id: string) => request<void>("DELETE", `/v1/apps/${enc(id)}`),
  },

  consent: {
    get: (handle: string) => request<ConsentRequest>("GET", `/v1/oauth/requests/${enc(handle)}`),
    /** Returns the URL to send the browser to: the app's redirect with a code, or with error=access_denied. */
    decide: (handle: string, approve: boolean) =>
      request<{ redirect: string }>("POST", `/v1/oauth/requests/${enc(handle)}`, { body: { approve } }),
  },

  users: {
    /** Everyone who can sign in, oldest first. */
    list: () => request<User[]>("GET", "/v1/users"),
    /** Ends their sessions too. The last person cannot be removed (409). */
    remove: (id: number) => request<void>("DELETE", `/v1/users/${id}`),
  },

  invites: {
    /** Returns a one-time token valid for 7 days; the UI builds the link. */
    create: (email: Email) => request<{ token: string }>("POST", "/v1/invites", { body: { email } }),
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
