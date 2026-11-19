# WorkOS AuthKit

WorkOS sends signed webhooks, so setup is one endpoint and one pasted secret.

1. In uncrm, open **Settings > Providers > WorkOS AuthKit** and copy the webhook URL (`https://<your-uncrm-host>/webhooks/workos`).
2. In the [WorkOS Dashboard](https://dashboard.workos.com), switch to the environment your app uses (Staging or Production) and open **Webhooks > Create Webhook**.
3. Set **Endpoint URL** to the URL from step 1.
4. Select `user.created`, `user.updated` and `user.deleted`, then click **Create**.
5. On the webhook's page, reveal the **Secret**, copy it, and paste it into uncrm.

Each environment has its own webhooks and secrets. Repeat these steps for Production when you go live.

## What uncrm reads

| uncrm field | WorkOS field |
|---|---|
| external id | `data.id` |
| email | `data.email` |
| email verified | `data.email_verified` |
| name | `data.name`, else `first_name` + `last_name` |
| avatar | `data.profile_picture_url` |
| occurred at | the event's `created_at` |

Other events, such as `organization_membership.created`, are accepted with a 200 and ignored.

## Verification

WorkOS signs each delivery with `WorkOS-Signature: t=<ms>, v1=<hex>`, an HMAC-SHA256 of `t.body` keyed by the secret. uncrm rejects a delivery whose signature does not match or whose timestamp is more than 3 minutes old.
