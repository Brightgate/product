/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package modeldb

import (
	"context"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/pkg/errors"
)

// GetModelFromURL is a helper routine; it either resolves or fetches
// the model base on an input URL; supported schemes are gs: and file:.
func GetModelFromURL(modelURL string) (string, error) {
	var modelPath string
	url, err := url.Parse(modelURL)
	if err != nil {
		return "", errors.Wrap(err, "parsing model-file")
	}

	if url.Scheme == "gs" {
		ctx := context.Background()
		storageClient, err := storage.NewClient(ctx)
		if err != nil {
			return "", errors.Wrapf(err, "creating storage client")
		}
		bucket := storageClient.Bucket(url.Host)
		upath := strings.TrimLeft(url.Path, "/")
		object := bucket.Object(upath)
		r, err := object.NewReader(ctx)
		if err != nil {
			return "", errors.Wrapf(err, "reading %s", modelURL)
		}
		defer r.Close()
		tmpFile, err := ioutil.TempFile("", "cl-obs-trained-model")
		if err != nil {
			return "", errors.Wrap(err, "creating temp file")
		}
		if _, err := io.Copy(tmpFile, r); err != nil {
			// TODO: Handle error.
			return "", errors.Wrapf(err, "copying %s -> %s", modelURL, tmpFile.Name())
		}
		if err := tmpFile.Close(); err != nil {
			return "", errors.Wrapf(err, "closing %s", tmpFile.Name())
		}
		modelPath = tmpFile.Name()

	} else if url.Scheme == "" {
		modelPath = url.Path
		if _, err := os.Stat(modelPath); os.IsNotExist(err) {
			return "", errors.Wrap(err, "doesn't exist")
		}
	} else {
		return "", errors.Errorf("unsupported scheme %s", url.Scheme)
	}
	return modelPath, nil
}

