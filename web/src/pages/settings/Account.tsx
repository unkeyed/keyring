import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { api, oauthURL, type Account as AccountData, type SignInProvider, type TOTPEnrolment } from "../../api";
import { keys, useAccount, useAuthOptions } from "../../queries";
import {
  Badge,
  Button,
  buttonClass,
  Card,
  CodeBlock,
  ConfirmButton,
  CopyField,
  ErrorNote,
  Field,
  Icon,
  inputClass,
  PageSpinner,
  RelativeTime,
} from "../../ui";
import { createPasskey } from "../../webauthn";
import { useConfirmIdentity, useReauthMutation } from "./Reauth";
import { SettingsHeader, SettingsSubhead } from "./Settings";

/** A change to a sign-in method, which re-reads the account after it and after a fresh sign-in. */
function useAccountMutation<V = void, D = unknown>({ mutationFn, onSuccess }: { mutationFn: (v: V) => Promise<D>; onSuccess?: (d: D) => void }) {
  const qc = useQueryClient();
  return useReauthMutation({
    mutationFn,
    onSuccess: (d: D) => {
      onSuccess?.(d);
      return qc.invalidateQueries({ queryKey: keys.account });
    },
  });
}

const factorNames: Record<string, string> = {
  passkey: "Passkey",
  password: "Password",
  totp: "Authenticator app",
  recovery_code: "Recovery code",
  setup: "Setup link",
  google: "Google",
  github: "GitHub",
  oidc: "OIDC",
};

const providerName = (id: string, providers: SignInProvider[]) => providers.find((p) => p.id === id)?.name ?? factorNames[id] ?? id;

const methodLabel = (method: string, providers: SignInProvider[]) =>
  method
    .split("+")
    .map((f) => providerName(f, providers))
    .join(" + ");

