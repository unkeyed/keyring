import { createContext, useContext, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, needsReauth, oauthURL, type Account as AccountData, type SignInProvider } from "../../api";
import { keys, useAccount, useAuthOptions } from "../../queries";
import { Button, Dialog, DialogBody, ErrorNote, Field, Icon, inputClass, PageSpinner } from "../../ui";
import { assertPasskey } from "../../webauthn";

/** Asks for a fresh sign-in, then runs retry. Null outside settings, where nothing can ask. */
const Reauth = createContext<((retry?: () => void) => void) | null>(null);

export const useConfirmIdentity = () => useContext(Reauth);

/**
 * Settings changes the server guards with a recent sign-in (sign-in methods,
 * keys, providers, the team) share one "Confirm it's you" dialog.
 */
export function ReauthProvider({ children }: { children: ReactNode }) {
  const [confirming, setConfirming] = useState<{ retry?: () => void } | null>(null);
  return (
    <Reauth.Provider value={(retry) => setConfirming({ retry })}>
      {children}
      <Dialog open={confirming !== null} onClose={() => setConfirming(null)} title="Confirm it's you">
        <Confirm
          onConfirmed={() => {
            confirming?.retry?.();
            setConfirming(null);
          }}
        />
      </Dialog>
    </Reauth.Provider>
  );
}

/**
 * A guarded change. When the server wants a fresh sign-in, it asks for one
 * and then runs the same change again, so the reauth error never shows as a
 * failure.
 */
export function useReauthMutation<V = void, D = unknown>({ mutationFn, onSuccess }: { mutationFn: (v: V) => Promise<D>; onSuccess?: (d: D, v: V) => unknown }) {
  const confirm = useConfirmIdentity();
  const qc = useQueryClient();
  const m = useMutation({
    mutationFn,
    onSuccess,
    onError: (err: unknown, v: V) => {
      if (!needsReauth(err) || !confirm) return;
      void qc.invalidateQueries({ queryKey: keys.account });
      confirm(() => m.mutate(v));
    },
  });
  return { ...m, error: confirm && needsReauth(m.error) ? null : m.error };
}

function Confirm({ onConfirmed }: { onConfirmed: () => void }) {
  const account = useAccount();
  const options = useAuthOptions();
  if (account.isPending || options.isPending) return <PageSpinner />;
  if (account.isError || options.isError) {
    return (
      <DialogBody>
        <ErrorNote error={account.error ?? options.error} />
      </DialogBody>
    );
  }
  return <ConfirmBody a={account.data} providers={options.data.providers} onConfirmed={onConfirmed} />;
}

function ConfirmBody({ a, providers, onConfirmed }: { a: AccountData; providers: SignInProvider[]; onConfirmed: () => void }) {
  const qc = useQueryClient();
  const [code, setCode] = useState("");
  const done = async () => {
    await qc.invalidateQueries({ queryKey: keys.account });
    onConfirmed();
  };
  const passkey = useMutation({
    mutationFn: async () => api.account.reauthPasskey(await assertPasskey(await api.account.reauthPasskeyBegin())),
    onSuccess: done,
  });
  const withCode = useMutation({ mutationFn: () => api.account.reauthCode(code.replace(/\s/g, "")), onSuccess: done });
  // A provider may let someone back in without asking anything, so the server
  // accepts it only from people who have no passkey and no authenticator app.
  const linked = a.passkeys.length > 0 || a.totp ? [] : providers.filter((p) => a.identities.some((i) => i.provider === p.id));
  const hasMethod = a.passkeys.length > 0 || a.totp || linked.length > 0;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (code.trim()) withCode.mutate();
  };

  return (
    <DialogBody>
      <div className="space-y-4">
        <p className="text-sm text-muted">This change needs a recent sign-in. Prove it&apos;s you with any of these.</p>
        <ErrorNote error={passkey.error ?? withCode.error} />
        {a.passkeys.length > 0 && (
          <Button variant="primary" className="w-full" onClick={() => passkey.mutate()} disabled={passkey.isPending}>
            <Icon name="key" className="size-3.5" />
            {passkey.isPending ? "Waiting for your passkey…" : "Use a passkey"}
          </Button>
        )}
        {a.totp && (
          <form onSubmit={submit} className="space-y-2">
            <Field label="Code" hint="From your authenticator app, or one of your recovery codes.">
              <input className={inputClass} autoComplete="one-time-code" autoCapitalize="none" spellCheck={false} required value={code} onChange={(e) => setCode(e.target.value)} />
            </Field>
            <Button type="submit" className="w-full" disabled={withCode.isPending}>
              Confirm
            </Button>
          </form>
        )}
        {linked.length > 0 && (
          <div className="space-y-2">
            {linked.map((p) => (
              <Button key={p.id} className="w-full" onClick={() => location.assign(oauthURL(p.id, { intent: "reauth" }))}>
                Continue with {p.name}
              </Button>
            ))}
            <p className="text-xs text-muted">The provider brings you back to your account settings. Then make your change again.</p>
          </div>
        )}
        <p className="text-xs text-muted">{hasMethod ? "Or sign out and sign in again." : "Sign out and sign in again, then make your change."}</p>
      </div>
    </DialogBody>
  );
}
