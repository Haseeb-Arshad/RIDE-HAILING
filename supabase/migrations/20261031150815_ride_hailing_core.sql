-- The checked-in Supabase config omits ride_hailing from exposed schemas;
-- keep the remote project setting aligned before applying this migration.
-- Go is the only business API; Row Level Security is enabled as defense in depth.
create schema if not exists ride_hailing;
revoke all on schema ride_hailing from public;
revoke all on schema ride_hailing from anon, authenticated, service_role;

create type ride_hailing.user_role as enum ('rider', 'driver');
create type ride_hailing.ride_category as enum ('economy', 'comfort');
create type ride_hailing.ride_status as enum ('requested', 'accepted', 'completed', 'cancelled');
create type ride_hailing.driver_status as enum ('offline', 'available', 'busy');
create type ride_hailing.assignment_status as enum ('active', 'completed', 'cancelled');

create table ride_hailing.users (
    id uuid primary key default gen_random_uuid(),
    email text not null,
    display_name text not null,
    role ride_hailing.user_role not null,
    password_hash text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint users_email_normalized check (email = lower(btrim(email))),
    constraint users_email_shape check (email ~ '^[^@[:space:]]+@[^@[:space:]]+$'),
    constraint users_display_name_length check (length(btrim(display_name)) between 1 and 80),
    constraint users_password_hash_length check (length(password_hash) between 1 and 255)
);

create unique index users_email_lower_uidx on ride_hailing.users (lower(email));

create table ride_hailing.locations (
    id uuid primary key default gen_random_uuid(),
    name text not null,
    area text not null,
    city text not null default 'Lahore',
    latitude double precision not null,
    longitude double precision not null,
    active boolean not null default true,
    created_at timestamptz not null default now(),
    constraint locations_city_lahore check (city = 'Lahore'),
    constraint locations_name_length check (length(btrim(name)) between 1 and 120),
    constraint locations_area_length check (length(btrim(area)) between 1 and 120),
    constraint locations_latitude_range check (latitude between -90 and 90),
    constraint locations_longitude_range check (longitude between -180 and 180)
);

create index locations_city_active_name_idx
    on ride_hailing.locations (city, name)
    where active;

create table ride_hailing.driver_profiles (
    user_id uuid primary key references ride_hailing.users(id) on delete restrict,
    status ride_hailing.driver_status not null default 'offline',
    city text not null default 'Lahore',
    demo_latitude double precision not null,
    demo_longitude double precision not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint driver_profiles_city_lahore check (city = 'Lahore'),
    constraint driver_profiles_latitude_range check (demo_latitude between -90 and 90),
    constraint driver_profiles_longitude_range check (demo_longitude between -180 and 180)
);

create index driver_profiles_available_city_idx
    on ride_hailing.driver_profiles (city, user_id)
    where status = 'available';

create table ride_hailing.driver_categories (
    driver_id uuid not null references ride_hailing.driver_profiles(user_id) on delete cascade,
    category ride_hailing.ride_category not null,
    created_at timestamptz not null default now(),
    primary key (driver_id, category)
);

create index driver_categories_category_driver_idx
    on ride_hailing.driver_categories (category, driver_id);

create table ride_hailing.rides (
    id uuid primary key default gen_random_uuid(),
    rider_id uuid not null references ride_hailing.users(id) on delete restrict,
    pickup_location_id uuid not null references ride_hailing.locations(id) on delete restrict,
    destination_location_id uuid not null references ride_hailing.locations(id) on delete restrict,
    category ride_hailing.ride_category not null,
    estimated_fare_paisa bigint not null,
    estimated_distance_meters bigint not null,
    pickup_eta_minutes integer,
    currency text not null default 'PKR',
    approximate boolean not null default true,
    status ride_hailing.ride_status not null default 'requested',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    accepted_at timestamptz,
    cancelled_at timestamptz,
    completed_at timestamptz,
    constraint rides_rider_id_id_key unique (rider_id, id),
    constraint rides_distinct_locations check (pickup_location_id <> destination_location_id),
    constraint rides_nonnegative_fare check (estimated_fare_paisa >= 0),
    constraint rides_positive_distance check (estimated_distance_meters > 0),
    constraint rides_valid_eta check (pickup_eta_minutes is null or pickup_eta_minutes >= 2),
    constraint rides_currency_pkr check (currency = 'PKR'),
    constraint rides_approximate check (approximate),
    constraint rides_lifecycle_timestamps check (
        (status = 'requested' and accepted_at is null and cancelled_at is null and completed_at is null)
        or (status = 'accepted' and accepted_at is not null and cancelled_at is null and completed_at is null)
        or (status = 'completed' and accepted_at is not null and cancelled_at is null and completed_at is not null)
        or (status = 'cancelled' and cancelled_at is not null and completed_at is null)
    )
);

