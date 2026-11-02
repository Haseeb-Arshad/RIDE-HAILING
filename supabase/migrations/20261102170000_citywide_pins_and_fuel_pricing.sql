-- Extend seeded stops with rider-selected pins and an auditable distance/fuel rate card.

alter table ride_hailing.locations
    add column selectable boolean not null default true;

alter table ride_hailing.locations
    add constraint locations_lahore_service_bounds_check
    check (
        latitude between 31.20 and 31.75
        and longitude between 73.95 and 74.70
    );

create index locations_city_selectable_name_idx
    on ride_hailing.locations (city, name)
    where active and selectable;

create table ride_hailing.pricing_rates (
    category ride_hailing.ride_category primary key,
    fuel_price_paisa_per_liter integer not null,
    fuel_price_effective_date date not null,
    fuel_efficiency_meters_per_liter integer not null,
    base_fare_paisa bigint not null,
    non_fuel_cost_paisa_per_km bigint not null,
    road_distance_factor_basis_points integer not null,
    version integer not null default 1,
    updated_at timestamptz not null default now(),
    constraint pricing_rates_fuel_price_positive check (fuel_price_paisa_per_liter > 0),
    constraint pricing_rates_fuel_efficiency_positive check (fuel_efficiency_meters_per_liter > 0),
    constraint pricing_rates_base_fare_nonnegative check (base_fare_paisa >= 0),
    constraint pricing_rates_non_fuel_cost_nonnegative check (non_fuel_cost_paisa_per_km >= 0),
    constraint pricing_rates_road_factor_positive check (road_distance_factor_basis_points between 10000 and 30000),
    constraint pricing_rates_version_positive check (version > 0)
);

insert into ride_hailing.pricing_rates (
    category,
    fuel_price_paisa_per_liter,
    fuel_price_effective_date,
    fuel_efficiency_meters_per_liter,
    base_fare_paisa,
    non_fuel_cost_paisa_per_km,
    road_distance_factor_basis_points
)
values
    ('economy', 37791, date '2026-10-31', 12000, 15000, 2850, 13000),
    ('comfort', 37791, date '2026-10-31', 9000, 25000, 2800, 13000);

create or replace function ride_hailing.bump_pricing_rate_version()
returns trigger
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
begin
    new.version := old.version + 1;
    new.updated_at := clock_timestamp();
    return new;
end;
$function$;

create trigger pricing_rates_bump_version
before update on ride_hailing.pricing_rates
for each row execute function ride_hailing.bump_pricing_rate_version();

alter table ride_hailing.pricing_rates enable row level security;
alter table ride_hailing.pricing_rates force row level security;

revoke all on ride_hailing.pricing_rates from public, anon, authenticated, service_role;

alter table ride_hailing.rides
    add column fuel_price_paisa_per_liter integer,
    add column fuel_price_effective_date date,
    add column fuel_cost_paisa bigint,
    add column base_fare_paisa bigint,
    add column distance_cost_paisa bigint,
    add column pricing_version integer;

alter table ride_hailing.rides
    add constraint rides_pricing_snapshot_consistent_check
    check (
        (
            fuel_price_paisa_per_liter is null
            and fuel_price_effective_date is null
            and fuel_cost_paisa is null
            and base_fare_paisa is null
            and distance_cost_paisa is null
            and pricing_version is null
        )
        or (
            fuel_price_paisa_per_liter is not null
            and fuel_price_effective_date is not null
            and fuel_cost_paisa is not null
            and base_fare_paisa is not null
            and distance_cost_paisa is not null
            and pricing_version is not null
            and fuel_price_paisa_per_liter > 0
            and fuel_cost_paisa >= 0
            and base_fare_paisa >= 0
            and distance_cost_paisa >= 0
            and pricing_version > 0
            and estimated_fare_paisa = fuel_cost_paisa + base_fare_paisa + distance_cost_paisa
        )
    );

create or replace function ride_hailing.guard_ride_update()
returns trigger
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
begin
    if row(
        new.id,
        new.rider_id,
        new.pickup_location_id,
        new.destination_location_id,
        new.category,
        new.estimated_fare_paisa,
        new.estimated_distance_meters,
        new.pickup_eta_minutes,
        new.currency,
        new.approximate,
        new.fuel_price_paisa_per_liter,
        new.fuel_price_effective_date,
        new.fuel_cost_paisa,
        new.base_fare_paisa,
        new.distance_cost_paisa,
        new.pricing_version,
        new.created_at
    ) is distinct from row(
        old.id,
        old.rider_id,
        old.pickup_location_id,
        old.destination_location_id,
        old.category,
        old.estimated_fare_paisa,
        old.estimated_distance_meters,
        old.pickup_eta_minutes,
        old.currency,
        old.approximate,
        old.fuel_price_paisa_per_liter,
        old.fuel_price_effective_date,
        old.fuel_cost_paisa,
        old.base_fare_paisa,
        old.distance_cost_paisa,
        old.pricing_version,
        old.created_at
    ) then
        raise exception using
            errcode = '23514',
            constraint = 'rides_request_snapshot_immutable_check',
            message = 'The rider, route, category, and fare snapshot cannot be changed.';
    end if;

    if new.status is distinct from old.status then
        if not (
            (old.status = 'requested' and new.status in ('accepted', 'cancelled'))
            or (old.status = 'accepted' and new.status in ('completed', 'cancelled'))
        ) then
            raise exception using
                errcode = '23514',
                constraint = 'rides_lifecycle_transition_check',
                message = 'The ride lifecycle transition is not allowed.';
        end if;

        case new.status
            when 'accepted' then
                new.accepted_at := clock_timestamp();
                new.cancelled_at := null;
                new.completed_at := null;
            when 'cancelled' then
                new.accepted_at := old.accepted_at;
                new.cancelled_at := clock_timestamp();
                new.completed_at := null;
            when 'completed' then
                new.accepted_at := old.accepted_at;
                new.cancelled_at := null;
                new.completed_at := clock_timestamp();
        end case;
    elsif row(new.accepted_at, new.cancelled_at, new.completed_at)
        is distinct from row(old.accepted_at, old.cancelled_at, old.completed_at) then
        raise exception using
            errcode = '23514',
            constraint = 'rides_lifecycle_timestamps_immutable_check',
            message = 'Ride lifecycle timestamps can only change with a valid transition.';
    end if;

    new.updated_at := greatest(clock_timestamp(), old.updated_at + interval '1 microsecond');
    return new;
end;
$function$;
