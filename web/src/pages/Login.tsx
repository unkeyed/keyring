import { useState, type FormEvent, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router";
import { api, ApiError } from "../api";
import { Button, ErrorNote, Field, inputClass, Logo } from "../ui";

export function AuthShell({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <main className="grid min-h-full place-items-center bg-bg px-4 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center gap-2">
          <Logo className="size-7 text-sm" />
          <span className="text-base font-semibold">uncrm</span>
        </div>
        <div className="rounded-overlay border border-line bg-surface p-6 shadow-[0_1px_2px_rgb(0_0_0/0.04),0_2px_4px_-2px_rgb(0_0_0/0.05)]">
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

export function Login() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const setup = useQuery({ queryKey: ["setup"], queryFn: api.auth.setup, staleTime: Infinity });

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.auth.login(email, password);
      await queryClient.invalidateQueries();
      void navigate(safeNext(params.get("next")), { replace: true });
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? new Error("That email and password don't match.") : err);
      setBusy(false);
    }
  };

  return (
    <AuthShell title="Log in" subtitle="Every sign-up from every auth provider, in one place.">
      <form onSubmit={(e) => void submit(e)} className="space-y-3">
        <Field label="Email">
          <input className={inputClass} type="email" autoComplete="username" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label="Password">
          <input className={inputClass} type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <ErrorNote error={error} />
        <Button type="submit" variant="primary" className="w-full" disabled={busy}>
          {busy ? "Logging in…" : "Log in"}
        </Button>
      </form>
      {setup.data?.has_users === false ? (
        <p className="mt-5 rounded-control border border-line bg-subtle p-3 text-xs leading-relaxed text-muted">
          No account exists yet. Set <code className="font-mono">ADMIN_EMAIL</code> and <code className="font-mono">ADMIN_PASSWORD</code> on this deployment
          and restart it, then log in with them.
        </p>
      ) : (
        <p className="mt-5 text-xs text-muted">New here? Ask a teammate for an invite link from Settings, Team.</p>
      )}
    </AuthShell>
  );
}
