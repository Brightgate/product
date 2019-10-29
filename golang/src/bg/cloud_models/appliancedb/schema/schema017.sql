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

-- Enforce that domain and email rules are stored in lower-cased form
ALTER TABLE oauth2_organization_rule
  ADD CONSTRAINT value_case_constraint CHECK 
	((rule_type != 'email' AND rule_type != 'domain') OR LOWER(rule_value)=rule_value);

COMMIT;
