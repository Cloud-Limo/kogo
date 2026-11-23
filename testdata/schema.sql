-- The test schema: the tables examples/ query, and one table holding
-- every type of spec.md section 10.4. Loaded by `mise run testdb`.
create extension if not exists citext;

create domain percent as integer check (value between 0 and 100);
create type invite_kind as enum ('in_house', 'outside');
create type invoice_status as enum ('draft', 'sent', 'paid');

create table business (
    id uuid primary key,
    slug citext not null unique,
    legal_name text not null,
    created_at timestamptz not null default now(),
    abn text,
    abn_checked_at timestamptz,
    payid text,
    payid_name text
);

create table person (
    id uuid primary key,
    name text not null,
    email citext not null unique
);

create table admin_grant (
    person_id uuid not null references person,
    business_id uuid not null references business,
    primary key (person_id, business_id)
);

create table tenant (
    id uuid primary key,
    business_id uuid not null unique references business,
    slug citext not null unique
);

create table tenant_driver (
    tenant_id uuid not null references tenant,
    driver_id uuid not null references person,
    active boolean not null default true,
    primary key (tenant_id, driver_id)
);

create table supply_agreement (
    operator_id uuid not null references business,
    supplier_id uuid not null references business,
    driver_share_percent percent not null,
    primary key (operator_id, supplier_id)
);

create table invite (
    id uuid primary key,
    business_id uuid not null references business,
    kind invite_kind not null,
    person_id uuid not null references person,
    invited_by uuid not null references person,
    created_at timestamptz not null default now(),
    driver_share_percent percent,
    unique (business_id, person_id)
);

create table invoice (
    id uuid primary key,
    number text not null unique,
    total_cents bigint,
    status invoice_status not null default 'draft',
    platform_fee_accounted_at timestamptz,
    platform_fee_reference text
);

-- How often each address has looked an invoice up (examples/22_api.kg).
create table look (
    address text primary key,
    count integer not null
);

-- A composite type made with create type, whose fields Postgres
-- cannot declare NOT NULL, and a table holding it.
create type amount as (cents bigint, currency text);

create table quote (
    id uuid primary key,
    total amount not null,
    deposit amount
);

-- A composite type whose name Postgres writes quoted.
create type "Price" as (cents bigint);

-- One column per row of the type table, all NOT NULL but `note`.
create table every_type (
    flag boolean not null,
    small integer not null,
    big bigint not null,
    ratio double precision not null,
    amount numeric(12, 2) not null,
    body text not null,
    folded citext not null,
    code varchar(8) not null,
    id uuid not null,
    at timestamptz not null,
    day date not null,
    clock time not null,
    span interval not null,
    kind invite_kind not null,
    tags text[] not null,
    share percent not null,
    note text
);

-- Postgres 18 reports a NOT VALID constraint as attnotnull: rows from
-- before it may still hold a null.
alter table every_type add column pending text;
alter table every_type add constraint pending_not_null not null pending not valid;

-- Bytes, kept and absent (examples/30_mail.kg).
create table letter (
    id uuid primary key,
    eml bytea not null,
    draft bytea
);

-- The domains whose pages may call, and their visitors' sessions
-- (examples/27_origin.kg, examples/28_session.kg).
create table shop (
    domain text primary key
);

create table session (
    id uuid primary key,
    csrf_token text not null,
    expires_at timestamptz not null
);

-- What a gate must not touch but to read (doc/gates.md): a view to
-- write through, a sequence, and a function that writes.
create view people as select id, name, email from person;
create sequence ticket;
create function rename_all() returns void language sql as $$ update person set name = name $$;

-- A cast the schema defines, whose function can return null.
create function amount_of(text) returns amount language sql as $$ select null::amount $$;
create cast (text as amount) with function amount_of(text);

-- A worker's queues (examples/32_work.kg; doc/work.md, "Workers"). A
-- parcel waits to be handed to a courier: a job queues its dispatch,
-- and an effect, whose table holds the runner's columns before its
-- own, books the courier.
create table parcel (
    id uuid primary key,
    depot text not null,
    address text not null,
    grams integer not null,
    status text not null default 'new'
);

create table parcel_dispatch (
    id uuid primary key,
    attempts integer not null default 0,
    next_attempt_at timestamptz not null,
    claimed_at timestamptz,
    last_error text,
    failed boolean not null default false,
    done_at timestamptz,
    not_after timestamptz,
    parcel_id uuid not null references parcel,
    express_since timestamptz,
    tracking text,
    failed_message text
);
