/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"os"
	"path/filepath"

	"bg/cloud_models/appliancedb"
	"bg/common/faults"

	"cloud.google.com/go/pubsub"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

func writeFaultFile(uuid uuid.UUID, data []byte) (string, error) {
	appPath := filepath.Join(reportBasePath, uuid.String(), "faults")
	if err := os.MkdirAll(appPath, 0755); err != nil {
		return "", errors.Wrap(err, "fault mkdir failed")
	}

	path, err := faults.WriteReportSerialized(appPath, data)
	if err != nil {
		err = errors.Wrap(err, "creating fault file")
	}

	return path, err
}

func faultMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	siteUUID uuid.UUID, m *pubsub.Message) {
	var err error

	// For now we have nothing we can really do with malformed messages
	defer m.Ack()

	slog := slog.With("appliance_uuid", m.Attributes["appliance_uuid"],
		"site_uuid", m.Attributes["site_uuid"])

	slog.Debugf("msg: %s", string(m.Data))

	path, err := writeFaultFile(siteUUID, m.Data)
	if err != nil {
		slog.Errorw("failed to write FaultReport to file",
			"path", path, "error", err)
	} else {
		slog.Infow("wrote FaultReport to file", "path", path)
	}

	path, err = faults.ReportPath("faults", m.Data)
	if err != nil {
		slog.Errorw("failed to write FaultReport to cloud",
			"error", err)
		return
	}
	url, err := writeCSObject(ctx, applianceDB, siteUUID, path, m.Data)
	if err != nil {
		slog.Errorw("failed to write FaultReport to cloud",
			"url", url, "error", err)
	} else {
		slog.Infow("wrote FaultReport to cloud", "url", url)
	}
}