// First match wins, so the more specific tokens come first: iOS user agents
// also say "Mac OS X" and "Safari", Android and ChromeOS also say "Linux".
const browsers: [RegExp, string][] = [
  [/Edg/, "Edge"],
  [/OPR\/|Opera/, "Opera"],
  [/Firefox\/|FxiOS/, "Firefox"],
  [/Chrome\/|CriOS/, "Chrome"],
  [/Safari\//, "Safari"],
];
const systems: [RegExp, string][] = [
  [/iPhone|iPad|iPod/, "iOS"],
  [/Android/, "Android"],
  [/CrOS/, "ChromeOS"],
  [/Mac OS X|Macintosh/, "macOS"],
  [/Windows/, "Windows"],
  [/Linux/, "Linux"],
];

export function deviceName(ua: string): string {
  const browser = browsers.find(([re]) => re.test(ua))?.[1];
  const os = systems.find(([re]) => re.test(ua))?.[1];
  if (browser && os) return `${browser} on ${os}`;
  return browser ?? os ?? (ua.split(/[\s(]/)[0] || "Unknown device");
}

export function Account() {
  const account = useAccount();
  const options = useAuthOptions();
  const confirm = useConfirmIdentity();

  if (account.isPending || options.isPending) return <PageSpinner />;
  if (account.isError || options.isError) return <ErrorNote error={account.error ?? options.error} />;
  const a = account.data;
  const providers = options.data.providers;

  return (
    <>
      <SettingsHeader title="Account">
        How you sign in to uncrm. A passkey or a connected login signs you in on its own. A password always needs a second step.
      </SettingsHeader>
      <div className="space-y-3 empty:hidden">
        <UrlNotice providers={providers} />
        {!a.recent && (
          <Card className="flex flex-col gap-3 p-3 sm:flex-row sm:items-center">
            <p className="min-w-0 flex-1 text-sm text-muted">Changing how you sign in needs a sign-in from the last 15 minutes. Confirm it&apos;s you first.</p>
            <Button onClick={() => confirm?.()}>Confirm it&apos;s you</Button>
          </Card>
        )}
      </div>

      <div className="mt-6 space-y-10">
        <Passkeys a={a} />
        <TwoFactor a={a} />
        <ConnectedLogins a={a} providers={providers} />
        {options.data.password_login && <Password a={a} />}
        <Sessions a={a} providers={providers} />
      </div>

    </>
  );
}

function UrlNotice({ providers }: { providers: SignInProvider[] }) {
  const [params, setParams] = useSearchParams();
  const error = params.get("error");
  const linked = params.get("linked");
  const text = linked
    ? `Connected ${providerName(linked, providers)}. You can sign in with it now.`
    : params.has("reauth")
      ? "Confirmed. For the next 15 minutes you can change how you sign in."
      : null;
  if (!error && !text) return null;
  const dismiss = () => setParams({}, { replace: true });
  return (
    <div className="flex items-start gap-2">
      <div className="min-w-0 flex-1">
        {error ? (
          <ErrorNote error={new Error(error)} />
        ) : (
          <p role="status" className="flex items-start gap-1.5 rounded-control bg-success-soft px-2.5 py-1.5 text-xs text-success">
            <Icon name="check" className="mt-px size-3.5" />
            <span className="min-w-0 [overflow-wrap:anywhere]">{text}</span>
          </p>
        )}
      </div>
      <Button size="sm" variant="ghost" onClick={dismiss} aria-label="Dismiss" className="w-7 px-0">
        <Icon name="x" className="size-3.5" />
      </Button>
    </div>
  );
}

function Passkeys({ a }: { a: AccountData }) {
  const [name, setName] = useState("");
  const add = useAccountMutation({
    mutationFn: async (n: string) => api.account.addPasskey(await createPasskey(await api.account.addPasskeyBegin(), n)),
    onSuccess: () => setName(""),
  });
  const remove = useAccountMutation({ mutationFn: api.account.removePasskey });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    add.mutate(name.trim());
  };
  return (
    <section>
      <SettingsSubhead>Passkeys</SettingsSubhead>
      <p className="-mt-1 mb-3 text-sm text-muted">A passkey signs you in with your fingerprint, face or device PIN. Add one on each device you use.</p>
      <form onSubmit={submit} className="flex flex-col gap-2 sm:flex-row">
        <input aria-label="Passkey name" placeholder="Name it, like Work laptop (optional)" value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
        <Button type="submit" variant="primary" disabled={add.isPending}>
          <Icon name="plus" className="size-3.5" />
          {add.isPending ? "Waiting for your passkey…" : "Add passkey"}
        </Button>
      </form>
      {a.passkeys.length > 0 && (
        <Card className="mt-3">
          <ul aria-label="Passkeys" className="divide-y divide-line">
            {a.passkeys.map((p) => {
              const label = p.name || "Passkey";
              return (
                <li key={p.id} className="flex items-center gap-3 px-4 py-3">
                  <span className="grid size-8 shrink-0 place-items-center rounded-control bg-subtle text-muted">
                    <Icon name="key" className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium" title={label}>
                      {label}
                    </div>
                    <div className="text-xs text-muted">
                      Added <RelativeTime iso={p.created_at} /> · {p.last_used_at ? <>used <RelativeTime iso={p.last_used_at} /></> : "never used"}
                    </div>
                  </div>
                  <ConfirmButton label={`Remove ${label}`} confirmLabel="Remove?" onConfirm={() => remove.mutate(p.id)} disabled={remove.isPending}>
                    Remove
                  </ConfirmButton>
                </li>
              );
            })}
          </ul>
        </Card>
      )}
      <div className="mt-2 empty:hidden">
        <ErrorNote error={add.error ?? remove.error} />
      </div>
    </section>
  );
}

function TwoFactor({ a }: { a: AccountData }) {
  const [enrolment, setEnrolment] = useState<TOTPEnrolment | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const [code, setCode] = useState("");
  const begin = useAccountMutation({ mutationFn: api.account.totpBegin, onSuccess: setEnrolment });
  const finish = useAccountMutation({
    mutationFn: (e: { handle: string; code: string }) => api.account.totpFinish(e.handle, e.code),
    onSuccess: (r) => {
      setEnrolment(null);
      setCode("");
      setCodes(r.recovery_codes);
    },
  });
  const disable = useAccountMutation({ mutationFn: api.account.totpDisable });
  const regenerate = useAccountMutation({ mutationFn: api.account.regenerateRecoveryCodes, onSuccess: (r) => setCodes(r.recovery_codes) });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (enrolment && code.trim()) finish.mutate({ handle: enrolment.handle, code: code.replace(/\s/g, "") });
  };

  return (
    <section>
      <SettingsSubhead>Two-factor</SettingsSubhead>
      <p className="-mt-1 mb-3 text-sm text-muted">
        A code from an authenticator app such as 1Password or Google Authenticator. Once it is on, a password or a connected login also asks for a code.
      </p>
      {codes && (
        <div className="mb-3 space-y-2 rounded-surface border border-line-strong bg-surface p-3">
          <div className="flex items-start justify-between gap-2">
            <p className="min-w-0 text-sm">
              <span className="font-medium">Save your recovery codes.</span>{" "}
              <span className="text-muted">Each works once if you lose your authenticator app. They are shown only now.</span>
            </p>
            <Button size="sm" variant="ghost" onClick={() => setCodes(null)} className="-my-1">
              Done
            </Button>
          </div>
          <CodeBlock code={codes.join("\n")} label="Recovery codes" />
        </div>
      )}
      <Card className="p-4">
        {a.totp ? (
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="font-medium">Authenticator app</span>
                <Badge tone="success">On</Badge>
              </div>
              <div className={a.recovery_codes_left <= 3 ? "text-xs text-warn" : "text-xs text-muted"}>
                {a.recovery_codes_left === 1 ? "1 recovery code left" : `${a.recovery_codes_left} recovery codes left`}
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-1">
              <ConfirmButton label="Regenerate recovery codes" confirmLabel="Replace all codes?" onConfirm={() => regenerate.mutate()} disabled={regenerate.isPending}>
                Regenerate codes
              </ConfirmButton>
              <ConfirmButton label="Turn off two-factor" confirmLabel="Turn off?" onConfirm={() => disable.mutate()} disabled={disable.isPending}>
                Turn off
              </ConfirmButton>
            </div>
          </div>
        ) : enrolment ? (
          <form onSubmit={submit} className="space-y-4">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
              <img src={enrolment.qr} alt="QR code for your authenticator app" width={160} height={160} className="size-40 shrink-0 rounded-control border border-line" />
              <div className="min-w-0 flex-1 space-y-3">
                <p className="text-sm text-muted">Scan the code with your authenticator app, or type this key into it.</p>
                <CopyField value={enrolment.secret} label="Copy key" />
                <Field label="Code" hint="The 6-digit code the app shows now.">
                  <input
                    className={inputClass}
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    required
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                  />
                </Field>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" variant="primary" disabled={finish.isPending}>
                Turn on
              </Button>
              <Button variant="ghost" onClick={() => setEnrolment(null)}>
                Cancel
              </Button>
            </div>
          </form>
        ) : (
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <p className="min-w-0 flex-1 text-sm text-muted">Not set up.</p>
            <Button onClick={() => begin.mutate()} disabled={begin.isPending}>
              Set up authenticator app
            </Button>
          </div>
        )}
      </Card>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={begin.error ?? finish.error ?? disable.error ?? regenerate.error} />
      </div>
    </section>
  );
}

