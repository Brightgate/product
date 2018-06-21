```
COPYRIGHT 2018 Brightgate Inc. All rights reserved.

This copyright notice is Copyright Management Information under 17 USC 1202
and is included to protect this work and deter copyright infringement.
Removal or alteration of this Copyright Management Information without the
express written permission of Brightgate Inc is prohibited, and any
such unauthorized removal or alteration will be a violation of federal law.
```

README.registry.md

This directory contains tools for use with our Google Cloud based Appliance
Registry.  An Appliance Registry tracks vital data about a fleet of appliances,
including the public key information needed to authenticate appliances.

# Appliance Registry

This is a SQL based registry of the Appliances in our fleet; the backend is
Postgres hosted by [Google Cloud SQL](https://cloud.google.com/sql/).

Each Cloud SQL instance can host an arbitrary number of registry databases, so
provisioning a new registry (while probably rare, mostly for testing) is a
lightweight operation. The `new-appliance-reg.sh` script will create a new one.
Once a registry is created, appliances can be provisioned to the
registry using the `new-appliance.sh` script.  Along with the registry, a cloud
pub/sub topic is created for appliance events associated with the registry.

The output of `new-appliance-reg` (besides the database itself) is a shell
include file (and a JSON document) which contains all of the parameters needed
to describe the registry.

Here is an example of registry creation:

```shell
$ ./new-appliance-reg.sh
usage: ./new_appliance-reg.sh <credentials-file> <cloudsql-instance> <project-id> <region-id> <registry-id>

$ ./new-appliance-reg.sh ~/secrets/Engineering-f51a19014a36.json eng-scratch0 peppy-breaker-161717 us-west1 testreg3
Postgres database password [user=postgres]:
-------------------------------------------------------------
Creating pubsub topic and database
Activated service account credentials for: [appliance-reg-admin@peppy-breaker-161717.iam.gserviceaccount.com]
Created topic [projects/peppy-breaker-161717/topics/cloudappliance-us-west1-testreg3-events].
Creating Cloud SQL database...done.
Created database [appliance-reg_testreg3].
instance: eng-scratch0
name: appliance-reg_testreg3
project: peppy-breaker-161717
-------------------------------------------------------------
Loading schema files into database
Loading: schema000.sql
NOTICE:  table "heartbeat_ingest" does not exist, skipping
NOTICE:  table "identity_map" does not exist, skipping
NOTICE:  table "appliance_id_map" does not exist, skipping
Loading: schema001.sql
-------------------------------------------------------------
Writing registry info to appliance-reg-peppy-breaker-161717-us-west1-testreg3.sh
-------------------------------------------------------------
Created pubsub topic: projects/peppy-breaker-161717/topics/appliance-reg-us-west1-testreg3-events
    Created registry: projects/peppy-breaker-161717/locations/us-west1/registries/testreg3
   CloudSQL instance: eng-scratch0
        Registry URI: postgres:///appliance-reg_testreg3?host=/var/tmp/cloud-sql-sock/peppy-breaker-161717:us-west1:eng-scratch0&user=postgres
      Loaded Schemas: schema000.sql schema001.sql
  Shell include file: appliance-reg-peppy-breaker-161717-us-west1-testreg3.sh
      JSON info file: appliance-reg-peppy-breaker-161717-us-west1-testreg3.json
-------------------------------------------------------------
```

Where `Engineering-xxx.json` is the credential fetched from the GCP service
account `appliance-reg-admin@project` in the GCP IAM console.

# Appliance Provisioning

The appliance provisioning process involves creating a key pair, provisioning
the public side of the key pair to the appliance registry, and provisioning the
private side of the key pair to the appliance.  The private side of the key
pair, as well as the rest of the credential parameters, are bundled in a JSON
envelope.

Example:

First, allocate the key pair and insert the appliance into the registry:

```shell
$ . appliance-reg-peppy-breaker-161717-us-west1-testreg3.sh
$ ./new-appliance.sh ~/secrets/Engineering-f51a19014a36.json test-appliance
Creating output_secrets/
Generating Key/Pair and Certificate for test-appliance
Generating a 2048 bit RSA private key
........................+++
............+++
writing new private key to 'test-appliance.rsa_private.pem'
-----
-------------------------------------------------------------
Recording appliance to SQL database; you may need to give a password.
Password:
-------------------------------------------------------------
Summary:
      Created device: projects/peppy-breaker-161717/locations/us-west1/registries/testreg3/appliances/test-appliance
Appliance Cloud UUID: e6ec7450-af1c-4dcc-9aed-3216a158df6f
        Secrets file: output_secrets/test-appliance.cloud.secret.json
-------------------------------------------------------------
Next, provision test-appliance.cloud.secret.json to the appliance at: /opt/com.brightgate/etc/secret/cloud/cloud.secret.json
```

As directed, copy the `.cloud.secret.json` file to the appliance, and
name it `/opt/com.brightgate/etc/secret/cloud/cloud.secret.json`.
`chmod 600 /opt/com.brightgate/etc/secret/cloud/cloud.secret.json`
is a good practice.

To confirm that it's working, try:

```
pi@pi $ sudo /opt/com.brightgate/bin/ap-rpc heartbeat
```
