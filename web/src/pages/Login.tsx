import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router";
import { api, ApiError, oauthURL, type OAuthStart, type SecondFactor, type SignInProvider, type SignInResponse } from "../api";
import { keys, useAuthOptions } from "../queries";
import { Button, ErrorNote, Field, Icon, inputClass, Logo, PageSpinner } from "../ui";
import { assertPasskey } from "../webauthn";

export function AuthShell({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <main className="grid min-h-full place-items-center bg-bg px-4 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center gap-2">
          <Logo className="size-7 text-sm" />
          <span className="text-base font-semibold">Keyring</span>
        </div>
        <div className="rounded-overlay border border-line bg-surface p-6 shadow-raised">
          <h1 className="text-lg font-semibold">{title}</h1>
          {subtitle && <p className="mt-1 text-sm text-muted">{subtitle}</p>}
          <div className="mt-5">{children}</div>
        </div>
      </div>
    </main>
  );
}

/** Only same-origin paths, so ?next= cannot bounce a fresh session elsewhere. */
export function safeNext(next: string | null): string {
  return next && next.startsWith("/") && !next.startsWith("//") ? next : "/contacts";
}

export function useSignIn() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  return (r: SignInResponse) => {
    if (r.status === "signed_in") {
      // clear, not invalidate: a refetch of the pending login would fail now that it is used up.
      queryClient.clear();
      void navigate(next, { replace: true });
      return;
    }
    queryClient.setQueryData(keys.secondFactor, r);
    void navigate(`/login?second_factor=1&next=${encodeURIComponent(next)}`);
  };
}

export function ProviderButtons({ providers, start }: { providers: SignInProvider[]; start: OAuthStart }) {
  if (providers.length === 0) return null;
  return (
    <div className="space-y-2">
      {providers.map((p) => (
        <Button key={p.id} className="w-full" onClick={() => location.assign(oauthURL(p.id, start))}>
          Continue with {p.name}
        </Button>
      ))}
    </div>
  );
}

export function OrDivider() {
  return (
    <div className="my-4 flex items-center gap-3 text-xs text-faint" aria-hidden="true">
      <span className="h-px flex-1 bg-line" />
      or
      <span className="h-px flex-1 bg-line" />
    </div>
  );
}

const urlError = (params: URLSearchParams) => {
  const e = params.get("error");
  return e ? new Error(e) : null;
};

export function Login() {
  const [params, setParams] = useSearchParams();
  const queryClient = useQueryClient();
  const awaitingFactor = params.has("second_factor");
  const pending = useQuery({ queryKey: keys.secondFactor, queryFn: api.auth.secondFactor, enabled: awaitingFactor, staleTime: Infinity });

  const restart = () => {
    queryClient.removeQueries({ queryKey: keys.secondFactor });
    const next = params.get("next");
    setParams(next ? { next } : {}, { replace: true });
  };

  if (awaitingFactor && pending.isPending) {
    return (
      <AuthShell title="Sign in">
        <PageSpinner />
      </AuthShell>
    );
  }
  if (awaitingFactor && pending.data?.status === "second_factor") {
    return <SecondFactorStep factors={pending.data.second_factors} onRestart={restart} />;
  }
  return <FirstFactorStep expired={awaitingFactor && pending.isError} />;
}

