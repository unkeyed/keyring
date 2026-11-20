create extension if not exists pg_net with schema extensions;

-- The secret lives in Vault so it is not written into the function body.
do $$
begin
  perform vault.update_secret(id, '{{secret}}') from vault.secrets where name = 'keyring_webhook_secret';
  if not found then
    perform vault.create_secret('{{secret}}', 'keyring_webhook_secret');
  end if;
end $$;

create schema if not exists keyring;
revoke all on schema keyring from public, anon, authenticated;

-- Sends one auth.users row to Keyring. Only the columns listed here leave the
-- database: never to_jsonb(u), which would send the password hash and tokens.
create or replace function keyring.send_user(op text, u auth.users)
returns void
language plpgsql
security definer
set search_path = ''
as $$
begin
  perform net.http_post(
    url := '{{webhook_url}}',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'Authorization', 'Bearer ' || (select decrypted_secret from vault.decrypted_secrets where name = 'keyring_webhook_secret')
    ),
    body := jsonb_build_object(
      'type', op,
      'timestamp', case when op = 'INSERT' then u.created_at else now() end,
      'record', jsonb_build_object(
        'id', u.id,
        'email', u.email,
        'email_confirmed_at', u.email_confirmed_at,
        'is_anonymous', u.is_anonymous,
        'deleted_at', u.deleted_at,
        'created_at', u.created_at,
        'raw_user_meta_data', jsonb_build_object(
          'full_name', u.raw_user_meta_data ->> 'full_name',
          'name', u.raw_user_meta_data ->> 'name',
          'avatar_url', u.raw_user_meta_data ->> 'avatar_url',
          'picture', u.raw_user_meta_data ->> 'picture'
        )
      )
    ),
    timeout_milliseconds := 5000
  );
exception when others then
  -- Never block a sign-up because Keyring is unreachable or misconfigured.
  raise warning 'keyring: %', sqlerrm;
end;
$$;

create or replace function keyring.on_user_change()
returns trigger
language plpgsql
security definer
set search_path = ''
as $$
begin
  if tg_op = 'DELETE' then
    perform keyring.send_user(tg_op, old);
    return old;
  end if;
  perform keyring.send_user(tg_op, new);
  return new;
end;
$$;

revoke all on all functions in schema keyring from public, anon, authenticated;

drop trigger if exists keyring_user_insert_delete on auth.users;
create trigger keyring_user_insert_delete
  after insert or delete on auth.users
  for each row execute function keyring.on_user_change();

-- Sign-ins also update auth.users; only send the changes Keyring stores.
drop trigger if exists keyring_user_update on auth.users;
create trigger keyring_user_update
  after update on auth.users
  for each row
  when (
    old.email is distinct from new.email
    or old.email_confirmed_at is distinct from new.email_confirmed_at
    or old.raw_user_meta_data is distinct from new.raw_user_meta_data
    or old.deleted_at is distinct from new.deleted_at
  )
  execute function keyring.on_user_change();
