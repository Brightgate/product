/*
 * Copyright 2017 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"testing"

	"text/template"

	"github.com/spf13/afero"
)

var (
	AppFs = afero.NewMemMapFs()

	// Build an RConf.
	trc = &rConf{
		ConfDir: "/opt/com.brightgate/etc/templates/ap.wifid",
	}

	UserauthTemplates = []struct {
		templateStem string
		fileStem     string
	}{
		{"hostapd.users.got", "hostapd.users.conf"},
		{"hostapd.radius.got", "hostapd.radius.conf"},
		{"hostapd.radius_clients.got", "hostapd.radius_client.conf"},
	}
)

func TestRadiusHostapdTemplates(t *testing.T) {
	for _, ut := range UserauthTemplates {
		tplt, err := template.ParseFiles(ut.templateStem)

		if err != nil {
			t.Fatalf("%s template parse failed: %v\n", ut.templateStem, err)
		}

		un := trc.ConfDir + "/" + ut.fileStem
		uf, _ := AppFs.Create(un)
		defer uf.Close()

		err = tplt.Execute(uf, trc)
		if err != nil {
			t.Fatalf("%s template execution failed: %v\n", ut.templateStem, err)
		}
	}
}

