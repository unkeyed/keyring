import type { AssertionStart, CeremonyFinish, RegistrationStart } from "./api";

// The server speaks WebAuthn's JSON form, with binary fields as base64url.
// Converted by hand, since PublicKeyCredential.parse*FromJSON and toJSON are
// missing from browsers people still run.

function fromB64url(s: string): ArrayBuffer {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (s.length % 4)) % 4));
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes.buffer;
}

function toB64url(buf: ArrayBuffer): string {
  let bin = "";
  for (const b of new Uint8Array(buf)) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

const descriptors = (list: PublicKeyCredentialDescriptorJSON[] | undefined): PublicKeyCredentialDescriptor[] | undefined =>
  list?.map((d) => ({ type: "public-key", id: fromB64url(d.id), transports: d.transports as AuthenticatorTransport[] | undefined }));

async function ceremony(run: () => Promise<Credential | null>): Promise<PublicKeyCredential> {
  if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys.");
  try {
    const cred = await run();
    if (!cred) throw new Error("No passkey was chosen.");
    return cred as PublicKeyCredential;
  } catch (err) {
    if (err instanceof DOMException && err.name === "NotAllowedError") throw new Error("The passkey prompt was closed or timed out. Try again.");
    if (err instanceof DOMException && err.name === "InvalidStateError") throw new Error("This device already holds a passkey for this account.");
    throw err;
  }
}

export async function assertPasskey(start: AssertionStart): Promise<CeremonyFinish> {
  const o = start.options.publicKey;
  const cred = await ceremony(() =>
    navigator.credentials.get({
      publicKey: { ...o, challenge: fromB64url(o.challenge), allowCredentials: descriptors(o.allowCredentials) } as PublicKeyCredentialRequestOptions,
    }),
  );
  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    handle: start.handle,
    credential: {
      id: cred.id,
      rawId: toB64url(cred.rawId),
      type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment,
      clientExtensionResults: cred.getClientExtensionResults(),
      response: {
        clientDataJSON: toB64url(r.clientDataJSON),
        authenticatorData: toB64url(r.authenticatorData),
        signature: toB64url(r.signature),
        userHandle: r.userHandle ? toB64url(r.userHandle) : null,
      },
    },
  };
}

export async function createPasskey(start: RegistrationStart, name?: string): Promise<CeremonyFinish> {
  const o = start.options.publicKey;
  const cred = await ceremony(() =>
    navigator.credentials.create({
      publicKey: {
        ...o,
        challenge: fromB64url(o.challenge),
        user: { ...o.user, id: fromB64url(o.user.id) },
        excludeCredentials: descriptors(o.excludeCredentials),
      } as PublicKeyCredentialCreationOptions,
    }),
  );
  const r = cred.response as AuthenticatorAttestationResponse;
  return {
    handle: start.handle,
    name: name || undefined,
    credential: {
      id: cred.id,
      rawId: toB64url(cred.rawId),
      type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment,
      clientExtensionResults: cred.getClientExtensionResults(),
      response: {
        clientDataJSON: toB64url(r.clientDataJSON),
        attestationObject: toB64url(r.attestationObject),
        transports: r.getTransports?.() ?? [],
      },
    },
  };
}
