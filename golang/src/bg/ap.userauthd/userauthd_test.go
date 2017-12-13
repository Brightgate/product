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

	"text/template"

	"github.com/spf13/afero"
)

var (
	AppFs = afero.NewMemMapFs()

	// Build an RConf.
	trc = rConf{
		ConfDir: "/opt/com.brightgate/etc/templates/ap.userauthd",
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
