import { useState, type FormEvent, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router";
import { api, ApiError } from "../api";
import { Button, ErrorNote, Field, inputClass } from "../ui";

export function AuthShell({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <main className="grid min-h-full place-items-center bg-bg px-4 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center gap-2">
          <span className="grid size-7 place-items-center rounded-md bg-accent-strong text-xs font-bold text-white">u</span>
          <span className="text-[15px] font-semibold">uncrm</span>
        </div>
        <div className="rounded-xl border border-line bg-surface p-6 shadow-sm">
          <h1 className="text-[17px] font-semibold">{title}</h1>
          {subtitle && <p className="mt-1 text-[13px] text-muted">{subtitle}</p>}
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
      <details className="mt-5 text-xs text-muted">
        <summary className="cursor-pointer select-none hover:text-fg">First time here?</summary>
        <p className="mt-2 leading-relaxed">
          The first account comes from the <code className="font-mono">ADMIN_EMAIL</code> and <code className="font-mono">ADMIN_PASSWORD</code> environment
          variables on this deployment. Teammates join through an invite link from Settings, Team.
        </p>
      </details>
    </AuthShell>
  );
}
