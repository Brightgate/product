--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

ALTER TABLE account_secrets
  ADD COLUMN appliance_user_mschapv2_ts timestamp with time zone NOT NULL DEFAULT NOW();
ALTER TABLE account_secrets
  ADD COLUMN appliance_user_mschapv2_regime varchar(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE account_secrets
  ADD COLUMN appliance_user_bcrypt_ts timestamp with time zone NOT NULL DEFAULT NOW();
ALTER TABLE account_secrets
  ADD COLUMN appliance_user_bcrypt_regime varchar(32) NOT NULL DEFAULT 'unknown';
COMMENT ON COLUMN account_secrets.appliance_user_mschapv2_ts IS 'timestamp when appliance_user_mschapv2 was changed';
COMMENT ON COLUMN account_secrets.appliance_user_bcrypt_ts IS 'timestamp when appliance_user_bcrypt was changed';

COMMIT;

