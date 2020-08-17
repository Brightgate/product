--
-- Copyright 2020 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

CREATE TYPE upgrade_stage AS ENUM (
        'notified',
        'manifest_retrieved',
        'installed',          -- bits are written to disk; appliance needs to reboot
        'complete'            -- appliance has rebooted and reported to the cloud
);

ALTER TABLE appliance_release_history ADD COLUMN repo_commits jsonb;
ALTER TABLE appliance_release_history ADD COLUMN stage upgrade_stage;
ALTER TABLE appliance_release_history ADD COLUMN success bool;
ALTER TABLE appliance_release_history ADD COLUMN message text;
ALTER TABLE appliance_release_history ADD COLUMN log_url text;
ALTER TABLE appliance_release_history
        DROP CONSTRAINT appliance_release_history_appliance_uuid_release_uuid_key;
CREATE UNIQUE INDEX appliance_release_history_appliance_uuid_release_uuid_stage_key
        ON appliance_release_history (appliance_uuid, release_uuid, stage);
ALTER TABLE appliance_release_history
        ADD CONSTRAINT appliance_release_history_appliance_uuid_release_uuid_stage_key
        UNIQUE USING INDEX appliance_release_history_appliance_uuid_release_uuid_stage_key;
COMMENT ON COLUMN appliance_release_history.repo_commits IS 'Repo/commit mapping appliance reported on boot';
COMMENT ON COLUMN appliance_release_history.stage IS 'Current stage of the appliance''s upgrade';
COMMENT ON COLUMN appliance_release_history.success IS 'Was the current stage of upgrade successful';
COMMENT ON COLUMN appliance_release_history.message IS 'Error encountered by appliance on upgrade to this release, if any';
COMMENT ON COLUMN appliance_release_history.log_url IS 'URL to upgrade output log';
COMMENT ON COLUMN appliance_release_history.updated_ts IS 'Time when appliance upgrade completed this stage';

-- Prior to this, this table was updated only when the appliance reported in
-- after reboot, so we pre-populate the stage column with complete.  We don't
-- populate the success column, since we don't actually have that information.
UPDATE appliance_release_history SET stage='complete';

COMMIT;

