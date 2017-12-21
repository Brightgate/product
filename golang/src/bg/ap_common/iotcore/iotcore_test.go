/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package iotcore

import (
	"fmt"
	"strings"
	"testing"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/fgrosse/zaptest"
	"go.uber.org/zap"
)

const testProject = "peppy-breaker-161717"
const testRegion = "us-central1"
const testRegistry = "unit-testing"
const testDevice = "unit-testing-fake-device"

const testConfigPayload = "unit test config payload (for iotcore_test.go)"

// XXX This key allows access to this endpoint in our cloud.  However the
// registry is for testing only, as is the device.  So the main risk would be
// to our GCP bill.  As such it represents a very small security threat.  When
// we have a secrets management facility, regenerate this secret and move it
// there.
//
const testPEM = `
-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCxzpqSsYuMyBet
1RilkvAwttSCyoAGdmQrwVACOSzIarRkX+MIicMTlslps0KQpGsBmP1IAS3sF930
sdhi/vnM97v0bBKJypwHwwX6w3Fygq/y5cn1RiS2I4td+yAs7AxGLowl+Iz95bFu
lJ3o9z1nWVOqdN5z2qsHAfeSBv5UZxUBs69brTGodDuDIHvDlwpoEIJoLfo2pNjj
EWNWv1NLQV/bv3zsTLbp4GDKGIQMlkUv2JRD7acl8m/D0mKG9FiAWNeJKKqlTFdI
07v0t0hAqCYLFVvolBGyrAtYK13DxZ13h6IUzPSUYT7RN653HrQkUyY8vAa2Bdyo
uwxY/CK7AgMBAAECggEAG6eHkQM+MiI41JeNIstswhbdjI4URW0KfWeumvnrhixa
bDYhqIVMqvJL1z3DP53i6rexxQ4x50N7CQDUJ+mCTqfFOunIJFg31lk1x9+3+Fht
JzkoJRbIxO9YUMCrK3F3Iz9AGvPCcgbUht9khARYL4fMJHnS03ASI5/hsnuV+Ohg
9wKo/lG3SEwj2rTBIImUf8b8EDXUONscwR++c1Wz9FmFlx4LnxSfGG+2QvZfKzdY
e1CRINARdabF0qG2shPFgjrlY/9928Bv3GPVk/YOFcenTiqmZYfjcKPazl/fyUkx
XE9P90/TlsvtaRNmf8oIQ49DVf+/FByje2OVe13kmQKBgQDiefoGHyyKHfWnjsD/
/1mh862bklEJKpHVkJh/PfX+jMnCURoHHfbEXPyEnMztiCr5FCTTtmacSu2HHfES
gdOtvDuaR6GrFKpLlTKbT7f11phLIiQS9c9LYa64ldVWafEM0R343n2nf4ppCYjy
dHlSDQS/lJljSVlLtvS9C/sRFQKBgQDI/GuT3R8+CAj8AtL3um1aJ7J++pWUBzGJ
rtVtJh5qEMh883YmSbIkJs0jaXsNcsznINMPTgf6Ng5j9C0Tv6WjQa4wZkyPBG6e
jhb4HRUryolJC4CUHSGGr6RA2J8PVWKtF9EA37euw6DoSjYdzfDabbb1YVu/oezy
ZBkEgj04jwKBgQDUMMqTz8NwSK+v5O1pLPry5Rekqgso1my6tvZaSVhgvdIPMON9
BZL92c1yBmNureTtZ/U1MzGigAVaUjBbUa5dmf4SB8kuPHdtx4UZxTArsnsP8hXw
ecRV8Vi9cwzmIO6LPqahVPxP4gxxa1CXMY+106K+SOEKCGAUs39MXJxIHQKBgQCR
hUnyzmhfhnvS08yiNyYT36g6jf6dJjQ05xR6qd3dl/dBmRlTkYpc6Icg+69vxk4b
jsWiUDIwdNEoh9PXd6xbLyQKwRbvehsJzAFPdectRMDv1VcsZocuuJ9poC5ScNU4
VIUsZ87bx6MKbSkPnVulG0kcE3jVoE0qF1WR0Sa4ewKBgHPWBDmxhiwlUz3wjzbC
YOXWo8hf0A/7tzjD7qkAP6eWkRInKbjLOgOQNjoaVGCIadUl/ufLHs/Xp4HA5s6q
NHuyfgRvDdTmU+40/8YbivbKm076xfOH7E6ykwlJ5IfOm7TspJX/nKNXdAdASDbl
Ds68lK+MfzYYO4zNX3AvapdZ
-----END PRIVATE KEY-----
`

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.Logger(t)
	slogger := logger.Sugar()
	MQTTLogToZap(logger)
	return logger, slogger
}

