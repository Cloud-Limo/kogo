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

-- Two columns of one type, which a test moves in its own schema to
-- see that a read takes each by its name.
create table account (
    id int primary key,
    owner text not null,
    payee text not null
);

-- Rows enough for a parallel plan, their keys out of their order.
create table ranked as select n as id, n * 7919 % 5003 as k from generate_series(1, 5000) n;

-- A domain over each scalar type of spec.md 10.4 and over an enum, for
-- the table of emit's TestShapes: a type is read alone, in an array
-- and through its domain.
create domain d_bool as boolean; create domain d_int as integer; create domain d_long as bigint;
create domain d_double as double precision; create domain d_numeric as numeric(10, 2); create domain d_text as text;
create domain d_citext as citext; create domain d_varchar as varchar; create domain d_uuid as uuid;
create domain d_timestamptz as timestamptz; create domain d_date as date; create domain d_time as time;
create domain d_interval as interval; create domain d_bytea as bytea; create domain d_kind as invite_kind;
create domain d_tags as text[];
