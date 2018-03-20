/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
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
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// IoTMQTTClient is an extension of MQTT.client, adding additional information
// needed for Google IoT Core MQTT clients (see also
// https://cloud.google.com/iot/docs/how-tos/mqtt-bridge and
// http://docs.oasis-open.org/mqtt/mqtt/v3.1.1/os/mqtt-v3.1.1-os.html).  This
// includes a set of parameters needed to build the ClientID, as well as data
// required to build the JSON Web Token (JWT, https://jwt.io/, RFC 7519) which
// is used as the connection password.  The JWT must periodically be refreshed,
// and this structure allows that to happen
type IoTMQTTClient interface {
	PublishEvent(subfolder string, payload interface{}) mqtt.Token
	PublishState(payload interface{}) mqtt.Token
	SubscribeConfig(onConfig ConfigHandler)
	mqtt.Client
}

// TransportOpts is used to set MQTT related options when constructing a client
type TransportOpts struct {
	StoreDir  string
	StateQOS  byte
	EventQOS  byte
	ConfigQOS byte
}

// DefaultTransportOpts is used to adopt default transport options.
var DefaultTransportOpts = TransportOpts{
	StoreDir:  "",
	StateQOS:  1,
	EventQOS:  1,
	ConfigQOS: 1,
}

// _IoTMQTTClient is the default implementation of IoTMQTTClient
type _IoTMQTTClient struct {
	cred          *IoTCredential
	transportOpts TransportOpts
	signedJWT     string
	eventTopic    string
	stateTopic    string
	configTopic   string
	onConfig      ConfigHandler
	mqttOpts      mqtt.ClientOptions
	mqtt.Client
}

// ConfigHandler represents a callback when a Config message is received
type ConfigHandler func(IoTMQTTClient, mqtt.Message)

// cJWTExpiry represents the number of seconds until the JWT is expired.
const cJWTExpiry = 1 * time.Hour

const cMQTTBrokerURI = "ssl://mqtt.googleapis.com:8883"
const cMQTTPingTimeout = 10 * time.Second
const cMQTTKeepAlive = 5 * time.Minute
const cMQTTUsername = "unused"

func (c *_IoTMQTTClient) String() string {
	return fmt.Sprintf("_IoTMQTTClient cred:%v", c.cred)
}

// NewMQTTClient will create a new Google Cloud IoT Core client.  It also
// exposes the mqtt.Client API.
func NewMQTTClient(cred *IoTCredential, transportOpts TransportOpts) (IoTMQTTClient, error) {
	var err error

	c := &_IoTMQTTClient{
		cred:          cred,
		transportOpts: transportOpts,
	}

	// Call makeJWT (to try to drive a hard error if something is misconfigured)
	_, err = c.cred.makeJWT()
	if err != nil {
		return nil, err
	}
	mqtt.DEBUG.Printf("NewMQTTClient:\n\t%+v\n\t%+v\n", cred, transportOpts)

	c.eventTopic = fmt.Sprintf("/devices/%s/events", c.cred.DeviceID)
	c.stateTopic = fmt.Sprintf("/devices/%s/state", c.cred.DeviceID)
	c.configTopic = fmt.Sprintf("/devices/%s/config", c.cred.DeviceID)

	// Setup MQTT client options
	opts := mqtt.NewClientOptions().AddBroker(cMQTTBrokerURI)
	opts.SetKeepAlive(cMQTTKeepAlive)

	opts.SetPingTimeout(cMQTTPingTimeout)

	opts.SetClientID(c.cred.clientID())
	opts.SetAutoReconnect(true)

	// We need to provide a new password whenever we get timed out due to
	// the credential expiring.
	opts.SetCredentialsProvider(func() (username string, password string) {
		signedJWT, err := c.cred.makeJWT()
		if err != nil {
			mqtt.ERROR.Printf("credentialProvider: failed to build JWT")
			return cMQTTUsername, ""
		}
		mqtt.DEBUG.Printf("credentialProvider: provided updated creds")
		return cMQTTUsername, signedJWT
	})

	opts.SetConnectionLostHandler(func(client mqtt.Client, e error) {
		mqtt.DEBUG.Printf("Connection Lost: %v", e)
	})

	if c.transportOpts.StoreDir != "" {
		opts.SetStore(mqtt.NewFileStore(c.transportOpts.StoreDir))
	}

	c.Client = mqtt.NewClient(opts)
	return IoTMQTTClient(c), nil
}

// PublishEvent publishes an event ('telemetry') to the IoT core broker
func (c *_IoTMQTTClient) PublishEvent(subfolder string, payload interface{}) mqtt.Token {
	var top string
	if subfolder != "" {
		top = c.eventTopic + "/" + subfolder
	} else {
		top = c.eventTopic
	}
	return c.Publish(top, c.transportOpts.EventQOS, false, payload)
}

// PublishState publishes device state to the IoT core broker
func (c *_IoTMQTTClient) PublishState(payload interface{}) mqtt.Token {
	t := c.Publish(c.stateTopic, c.transportOpts.StateQOS, false, payload)
	return t
}

// SubscribeConfig registers a receiver for configuration data and subscribes
// to the appropriate topic.
func (c *_IoTMQTTClient) SubscribeConfig(onConfig ConfigHandler) {
	c.onConfig = onConfig
	configClosure := func(client mqtt.Client, message mqtt.Message) {
		c.onConfig(IoTMQTTClient(c), message)
	}
	c.Subscribe(c.configTopic, c.transportOpts.ConfigQOS, configClosure).Wait()
	return
}

// MQTTLogToZap is a convenience function for connecting the MQTT's somewhat
// clunky logger to Zap.
func MQTTLogToZap(logger *zap.Logger) {
	mqtt.DEBUG, _ = zap.NewStdLogAt(logger, zapcore.DebugLevel)
	// mqtt's WARN msgs aren't very helpful, and trigger stack traces with
	// the default config, so we use DebugLevel
	mqtt.WARN, _ = zap.NewStdLogAt(logger, zapcore.DebugLevel)
	mqtt.ERROR, _ = zap.NewStdLogAt(logger, zapcore.ErrorLevel)
	mqtt.CRITICAL, _ = zap.NewStdLogAt(logger, zapcore.PanicLevel)
}
