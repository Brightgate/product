--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

-- Enforce that domain and email rules are stored in lower-cased form
ALTER TABLE oauth2_organization_rule
  ADD CONSTRAINT value_case_constraint CHECK 
	((rule_type != 'email' AND rule_type != 'domain') OR LOWER(rule_value)=rule_value);

COMMIT;

