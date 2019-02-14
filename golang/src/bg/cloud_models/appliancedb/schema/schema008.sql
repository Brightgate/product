--
-- COPYRIGHT 2019 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
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
