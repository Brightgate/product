--
-- COPYRIGHT 2020 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

BEGIN;

DO $$
BEGIN
    CREATE ROLE httpd_group WITH NOLOGIN;
    EXCEPTION WHEN duplicate_object THEN
        RAISE NOTICE 'httpd_group role already exists';
END
$$;

GRANT DELETE
    ON TABLE account, account_org_role, account_secrets, oauth2_identity, person
    TO httpd_group;
GRANT INSERT
    ON TABLE account, account_org_role, account_secrets, oauth2_identity,
        oauth2_refresh_token, person
    TO httpd_group;
GRANT SELECT
    ON TABLE account, account_org_role, account_secrets, appliance_id_map,
        customer_site, heartbeat_ingest, oauth2_identity, oauth2_organization_rule,
        org_org_relationship, organization, person, relationship_roles, site_commands
    TO httpd_group;
GRANT UPDATE
    ON TABLE account, account_secrets, oauth2_refresh_token
    TO httpd_group;
GRANT USAGE
    ON SEQUENCE oauth2_identity_id_seq
    TO httpd_group;

DO $$
BEGIN
    CREATE ROLE vault_root WITH LOGIN CREATEROLE;
    EXCEPTION WHEN duplicate_object THEN
        RAISE NOTICE 'vault_root role already exists';
END
$$;

GRANT httpd_group TO vault_root;

COMMIT;