function ConnectedLogins({ a, providers }: { a: AccountData; providers: SignInProvider[] }) {
  const confirm = useConfirmIdentity();
  const unlink = useAccountMutation({ mutationFn: api.account.unlink });
  const configured = providers.map((p) => ({ id: p.id, name: p.name, identity: a.identities.find((i) => i.provider === p.id) }));
  // A login linked before its provider was unset can still be unlinked, though not used.
  const orphaned = a.identities
    .filter((i) => !providers.some((p) => p.id === i.provider))
    .map((i) => ({ id: i.provider, name: providerName(i.provider, providers), identity: i }));
  const rows = [...configured, ...orphaned];

  return (
    <section>
      <SettingsSubhead>Connected logins</SettingsSubhead>
      <p className="-mt-1 mb-3 text-sm text-muted">Sign in with a provider account whose verified email you connect here.</p>
      {rows.length === 0 ? (
        <p className="rounded-control border border-line bg-subtle p-3 text-xs leading-relaxed text-muted">
          No sign-in providers are configured on this server. Settings, Team lists the env vars that add Google, GitHub or OIDC.
        </p>
      ) : (
        <Card>
          <ul aria-label="Connected logins" className="divide-y divide-line">
            {rows.map((r) => {
              const href = oauthURL(r.id, { intent: "link" });
              return (
                <li key={r.id} className="flex items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <div className="font-medium">{r.name}</div>
                    <div className="truncate text-xs text-muted" title={r.identity?.email}>
                      {r.identity ? (
                        <>
                          {r.identity.email} · connected <RelativeTime iso={r.identity.created_at} />
                        </>
                      ) : (
                        "Not connected"
                      )}
                    </div>
                  </div>
                  {r.identity ? (
                    <ConfirmButton label={`Unlink ${r.name}`} confirmLabel="Unlink?" onConfirm={() => unlink.mutate(r.id)} disabled={unlink.isPending}>
                      Unlink
                    </ConfirmButton>
                  ) : (
                    <a
                      href={href}
                      aria-label={`Connect ${r.name}`}
                      className={buttonClass({ size: "sm" })}
                      onClick={(e) => {
                        if (a.recent) return;
                        e.preventDefault();
                        confirm?.(() => location.assign(href));
                      }}
                    >
                      Connect
                    </a>
                  )}
                </li>
              );
            })}
          </ul>
        </Card>
      )}
      <div className="mt-2 empty:hidden">
        <ErrorNote error={unlink.error} />
      </div>
    </section>
  );
}

