import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, ApiError } from "../api";
import { Button, ErrorNote, Field, inputClass } from "../ui";
import { AuthShell } from "./Login";

export function AcceptInvite() {
  const { token = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.auth.acceptInvite(token, name.trim(), password);
      await queryClient.invalidateQueries();
      void navigate("/contacts", { replace: true });
    } catch (err) {
      const expired = err instanceof ApiError && (err.status === 401 || err.status === 404);
      setError(expired ? new Error("This invite link has expired or was already used. Ask a teammate for a new one.") : err);
      setBusy(false);
    }
  };

  return (
    <AuthShell title="Join your team on uncrm" subtitle="Pick a name and a password to finish setting up your account.">
      <form onSubmit={(e) => void submit(e)} className="space-y-3">
        <Field label="Your name">
          <input className={inputClass} autoComplete="name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Password">
          <input
            className={inputClass}
            type="password"
            autoComplete="new-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <ErrorNote error={error} />
        <Button type="submit" variant="primary" className="w-full" disabled={busy}>
          {busy ? "Creating account…" : "Create account"}
        </Button>
      </form>
      <p className="mt-5 text-xs text-muted">
        Already have an account?{" "}
        <Link to="/login" className="text-accent hover:underline">
          Log in
        </Link>
      </p>
    </AuthShell>
  );
}
