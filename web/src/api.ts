// The REST contract, mirrored from internal/httpapi/server.go (routes),
// internal/crm/types.go (shapes) and internal/auth (keys, sessions). Every
// call the UI makes goes through this file, so a contract change is a
// one-file fix here.

type Brand<T, B extends string> = T & { readonly __brand: B };

export type ContactID = Brand<number, "ContactID">;
export type CompanyID = Brand<number, "CompanyID">;
export type NoteID = Brand<number, "NoteID">;
export type KeyID = Brand<string, "KeyID">;

/** Parses a route segment like "42" into an id, or null when it is not one. */
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
export type CompanyField = "name" | "description" | "employee_range" | "industry" | "country" | "linkedin_url";
export type Field = ContactField | CompanyField;

export type ActorKind = "user" | "api_key" | "provider" | "system";
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

export interface Contact {
  id: ContactID;
  email: Email;
  name: string;
  avatar_url: string;
  title: string;
  location: string;
  bio: string;
  linkedin_url: string;
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
  | { kind: "signed_up"; data: { provider: string; external_id: string; raw?: unknown } }
  | { kind: "provider_deleted"; data: { provider: string; external_id: string } }
  | { kind: "contact_updated"; data: { reset: Field } | FieldChanges }
  | { kind: "company_updated"; data: { reset: Field } | FieldChanges }
  | { kind: "note_added"; data: { note_id: NoteID } }
  | { kind: "contacts_merged"; data: { drop_id: ContactID; drop_email: Email } }
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

export interface Company {
  id: CompanyID;
  domain: Domain;
  name: string;
  description: string;
  logo_url: string;
  employee_range: string;
  industry: string;
  country: string;
  linkedin_url: string;
  contact_count: number;
  enrichment: JobState;
  manual_fields: CompanyField[];
  created_at: Timestamp;
  updated_at: Timestamp;
}

export interface CompanyDetail extends Company {
  contacts: Contact[];
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
  since?: Timestamp;
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
  /** True: uncrm generates the secret. False: paste the provider's secret. */
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
  /** "uncrm_ab12", so people can tell keys apart. */
  prefix: string;
  created_at: Timestamp;
  last_used_at?: Timestamp;
}

/** A person who can log into the UI. */
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

/** The job state after an enrich request: usually "pending", "skipped" for free mail. */
export interface EnrichResult {
  state: JobState;
}

/** Error body for every non-2xx: {"error": "<message>"}, plus request_id on a 5xx. */
export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

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
    try {
      const body: unknown = await res.json();
      if (body && typeof body === "object" && "error" in body && typeof body.error === "string") message = body.error;
    } catch {
      // Not JSON (a proxy error page, say); keep the status text.
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

const enc = encodeURIComponent;

export const api = {
  auth: {
    /** Public. has_users is false until ADMIN_EMAIL and ADMIN_PASSWORD seed the first account. */
    setup: () => request<{ has_users: boolean }>("GET", "/auth/setup"),
    /** Sets the session cookie. */
    login: (email: string, password: string) => request<void>("POST", "/auth/login", { body: { email, password } }),
    logout: () => request<void>("POST", "/auth/logout"),
    /** Creates the user and sets the session cookie. */
    acceptInvite: (token: string, name: string, password: string) =>
      request<void>("POST", "/auth/invites/accept", { body: { token, name, password } }),
  },

  me: {
    get: () => request<Actor>("GET", "/v1/me"),
    changePassword: (old_password: string, new_password: string) =>
      request<void>("POST", "/v1/me/password", { body: { old_password, new_password } }),
  },

  signups: {
    create: (body: SignupBody) => request<IngestResult>("POST", "/v1/signups", { body }),
  },

  contacts: {
    search: (q: ContactQuery) => request<Page<Contact>>("GET", "/v1/contacts", { query: { ...q } }),
    get: (id: ContactID) => request<ContactDetail>("GET", `/v1/contacts/${id}`),
    update: (id: ContactID, patch: ContactPatch) => request<Contact>("PATCH", `/v1/contacts/${id}`, { body: patch }),
    resetField: (id: ContactID, field: ContactField) =>
      request<Contact>("DELETE", `/v1/contacts/${id}/manual_fields/${field}`),
    /** Folds drop into keep, deletes drop, and returns keep. */
    merge: (keep: ContactID, drop: ContactID) => request<Contact>("POST", `/v1/contacts/${keep}/merge`, { body: { drop } }),
    addNote: (id: ContactID, body: string) => request<Note>("POST", `/v1/contacts/${id}/notes`, { body: { body } }),
    enrich: (id: ContactID) => request<EnrichResult>("POST", `/v1/contacts/${id}/enrich`),
  },

  notes: {
    delete: (id: NoteID) => request<void>("DELETE", `/v1/notes/${id}`),
  },

  companies: {
    search: (q: CompanyQuery) => request<Page<Company>>("GET", "/v1/companies", { query: { ...q } }),
    get: (id: CompanyID) => request<CompanyDetail>("GET", `/v1/companies/${id}`),
    update: (id: CompanyID, patch: CompanyPatch) => request<Company>("PATCH", `/v1/companies/${id}`, { body: patch }),
    resetField: (id: CompanyID, field: CompanyField) =>
      request<Company>("DELETE", `/v1/companies/${id}/manual_fields/${field}`),
    enrich: (id: CompanyID) => request<EnrichResult>("POST", `/v1/companies/${id}/enrich`),
  },

  activity: {
    list: (q: ActivityQuery) => request<Activity[]>("GET", "/v1/activity", { query: { ...q } }),
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
    /** An empty secret asks uncrm to issue one; the response holds it once. */
    connect: (name: string, secret: string) =>
      request<{ secret: string }>("PUT", `/v1/providers/${enc(name)}`, { body: { secret } }),
    disconnect: (name: string) => request<void>("DELETE", `/v1/providers/${enc(name)}`),
  },

  keys: {
    list: () => request<Key[]>("GET", "/v1/keys"),
    create: (name: string) => request<CreatedKey>("POST", "/v1/keys", { body: { name } }),
    revoke: (id: KeyID) => request<void>("DELETE", `/v1/keys/${enc(id)}`),
  },

  users: {
    /** Everyone who can log in, oldest first. */
    list: () => request<User[]>("GET", "/v1/users"),
  },

  invites: {
    /** Returns a one-time token valid for 7 days; the UI builds the link. */
    create: (email: Email) => request<{ token: string }>("POST", "/v1/invites", { body: { email } }),
  },
};

/** The link a teammate opens to accept an invite (routes to AcceptInvite). */
export function inviteURL(token: string): string {
  return `${location.origin}/invite/${enc(token)}`;
}