function Password({ a }: { a: AccountData }) {
  const [password, setPassword] = useState("");
  const save = useAccountMutation({ mutationFn: (p: string) => api.account.setPassword(p), onSuccess: () => setPassword("") });
  const remove = useAccountMutation({ mutationFn: api.account.removePassword });
  const hasSecondFactor = a.totp || a.passkeys.length > 0;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(password);
  };

  return (
    <section>
      <SettingsSubhead>Password</SettingsSubhead>
      <p className="-mt-1 mb-3 text-sm text-muted">
        A password never signs you in alone. After it, uncrm asks for a code from your authenticator app or a passkey.
      </p>
      <Card className="p-4">
        {!hasSecondFactor ? (
          <p className="text-sm text-muted">Add a passkey or an authenticator app first. A password needs one of them as its second step.</p>
        ) : (
          <form onSubmit={submit} className="space-y-3">
            <input type="email" autoComplete="username" value={a.user.email} readOnly hidden />
            <Field label={a.password ? "New password" : "Password"} hint="At least 12 characters.">
              <input
                className={inputClass}
                type="password"
                autoComplete="new-password"
                minLength={12}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={save.isPending}>
                {a.password ? "Change password" : "Set password"}
              </Button>
              {a.password && (
                <ConfirmButton label="Remove password" confirmLabel="Remove password?" onConfirm={() => remove.mutate()} disabled={remove.isPending}>
                  Remove password
                </ConfirmButton>
              )}
              {save.isSuccess && <span className="text-xs text-success">Password saved.</span>}
            </div>
          </form>
        )}
      </Card>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={save.error ?? remove.error} />
      </div>
    </section>
  );
}

function Sessions({ a, providers }: { a: AccountData; providers: SignInProvider[] }) {
  const revoke = useAccountMutation({ mutationFn: api.account.revokeSession });
  const sessions = [...a.sessions].sort((x, y) => Number(y.current) - Number(x.current));
  return (
    <section>
      <SettingsSubhead>Sessions</SettingsSubhead>
      <p className="-mt-1 mb-3 text-sm text-muted">Where you are signed in. Sign out of any device you don&apos;t recognise.</p>
      <Card>
        <ul aria-label="Sessions" className="divide-y divide-line">
          {sessions.map((s) => {
            const device = deviceName(s.user_agent);
            return (
              <li key={s.id} className="flex items-center gap-3 px-4 py-3">
                <span className="grid size-8 shrink-0 place-items-center rounded-control bg-subtle text-muted">
                  <Icon name="monitor" className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="min-w-0 truncate font-medium" title={s.user_agent}>
                      {device}
                    </span>
                    {s.current && <Badge tone="info">This device</Badge>}
                  </div>
                  <div className="text-xs text-muted">
                    {methodLabel(s.method, providers)} · signed in <RelativeTime iso={s.created_at} /> · active <RelativeTime iso={s.last_seen_at} />
                  </div>
                </div>
                {!s.current && (
                  <ConfirmButton label={`Sign out ${device}`} confirmLabel="Sign out?" onConfirm={() => revoke.mutate(s.id)} disabled={revoke.isPending}>
                    Sign out
                  </ConfirmButton>
                )}
              </li>
            );
          })}
        </ul>
      </Card>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={revoke.error} />
      </div>
    </section>
  );
}
