-- Link application profiles to Supabase Auth without trusting role metadata
-- supplied in an access token. Existing ride ownership IDs remain stable.
alter table ride_hailing.users
    add column supabase_auth_id uuid;

create unique index users_supabase_auth_id_uidx
    on ride_hailing.users (supabase_auth_id)
    where supabase_auth_id is not null;

-- Supabase Auth now owns credentials and refresh sessions.
alter table ride_hailing.users
    drop constraint if exists users_password_hash_length;
alter table ride_hailing.users
    drop column if exists password_hash;
drop table if exists ride_hailing.sessions;