create unique index rides_one_open_per_rider_uidx
    on ride_hailing.rides (rider_id)
    where status in ('requested', 'accepted');

create index rides_requested_order_idx
    on ride_hailing.rides (category, created_at, id)
    where status = 'requested';

create index rides_pickup_location_idx on ride_hailing.rides (pickup_location_id);
create index rides_destination_location_idx on ride_hailing.rides (destination_location_id);

create table ride_hailing.ride_assignments (
    id uuid primary key default gen_random_uuid(),
    ride_id uuid not null unique references ride_hailing.rides(id) on delete restrict,
    driver_id uuid not null references ride_hailing.driver_profiles(user_id) on delete restrict,
    status ride_hailing.assignment_status not null default 'active',
    assigned_at timestamptz not null default now(),
    ended_at timestamptz,
    constraint ride_assignments_closed_state check (
        (status = 'active' and ended_at is null)
        or (status in ('completed', 'cancelled') and ended_at is not null)
    )
);

create unique index ride_assignments_one_active_per_driver_uidx
    on ride_hailing.ride_assignments (driver_id)
    where status = 'active';

create index ride_assignments_driver_history_idx
    on ride_hailing.ride_assignments (driver_id, assigned_at desc);

create table ride_hailing.idempotency_keys (
    rider_id uuid not null references ride_hailing.users(id) on delete restrict,
    idempotency_key text not null,
    request_hash bytea not null,
    ride_id uuid not null,
    created_at timestamptz not null default now(),
    constraint idempotency_keys_ride_owner_fk
        foreign key (rider_id, ride_id)
        references ride_hailing.rides(rider_id, id) on delete restrict,
    primary key (rider_id, idempotency_key),
    constraint idempotency_key_length check (length(idempotency_key) between 1 and 128),
    constraint idempotency_key_ascii check (idempotency_key ~ '^[!-~]+$'),
    constraint idempotency_request_hash_sha256 check (octet_length(request_hash) = 32)
);

create index idempotency_keys_ride_idx on ride_hailing.idempotency_keys (ride_id);

create table ride_hailing.sessions (
    token_hash bytea primary key,
    user_id uuid not null references ride_hailing.users(id) on delete cascade,
    csrf_token_hash bytea not null,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    revoked_at timestamptz,
    constraint sessions_token_hash_sha256 check (octet_length(token_hash) = 32),
    constraint sessions_csrf_hash_sha256 check (octet_length(csrf_token_hash) = 32),
    constraint sessions_expiry_after_creation check (expires_at > created_at)
);

create index sessions_active_user_expiry_idx
    on ride_hailing.sessions (user_id, expires_at)
    where revoked_at is null;

-- No Data API grants or policies are created for these tables. The trusted Go
-- service connects server-side and enforces the application session/ownership model.
revoke all privileges on all tables in schema ride_hailing
    from public, anon, authenticated, service_role;

alter table ride_hailing.users enable row level security;
alter table ride_hailing.users force row level security;
alter table ride_hailing.locations enable row level security;
alter table ride_hailing.locations force row level security;
alter table ride_hailing.driver_profiles enable row level security;
alter table ride_hailing.driver_profiles force row level security;
alter table ride_hailing.driver_categories enable row level security;
alter table ride_hailing.driver_categories force row level security;
alter table ride_hailing.rides enable row level security;
alter table ride_hailing.rides force row level security;
alter table ride_hailing.ride_assignments enable row level security;
alter table ride_hailing.ride_assignments force row level security;
alter table ride_hailing.idempotency_keys enable row level security;
alter table ride_hailing.idempotency_keys force row level security;
alter table ride_hailing.sessions enable row level security;
alter table ride_hailing.sessions force row level security;
