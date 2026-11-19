import type { BetterAuthPlugin, User } from "better-auth";

export interface UncrmOptions {
  /** The webhook URL from uncrm's Settings > Providers > Better Auth. */
  url: string;
  /** The secret uncrm showed when you connected Better Auth (starts with whsec_). */
  secret: string;
}

type EventType = "user.created" | "user.updated" | "user.deleted";

/**
 * Sends every Better Auth user create, update and delete to uncrm, signed
 * with Standard Webhooks. A failed delivery is logged and never fails the
 * sign-up.
 */
export const uncrm = (options: UncrmOptions) => {
  const key = importKey(options.secret);

  const send = async (type: EventType, user: User) => {
    try {
      const id = `msg_${crypto.randomUUID()}`;
      const now = new Date();
      const timestamp = String(Math.floor(now.getTime() / 1000));
      // Only these fields leave your app: additionalFields may hold data
      // that does not belong in a CRM.
      const body = JSON.stringify({
        type,
        timestamp: now.toISOString(),
        data: {
          id: user.id,
          email: user.email,
          emailVerified: user.emailVerified,
          name: user.name,
          image: user.image ?? null,
          createdAt: user.createdAt,
          updatedAt: user.updatedAt,
        },
      });
      const mac = await crypto.subtle.sign("HMAC", await key, new TextEncoder().encode(`${id}.${timestamp}.${body}`));
      const res = await fetch(options.url, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          "webhook-id": id,
          "webhook-timestamp": timestamp,
          "webhook-signature": `v1,${toBase64(new Uint8Array(mac))}`,
        },
        body,
        signal: AbortSignal.timeout(5000),
      });
      if (!res.ok) {
        console.error(`[uncrm] ${type} for ${user.id}: ${res.status} ${await res.text()}`);
      }
    } catch (err) {
      console.error(`[uncrm] ${type} for ${user.id}:`, err);
    }
  };

  return {
    id: "uncrm",
    init(ctx) {
      const emit = (type: EventType) => async (user: User) => {
        await ctx.runInBackgroundOrAwait(send(type, user));
      };
      return {
        options: {
          databaseHooks: {
            user: {
              create: { after: emit("user.created") },
              update: { after: emit("user.updated") },
              delete: { after: emit("user.deleted") },
            },
          },
        },
      };
    },
  } satisfies BetterAuthPlugin;
};

function importKey(secret: string): Promise<CryptoKey> {
  const raw = Uint8Array.from(atob(secret.replace(/^whsec_/, "")), (c) => c.charCodeAt(0));
  if (raw.length === 0) {
    throw new Error("uncrm: secret is empty; copy it from uncrm's Settings > Providers > Better Auth");
  }
  return crypto.subtle.importKey("raw", raw, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
}

function toBase64(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes));
}
