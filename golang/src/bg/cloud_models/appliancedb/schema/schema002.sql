--
-- Copyright 2018 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

CREATE TABLE IF NOT EXISTS appliance_cloudstorage (
    cloud_uuid  uuid REFERENCES appliance_id_map(cloud_uuid) PRIMARY KEY,
    bucket      varchar(63),
    provider    varchar(8)
);

COMMENT ON TABLE appliance_cloudstorage IS 'Holds information about cloud storage associated with an appliance';
COMMENT ON COLUMN appliance_cloudstorage.bucket IS 'Bucket name as per https://cloud.google.com/storage/docs/naming, and https://docs.aws.amazon.com/AmazonS3/latest/dev/BucketRestrictions.html';
COMMENT ON COLUMN appliance_cloudstorage.provider IS 'Names the provider (presently gcs) for the bucket';

COMMIT;

