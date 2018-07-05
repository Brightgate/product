--
-- COPYRIGHT 2018 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

CREATE TABLE IF NOT EXISTS appliance_cloudstorage (
    cloud_uuid  uuid REFERENCES appliance_id_map(cloud_uuid) PRIMARY KEY,
    bucket      varchar(63),
    provider    varchar(8)
);

COMMENT ON TABLE appliance_cloudstorage IS 'Holds information about cloud storage associated with an appliance';
COMMENT ON COLUMN appliance_cloudstorage.bucket IS 'Bucket name as per https://cloud.google.com/storage/docs/naming, and https://docs.aws.amazon.com/AmazonS3/latest/dev/BucketRestrictions.html';
COMMENT ON COLUMN appliance_cloudstorage.provider IS 'Names the provider (presently gcs) for the bucket';
