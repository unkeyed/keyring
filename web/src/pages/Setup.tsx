import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useParams } from "react-router";
import { api, ApiError } from "../api";
import { keys, useAuthOptions } from "../queries";
import { Button, ErrorNote, Field, Icon, inputClass, PageSpinner } from "../ui";
import { createPasskey } from "../webauthn";
import { SignInLink } from "./AcceptInvite";
import { AuthShell, OrDivider, ProviderButtons, useSignIn } from "./Login";

const used = (err: unknown) => err instanceof ApiError && err.status === 404;

export function Setup() {
  const { token = "" } = useParams();
  const check = useQuery({
    queryKey: keys.setup(token),
    queryFn: async () => {
      await api.auth.checkSetup(token);
      return true;
    },
  });
  const options = useAuthOptions();
  const signIn = useSignIn();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const create = useMutation({
    mutationFn: async () =>
      api.auth.signupFinish(await createPasskey(await api.auth.signupBegin({ setup_token: token, email: email.trim(), name: name.trim() }))),
    onSuccess: signIn,
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };

  if (check.isPending) {
    return (
      <AuthShell title="Set up uncrm">
        <PageSpinner />
      </AuthShell>
    );
  }
  if (check.isError || used(create.error)) {
    return (
      <AuthShell title="This setup link no longer works">
        {used(check.error) || used(create.error) ? (
          <p className="text-sm text-muted">
            It was already used, or it is not the latest one. If nobody can sign in yet, restart uncrm and open the new link from the server log.
          </p>
        ) : (
          <ErrorNote error={check.error} />
        )}
        <SignInLink text="Someone already set it up?" />
      </AuthShell>
    );
  }

  const providers = options.data?.providers ?? [];
  return (
    <AuthShell title="Set up uncrm" subtitle="Create the first account. You sign in with a passkey, so there is no password to choose.">
      <form onSubmit={submit} className="space-y-3">
        <Field label="Your name">
          <input className={inputClass} autoComplete="name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Email">
          <input className={inputClass} type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <ErrorNote error={create.error} />
        <Button type="submit" variant="primary" className="w-full" disabled={create.isPending}>
          <Icon name="key" className="size-3.5" />
          {create.isPending ? "Waiting for your passkey…" : "Create account with a passkey"}
        </Button>
      </form>
      {providers.length > 0 && (
        <>
          <OrDivider />
          <ProviderButtons providers={providers} start={{ intent: "setup", token }} />
          <p className="mt-2 text-xs text-muted">Your account takes the email the provider confirms.</p>
        </>
      )}
    </AuthShell>
  );
}
