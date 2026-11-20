import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { api, ApiError } from "../api";
import { keys, useAuthOptions } from "../queries";
import { Button, ErrorNote, Field, Icon, inputClass, PageSpinner } from "../ui";
import { createPasskey } from "../webauthn";
import { AuthShell, OrDivider, ProviderButtons, useSignIn } from "./Login";

const used = (err: unknown) => err instanceof ApiError && err.status === 404;

export function SignInLink({ text = "Already have an account?" }: { text?: string }) {
  return (
    <p className="mt-5 text-xs text-muted">
      {text}{" "}
      <Link to="/login" className="text-fg underline underline-offset-2 hover:opacity-70">
        Sign in
      </Link>
    </p>
  );
}

export function AcceptInvite() {
  const { token = "" } = useParams();
  const invite = useQuery({ queryKey: keys.invite(token), queryFn: () => api.auth.invite(token) });
  const options = useAuthOptions();
  const signIn = useSignIn();
  const [name, setName] = useState("");
  const join = useMutation({
    mutationFn: async () => api.auth.signupFinish(await createPasskey(await api.auth.signupBegin({ invite_token: token, name: name.trim() }))),
    onSuccess: signIn,
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    join.mutate();
  };

  if (invite.isPending) {
    return (
      <AuthShell title="Join your team on Keyring">
        <PageSpinner />
      </AuthShell>
    );
  }
  if (invite.isError || used(join.error)) {
    return (
      <AuthShell title="This invite no longer works">
        {used(invite.error) || used(join.error) ? (
          <p className="text-sm text-muted">It expired or was already used. Ask a teammate for a new link from Settings, Team.</p>
        ) : (
          <ErrorNote error={invite.error} />
        )}
        <SignInLink />
      </AuthShell>
    );
  }

  const { email } = invite.data;
  const providers = options.data?.providers ?? [];
  return (
    <AuthShell
      title="Join your team on Keyring"
      subtitle={
        <>
          You&apos;re invited as <span className="font-medium text-fg [overflow-wrap:anywhere]">{email}</span>. Sign in with a passkey, no password needed.
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3">
        <Field label="Your name">
          <input className={inputClass} autoComplete="name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <ErrorNote error={join.error} />
        <Button type="submit" variant="primary" className="w-full" disabled={join.isPending}>
          <Icon name="key" className="size-3.5" />
          {join.isPending ? "Waiting for your passkey…" : "Create account with a passkey"}
        </Button>
      </form>
      {providers.length > 0 && (
        <>
          <OrDivider />
          <ProviderButtons providers={providers} start={{ intent: "invite", token }} />
          <p className="mt-2 text-xs text-muted">
            The provider must confirm <span className="[overflow-wrap:anywhere]">{email}</span> as your verified email. Any other address is refused.
          </p>
        </>
      )}
      <SignInLink />
    </AuthShell>
  );
}