function FirstFactorStep({ expired }: { expired: boolean }) {
  const [params] = useSearchParams();
  const options = useAuthOptions();
  const signIn = useSignIn();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const passkey = useMutation({
    mutationFn: async () => api.auth.passkeyFinish(await assertPasskey(await api.auth.passkeyBegin())),
    onSuccess: signIn,
  });
  const withPassword = useMutation({
    mutationFn: () => api.auth.password(email, password),
    onSuccess: signIn,
    onError: () => setPassword(""),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    withPassword.mutate();
  };
  const passwordError =
    withPassword.error instanceof ApiError && withPassword.error.status === 401 ? new Error("That email and password don't match.") : withPassword.error;
  const error = passkey.error ?? passwordError ?? (expired ? new Error("That sign-in expired. Start again.") : urlError(params));

  if (options.isPending) {
    return (
      <AuthShell title="Sign in">
        <PageSpinner />
      </AuthShell>
    );
  }
  if (options.data?.setup_required) {
    return (
      <AuthShell title="Set up Keyring" subtitle="Nobody can sign in yet.">
        <div className="space-y-3">
          <ErrorNote error={error} />
          <p className="rounded-control border border-line bg-subtle p-3 text-xs leading-relaxed text-muted">
            When Keyring starts, the server log prints a one-time setup link. Open it to create the first account with a passkey or a configured sign-in
            provider. Restart Keyring to print a fresh link.
          </p>
        </div>
      </AuthShell>
    );
  }

  const providers = options.data?.providers ?? [];
  return (
    <AuthShell title="Sign in" subtitle="Every sign-up from every auth provider, in one place.">
      <div className="space-y-3">
        <ErrorNote error={error ?? options.error} />
        <Button variant="primary" className="w-full" onClick={() => passkey.mutate()} disabled={passkey.isPending}>
          <Icon name="key" className="size-3.5" />
          {passkey.isPending ? "Waiting for your passkey…" : "Sign in with passkey"}
        </Button>
        <ProviderButtons providers={providers} start={{ intent: "login", next: safeNext(params.get("next")) }} />
      </div>
      {options.data?.password_login && (
        <>
          <OrDivider />
          <form onSubmit={submit} className="space-y-3">
            <Field label="Email">
              <input className={inputClass} type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
            <Field label="Password">
              <input className={inputClass} type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            </Field>
            <Button type="submit" className="w-full" disabled={withPassword.isPending}>
              {withPassword.isPending ? "Checking…" : "Sign in with password"}
            </Button>
            <p className="text-xs text-muted">A password always needs a second step: a code from your authenticator app or a passkey.</p>
          </form>
        </>
      )}
      <p className="mt-5 text-xs text-muted">New here? Ask a teammate for an invite link from Settings, Team.</p>
    </AuthShell>
  );
}

function SecondFactorStep({ factors, onRestart }: { factors: SecondFactor[]; onRestart: () => void }) {
  const signIn = useSignIn();
  const [code, setCode] = useState("");
  const takesCode = factors.includes("totp") || factors.includes("recovery_code");
  const takesPasskey = factors.includes("passkey");
  const withCode = useMutation({
    mutationFn: () => api.auth.secondFactorCode(code.replace(/\s/g, "")),
    onSuccess: signIn,
  });
  const passkey = useMutation({
    mutationFn: async () => api.auth.secondFactorPasskeyFinish(await assertPasskey(await api.auth.secondFactorPasskeyBegin())),
    onSuccess: signIn,
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (code.trim()) withCode.mutate();
  };

  return (
    <AuthShell
      title="Confirm it's you"
      subtitle={
        takesCode
          ? "Enter the 6-digit code from your authenticator app, or one of your recovery codes."
          : "Use one of your passkeys to finish signing in."
      }
    >
      <div className="space-y-3">
        <ErrorNote error={withCode.error ?? passkey.error} />
        {takesCode && (
          <form onSubmit={submit} className="space-y-3">
            <Field label="Code">
              <input
                className={inputClass}
                autoComplete="one-time-code"
                autoCapitalize="none"
                spellCheck={false}
                required
                autoFocus
                placeholder="123456 or abcd-efgh-ijkl-mnop"
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            </Field>
            <Button type="submit" variant="primary" className="w-full" disabled={withCode.isPending}>
              {withCode.isPending ? "Checking…" : "Verify"}
            </Button>
          </form>
        )}
        {takesCode && takesPasskey && <OrDivider />}
        {takesPasskey && (
          <Button variant={takesCode ? "secondary" : "primary"} className="w-full" onClick={() => passkey.mutate()} disabled={passkey.isPending}>
            <Icon name="key" className="size-3.5" />
            {passkey.isPending ? "Waiting for your passkey…" : "Use a passkey"}
          </Button>
        )}
      </div>
      <p className="mt-5 text-xs text-muted">
        Wrong account?{" "}
        <button type="button" onClick={onRestart} className="text-fg underline underline-offset-2 hover:opacity-70">
          Start over
        </button>
      </p>
    </AuthShell>
  );
}
