--
-- Copyright 2020 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

