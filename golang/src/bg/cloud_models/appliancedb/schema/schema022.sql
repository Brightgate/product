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
    CREATE ROLE rpcd_group WITH NOLOGIN;
    EXCEPTION WHEN duplicate_object THEN
        RAISE NOTICE 'rpcd_group role already exists';
END
$$;

GRANT EXECUTE
    ON FUNCTION next_siteid(text), register_domain(uuid, varchar(16))
    TO rpcd_group;
GRANT INSERT
    ON TABLE appliance_release_history, jurisdictions, site_domains,
        siteid_sequences
    TO rpcd_group;
GRANT SELECT
    ON TABLE appliance_id_map, appliance_pubkey, appliance_release_history,
        appliance_release_targets, artifacts, platforms, release_artifacts,
        releases, site_certs, site_cloudstorage, site_domains, siteid_sequences
    TO rpcd_group;
GRANT UPDATE
    ON TABLE appliance_release_history, siteid_sequences
    TO rpcd_group;

GRANT rpcd_group TO vault_root;

-- This should have been part of schema019.sql.
GRANT SELECT
    ON TABLE oauth2_refresh_token
    TO httpd_group;

COMMIT;