func TestNewJSON(t *testing.T) {
	pemstr := strings.Replace(testPEM, "\n", "\\n", -1)
	credJSON := fmt.Sprintf(`
		{
		"project": "%s",
		"region": "%s",
		"registry": "%s",
		"device_id": "%s",
		"private_key": "%s"
		}`, testProject, testRegion, testRegistry, testDevice, pemstr)

	cred, err := NewCredentialFromJSON([]byte(credJSON))
	if err != nil {
		t.Errorf("Failed to make cred from JSON")
	}

	// Also exercise String() method of cred
	t.Logf("created cred %s", cred)
}

func TestBadJSON(t *testing.T) {
	// Mostly valid with a bad PEM
	badPEM := fmt.Sprintf(`{
		"project": "%s",
		"region": "%s",
		"registry": "%s",
		"device_id": "%s",
		"private_key": "nonsense"
		}`, testProject, testRegion, testRegistry, testDevice)

	for _, badJSON := range []string{"{}", "", "badjson", badPEM} {
		_, err := NewCredentialFromJSON([]byte(badJSON))
		if err == nil {
			t.Errorf("Unexpected success making cred from bad JSON")
			return
		}
		t.Logf("Saw expected error: %s", err)
	}

}

func setupClient(t *testing.T) (*IoTMQTTClient, error) {
	testKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(testPEM))
	if err != nil {
		return nil, err
	}
	cred := NewCredential(testProject, testRegion, testRegistry, testDevice, testKey)

	client, err := NewMQTTClient(cred)
	if err != nil {
		return nil, err
	}
	if token := client.Connect(); token.WaitTimeout(10*time.Second) == false || token.Error() != nil {
		return nil, fmt.Errorf("Connect failed or timed out: %s", token.Error())
	}
	// Also exercise String() method of client
	t.Logf("created and connected %s", client)
	return client, err
}

func TestConnection(t *testing.T) {
	_, _ = setupLogging(t)
	client, err := setupClient(t)
	if err != nil {
		t.Errorf("Failed to make client: %s", err)
		return
	}
	t.Logf("Connected")
	client.Disconnect(250)
	t.Logf("Disconnected")
}

func TestRefresh(t *testing.T) {
	_, _ = setupLogging(t)
	client, err := setupClient(t)
	if err != nil {
		t.Errorf("Failed to make client: %s", err)
		return
	}
	t.Logf("Connected")
	client.refreshJWT()
	if !client.IsConnected() {
		t.Errorf("Client is not connected after refresh")
		return
	}
	client.Disconnect(250)
	t.Logf("Disconnected")
}

func TestPublishEvent(t *testing.T) {
	_, _ = setupLogging(t)
	client, err := setupClient(t)
	if err != nil {
		t.Errorf("Failed to make client: %s", err)
		return
	}

	// No Subfolder
	text := "Test Event"
	token := client.PublishEvent("", 1, text)
	if token.WaitTimeout(2*time.Second) == false {
		t.Errorf("Publish timed out.")
		return
	}
	// Subfolder
	token = client.PublishEvent("testsubfolder", 1, text)
	if token.WaitTimeout(2*time.Second) == false {
		t.Errorf("Publish timed out.")
		return
	}
	client.Disconnect(250)
}

func TestPublishState(t *testing.T) {
	_, _ = setupLogging(t)
	client, err := setupClient(t)
	if err != nil {
		t.Errorf("Failed to make client: %s", err)
		return
	}

	text := "Test State"
	token := client.PublishState(1, text)
	if token.WaitTimeout(2*time.Second) == false {
		t.Errorf("Publish timed out.")
		return
	}
	client.Disconnect(250)
}

func TestConfig(t *testing.T) {
	_, _ = setupLogging(t)
	client, err := setupClient(t)
	if err != nil {
		t.Errorf("Failed to make client: %s", err)
		return
	}

	incoming := make(chan string)
	onConfig := func(client *IoTMQTTClient, msg mqtt.Message) {
		t.Logf("onConfig: saw %s: %s", msg.Topic(), string(msg.Payload()))
		incoming <- string(msg.Payload())
	}
	client.SubscribeConfig(onConfig)
	incomingMsg := <-incoming
	t.Logf("Received message '%s'", incomingMsg)
	if incomingMsg != testConfigPayload {
		t.Errorf("payload: %s != %s", incomingMsg, testConfigPayload)
		return
	}

	// We expect that refreshJWT will hangup the connection and then
	// reestablish it.  It will resubscribe onConfig.  So we expect to
	// see the config message again.
	client.refreshJWT()
	incomingMsg = <-incoming
	t.Logf("Received second message '%s'", incomingMsg)
	if incomingMsg != testConfigPayload {
		t.Errorf("payload: %s != %s", incomingMsg, testConfigPayload)
		return
	}
	client.Disconnect(250)
}
