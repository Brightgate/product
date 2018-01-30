/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package daemonutils

import (
	"testing"
)

func TestAutoSetup(t *testing.T) {
	globalLog = nil
	logTypeFlag = logTypeAuto
	_, _ = SetupLogs()
}

func TestDevSetup(t *testing.T) {
	globalLog = nil
	logTypeFlag = logTypeDev
	_, _ = SetupLogs()
}

func TestProdSetup(t *testing.T) {
	globalLog = nil
	logTypeFlag = logTypeProd
	_, _ = SetupLogs()
}

func TestResetup(t *testing.T) {
	l1, _ := SetupLogs()
	l2, _ := ResetupLogs()
	if l1 == l2 {
		t.Errorf("Resetup seemed to have no effect")
	}
}

func TestLogType(t *testing.T) {
	var l logType
	for _, v := range []string{"dev", "DEV", "Development", "pro", "prod", "PRODUCTION"} {
		err := l.Set(v)
		if err != nil {
			t.Errorf("Unexpected error logtype %s", v)
		}
		if l != logTypeDev && l != logTypeProd {
			t.Errorf("Unexpected devtype string")
		}
	}
	err := l.Set("foo")
	if err == nil {
		t.Errorf("Didn't get expected error from bad logtype")
	}
}
