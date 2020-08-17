--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

-- Organization table
CREATE TABLE IF NOT EXISTS organization (
  uuid uuid PRIMARY KEY,
  name varchar(256) NOT NULL
);
COMMENT ON TABLE organization IS 'Organization is an entity that can own and deploy Brightgate appliances.  Among other possible uses, organization is used to tie accounts to sites';
COMMENT ON COLUMN organization.name IS 'Name of company or group';
INSERT INTO organization (uuid, name) VALUES
  ('00000000-0000-0000-0000-000000000000'::uuid, 'sentinel:null-organization')
  ON CONFLICT DO NOTHING;

-- Add Organization to existing customer_site table
ALTER TABLE customer_site ADD COLUMN organization_uuid uuid REFERENCES organization (uuid);
UPDATE customer_site
  SET organization_uuid = '00000000-0000-0000-0000-000000000000'::uuid
  WHERE organization_uuid IS NULL;
ALTER TABLE customer_site ALTER COLUMN organization_uuid SET NOT NULL;
COMMENT ON COLUMN customer_site.organization_uuid IS 'The organization which owns the site';

-- OAuth2 providers
CREATE TABLE IF NOT EXISTS oauth2_providers (
	name varchar(32) PRIMARY KEY
);
INSERT INTO oauth2_providers (name) VALUES
  ('google'),
  ('azureadv2') ON CONFLICT DO NOTHING;
COMMENT ON TABLE oauth2_providers IS 'The list of valid OAuth2 providers.';
COMMENT ON COLUMN oauth2_providers.name IS 'OAuth2 provider name';

-- OAuth2 information --> Organization mapping rules
-- These rules are used when a new user is created in order to
-- associate them with an organization.
do $$
begin
    if not exists (select 1 from pg_type where typname = 'oauth2_org_ruletype') then
	CREATE TYPE oauth2_org_ruletype AS ENUM ('tenant', 'domain', 'email');
    end if;
end$$;
COMMENT ON TYPE oauth2_org_ruletype IS 'The set of valid OAuth2Organization rule types.';

CREATE TABLE IF NOT EXISTS oauth2_organization_rule (
  provider varchar(32) REFERENCES oauth2_providers (name) NOT NULL,
  rule_type oauth2_org_ruletype NOT NULL,
  rule_value varchar(1024) NOT NULL,
  organization_uuid uuid REFERENCES organization(uuid) NOT NULL,
  PRIMARY KEY (provider,rule_type,rule_value)
);
COMMENT ON TABLE oauth2_organization_rule IS 'Rules to map OAuth2 information to an organization, for use in account creation.  This table of rules forms a whitelist.';
COMMENT ON COLUMN oauth2_organization_rule.provider IS 'Provider name for matching rule';
COMMENT ON COLUMN oauth2_organization_rule.rule_type IS 'Match by email address, by email domain, or by provider-specific "tenant" id (preferred)';
COMMENT ON COLUMN oauth2_organization_rule.rule_value IS 'The value to match, specific meaning depends on the rule type';
COMMENT ON COLUMN oauth2_organization_rule.organization_uuid IS 'The organization to which a user, matching the criteria, should be bound';

-- person records
CREATE TABLE IF NOT EXISTS person (
  uuid uuid PRIMARY KEY,
  name varchar(1024) NOT NULL,
  primary_email varchar(254) NOT NULL -- http://www.rfc-editor.org/errata/eid1690
);
-- A person may have multiple accounts; we may not be able to tell that two
-- accounts belong to the same person.
COMMENT ON TABLE person IS 'Natural person -- a human, but not necessarily unique.';
COMMENT ON COLUMN person.name IS 'Full name; ref. openid-connect-core-1.0 "name" claim.';
COMMENT ON COLUMN person.primary_email IS 'Preferred email; ref. openid-connect-core-1.0 "email" claim.';

-- account represents a user account in our system.
CREATE TABLE IF NOT EXISTS account (
  uuid uuid PRIMARY KEY,
  email varchar(254) NOT NULL,
  phone_number varchar(64),
  person_uuid uuid REFERENCES person (uuid) NOT NULL,
  organization_uuid uuid REFERENCES organization (uuid) NOT NULL
);
COMMENT ON TABLE account IS 'User accounts';
COMMENT ON COLUMN account.email IS 'Email address associated with this account; ref. openid-connect-core-1.0 "email" claim.';
COMMENT ON COLUMN account.phone_number IS 'Phone number associated with this account; ref. openid-connect-core-1.0 "phone_number" claim.';
COMMENT ON COLUMN account.person_uuid IS 'Underlying Person record for this account.';
COMMENT ON COLUMN account.organization_uuid IS 'Organization which is the owner of this account.';

-- oauth2_identity represents an oath2-based statement of identity.
-- This tracks an external provider's identity; one or more oauth2_identity
-- records can exist for a given account.
-- Terminology and some field lengths are cribbed from OpenID connect,
-- https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth.
--
-- Limitations: This does not handle users from unknown domains, basically at
-- all due to the requirement that account_uuid is non-null. In the future
-- we may wish to also store the issuer if we can get it.
CREATE TABLE IF NOT EXISTS oauth2_identity (
  id serial PRIMARY KEY,
  provider varchar(32) REFERENCES oauth2_providers (name) NOT NULL,
  subject varchar(256) NOT NULL, -- as per openid-connect-core-1_0

  account_uuid uuid REFERENCES account (uuid) NOT NULL,
  UNIQUE (provider, subject)
);
COMMENT ON TABLE oauth2_identity IS 'OAuth2 asserted identities';
COMMENT ON COLUMN oauth2_identity.provider IS 'Provider name for this identity';
COMMENT ON COLUMN oauth2_identity.subject IS 'Subject (OAuth2 unique) name for this identity; ref. openid-connect-core-1.0 "sub" claim.';
COMMENT ON COLUMN oauth2_identity.account_uuid IS 'Account which this identity can authenticate.';

-- OAuth2 access token storage
-- inspired by https://github.com/jazzband/django-oauth-toolkit/tree/master/oauth2_provider/migrations
CREATE TABLE IF NOT EXISTS oauth2_access_token (
  id bigserial PRIMARY KEY,
  identity_id integer REFERENCES oauth2_identity (id),
  token varchar(32768) NOT NULL,
  expires timestamp with time zone NOT NULL
);
COMMENT ON TABLE oauth2_access_token IS 'OAuth2 access tokens';
COMMENT ON COLUMN oauth2_access_token.identity_id IS 'Identity which this token authenticates';
COMMENT ON COLUMN oauth2_access_token.token IS 'Verbatim OAuth2 token';
COMMENT ON COLUMN oauth2_access_token.expires IS 'Token expiry timestamp';

-- inspired by https://github.com/jazzband/django-oauth-toolkit/tree/master/oauth2_provider/migrations
CREATE TABLE IF NOT EXISTS oauth2_refresh_token (
  identity_id integer REFERENCES oauth2_identity (id) PRIMARY KEY,
  token varchar(32768) NOT NULL
);
COMMENT ON TABLE oauth2_refresh_token IS 'OAuth2 refresh tokens';
COMMENT ON TABLE oauth2_refresh_token IS 'Verbatim OAuth2 refresh token';

COMMIT;

