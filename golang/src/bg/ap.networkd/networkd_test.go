/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"testing"

	"os"
	"text/template"

	"github.com/spf13/afero"
)

var (
	AppFs = afero.NewMemMapFs()

	conf apConfig
)

func TestHostAPDConfTemplate(t *testing.T) {
	var err error
	tfile := "hostapd.conf.got"

	// Create hostapd.conf, using the apConfig contents to fill out the .got
	// template
	tplt, err := template.ParseFiles(tfile)
	if err != nil {
		cwd, _ := os.Getwd()
		t.Fatalf("can't find template from %s: %v\n", cwd, err)
	}

	fn := conf.ConfDir + "/" + conf.confFile
	cf, _ := AppFs.Create(fn)
	defer cf.Close()

	err = tplt.Execute(cf, conf)
	if err != nil {
		t.Fatalf("template execution failed: %v\n", err)
	}
}
