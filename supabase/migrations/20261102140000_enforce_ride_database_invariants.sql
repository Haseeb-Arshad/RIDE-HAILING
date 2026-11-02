-- Keep critical lifecycle and assignment invariants true even if a write does
-- not originate from the Go service. The Go API remains the business layer;
-- these guards are database-level integrity backstops.

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
                new.accepted_at := coalesce(new.accepted_at, clock_timestamp());
                new.cancelled_at := null;
                new.completed_at := null;
            when 'cancelled' then
                new.accepted_at := old.accepted_at;
                new.cancelled_at := coalesce(new.cancelled_at, clock_timestamp());
                new.completed_at := null;
            when 'completed' then
                new.accepted_at := old.accepted_at;
                new.cancelled_at := null;
                new.completed_at := coalesce(new.completed_at, clock_timestamp());
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

create trigger rides_guard_update
before update on ride_hailing.rides
for each row execute function ride_hailing.guard_ride_update();

create or replace function ride_hailing.guard_assignment_update()
returns trigger
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
begin
    if row(new.id, new.ride_id, new.driver_id, new.assigned_at)
        is distinct from row(old.id, old.ride_id, old.driver_id, old.assigned_at) then
        raise exception using
            errcode = '23514',
            constraint = 'ride_assignments_identity_immutable_check',
            message = 'A ride assignment cannot be moved to a different ride or driver.';
    end if;

    if new.status is distinct from old.status then
        if old.status <> 'active' or new.status not in ('completed', 'cancelled') then
            raise exception using
                errcode = '23514',
                constraint = 'ride_assignments_lifecycle_transition_check',
                message = 'The assignment lifecycle transition is not allowed.';
        end if;
        new.ended_at := coalesce(new.ended_at, clock_timestamp());
    elsif new.ended_at is distinct from old.ended_at then
        raise exception using
            errcode = '23514',
            constraint = 'ride_assignments_lifecycle_timestamps_immutable_check',
            message = 'Assignment end time can only change when the assignment closes.';
    end if;

    return new;
end;
$function$;

create trigger ride_assignments_guard_update
before update on ride_hailing.ride_assignments
for each row execute function ride_hailing.guard_assignment_update();

create or replace function ride_hailing.assert_ride_assignment_consistency(target_ride_id uuid)
returns void
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
declare
    current_ride_status ride_hailing.ride_status;
    current_assignment_status ride_hailing.assignment_status;
    current_driver_id uuid;
    current_driver_status ride_hailing.driver_status;
    current_user_role ride_hailing.user_role;
begin
    select status
    into current_ride_status
    from ride_hailing.rides
    where id = target_ride_id;

    if not found then
        return;
    end if;

    select driver_id, status
    into current_driver_id, current_assignment_status
    from ride_hailing.ride_assignments
    where ride_id = target_ride_id;

    if not found then
        if current_ride_status in ('accepted', 'completed') then
            raise exception using
                errcode = '23514',
                constraint = 'ride_assignment_consistency_check',
                message = 'An accepted or completed ride must retain its driver assignment.';
        end if;
        return;
    end if;

    if current_ride_status = 'requested'
        or (current_ride_status = 'accepted' and current_assignment_status <> 'active')
        or (current_ride_status = 'completed' and current_assignment_status <> 'completed')
        or (current_ride_status = 'cancelled' and current_assignment_status <> 'cancelled') then
        raise exception using
            errcode = '23514',
            constraint = 'ride_assignment_consistency_check',
            message = 'Ride and assignment lifecycle states must agree.';
    end if;

    select profile.status, app_user.role
    into current_driver_status, current_user_role
    from ride_hailing.driver_profiles as profile
    join ride_hailing.users as app_user on app_user.id = profile.user_id
    where profile.user_id = current_driver_id;

    if not found or current_user_role <> 'driver' then
        raise exception using
            errcode = '23514',
            constraint = 'ride_assignment_driver_role_check',
            message = 'Only a driver profile can hold a ride assignment.';
    end if;

    if current_assignment_status = 'active' and current_driver_status <> 'busy' then
        raise exception using
            errcode = '23514',
            constraint = 'ride_assignment_driver_status_check',
            message = 'A driver with an active assignment must be busy.';
    end if;
end;
$function$;

create or replace function ride_hailing.assert_driver_assignment_consistency(target_driver_id uuid)
returns void
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
declare
    current_driver_status ride_hailing.driver_status;
    current_user_role ride_hailing.user_role;
    has_active_assignment boolean;
begin
    select profile.status, app_user.role
    into current_driver_status, current_user_role
    from ride_hailing.driver_profiles as profile
    join ride_hailing.users as app_user on app_user.id = profile.user_id
    where profile.user_id = target_driver_id;

    if not found then
        return;
    end if;

    if current_user_role <> 'driver' then
        raise exception using
            errcode = '23514',
            constraint = 'driver_profiles_role_check',
            message = 'A driver profile must belong to a driver account.';
    end if;

    select exists (
        select 1
        from ride_hailing.ride_assignments
        where driver_id = target_driver_id and status = 'active'
    ) into has_active_assignment;

    if (current_driver_status = 'busy') is distinct from has_active_assignment then
        raise exception using
            errcode = '23514',
            constraint = 'driver_assignment_status_consistency_check',
            message = 'Busy driver status and active assignment must agree.';
    end if;
end;
$function$;

create or replace function ride_hailing.check_ride_consistency_trigger()
returns trigger
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
begin
    perform ride_hailing.assert_ride_assignment_consistency(new.id);
    return null;
end;
$function$;

create constraint trigger rides_assignment_consistency
after insert or update on ride_hailing.rides
deferrable initially deferred
for each row execute function ride_hailing.check_ride_consistency_trigger();

create or replace function ride_hailing.check_assignment_consistency_trigger()
returns trigger
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
begin
    if tg_op = 'DELETE' then
        perform ride_hailing.assert_ride_assignment_consistency(old.ride_id);
        perform ride_hailing.assert_driver_assignment_consistency(old.driver_id);
        return null;
    end if;

    perform ride_hailing.assert_ride_assignment_consistency(new.ride_id);
    perform ride_hailing.assert_driver_assignment_consistency(new.driver_id);

    if tg_op = 'UPDATE' and old.ride_id is distinct from new.ride_id then
        perform ride_hailing.assert_ride_assignment_consistency(old.ride_id);
    end if;
    if tg_op = 'UPDATE' and old.driver_id is distinct from new.driver_id then
        perform ride_hailing.assert_driver_assignment_consistency(old.driver_id);
    end if;

    return null;
end;
$function$;

create constraint trigger assignments_ride_driver_consistency
after insert or update or delete on ride_hailing.ride_assignments
deferrable initially deferred
for each row execute function ride_hailing.check_assignment_consistency_trigger();

create or replace function ride_hailing.check_driver_consistency_trigger()
returns trigger
language plpgsql
set search_path = pg_catalog, ride_hailing
as $function$
begin
    perform ride_hailing.assert_driver_assignment_consistency(new.user_id);
    return null;
end;
$function$;

create constraint trigger driver_profiles_assignment_consistency
after insert or update on ride_hailing.driver_profiles
deferrable initially deferred
for each row execute function ride_hailing.check_driver_consistency_trigger();
