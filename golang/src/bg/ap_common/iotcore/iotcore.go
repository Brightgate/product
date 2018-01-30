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
	"log"
	"sync"
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
	PublishEvent(subfolder string, qos byte, payload interface{}) mqtt.Token
	PublishState(qos byte, payload interface{}) mqtt.Token
	SubscribeConfig(onConfig ConfigHandler)
	mqtt.Client
}

// _IoTMQTTClient is the default implementation of IoTMQTTClient
//
// XXX Presently, a missing feature in the MQTT.client means that we need to
// tear down and rebuild the client in order to change the password, and this
// class facilitates that as well.  This is highly disruptive, so it should
// be ripped out as soon as possible, or we should fix the upstream library.
// See https://github.com/eclipse/paho.mqtt.golang/issues/147.
//
type _IoTMQTTClient struct {
	cred        *IoTCredential
	signedJWT   string
	eventTopic  string
	stateTopic  string
	configTopic string
	onConfig    ConfigHandler
	mqttOpts    mqtt.ClientOptions
	mqtt.Client
	// Due to the above bug, sometimes Publish() calls might fail due to
	// Disconnect() triggered from our timer.  We use a lock to try to get
	// things to drain before doing the Disconnect() but it's not
	// guaranteed.  The lock guarantees that once we start the JWT refresh,
	// we won't submit any other commands.
	sync.RWMutex
}

// ConfigHandler represents a callback when a Config message is received
type ConfigHandler func(IoTMQTTClient, mqtt.Message)

// cJWTExpiry represents the number of seconds until the JWT is expired.
const cJWTExpiry = 3600

const cMQTTBrokerURI = "ssl://mqtt.googleapis.com:8883"
const cMQTTPingTimeout = 10 * time.Second
const cMQTTKeepAlive = 5 * time.Minute

func (c *_IoTMQTTClient) String() string {
	return fmt.Sprintf("_IoTMQTTClient cred:%v", c.cred)
}

// Periodically refresh the JWT token
func (c *_IoTMQTTClient) refreshJWT() {
	log.Printf("refreshJWT()\n")
	c.Lock()
	defer c.Unlock()
	signedJWT, err := c.cred.makeJWT()
	if err != nil {
		log.Printf("refreshJWT failed: %s\n", err)
		// Try again sooner.
		time.AfterFunc(time.Second*cJWTExpiry/10, c.refreshJWT)
		return
	}

	c.signedJWT = signedJWT
	// Switching this dynamically doesn't actually work; see
	// https://github.com/eclipse/paho.mqtt.golang/issues/147
	c.mqttOpts.SetPassword(c.signedJWT)

	connected := c.Client.IsConnected()
	if connected {
		// So instead we disconnect, make a new client, and reconnect
		log.Printf("refreshJWT() disconnecting client\n")
		c.Client.Disconnect(1000)
	}
	c.makeClient()
	if connected {
		log.Printf("refreshJWT() reconnecting client\n")
		if token := c.Client.Connect(); token.WaitTimeout(10*time.Second) && token.Error() != nil {
			panic(token.Error())
		}
		// Restore config subscription
		c.resubscribeConfig().WaitTimeout(10 * time.Second)
		log.Printf("refreshJWT() reconnected client\n")
	}
	time.AfterFunc(time.Second*cJWTExpiry/2, c.refreshJWT)
}

func (c *_IoTMQTTClient) makeClient() {
	// Setup MQTT client options
	opts := mqtt.NewClientOptions().AddBroker(cMQTTBrokerURI)
	opts.SetKeepAlive(cMQTTKeepAlive)

	opts.SetPingTimeout(cMQTTPingTimeout)

	opts.SetUsername("unused")
	opts.SetPassword(c.signedJWT)
	opts.SetClientID(c.cred.clientID())

	mqttc := mqtt.NewClient(opts)
	c.Client = mqttc
}

// NewMQTTClient will create a new Google Cloud IoT Core client.  It also
// exposes the mqtt.Client API.
func NewMQTTClient(cred *IoTCredential) (IoTMQTTClient, error) {
	var err error

	c := &_IoTMQTTClient{
		cred: cred,
	}

	// Call makeJWT (to try to drive a hard error if something is misconfigured)
	c.signedJWT, err = c.cred.makeJWT()
	if err != nil {
		return nil, err
	}
	c.eventTopic = fmt.Sprintf("/devices/%s/events", c.cred.DeviceID)
	c.stateTopic = fmt.Sprintf("/devices/%s/state", c.cred.DeviceID)
	c.configTopic = fmt.Sprintf("/devices/%s/config", c.cred.DeviceID)

	c.makeClient()
	// Start timer driver JWT refresh
	time.AfterFunc(time.Second*cJWTExpiry/2, c.refreshJWT)
	return IoTMQTTClient(c), nil
}

// PublishEvent publishes an event ('telemetry') to the IoT core broker
func (c *_IoTMQTTClient) PublishEvent(subfolder string, qos byte, payload interface{}) mqtt.Token {
	c.RLock()
	defer c.RUnlock()
	var t mqtt.Token
	if subfolder != "" {
		t = c.Publish(c.eventTopic+"/"+subfolder, qos, false, payload)
	} else {
		t = c.Publish(c.eventTopic, qos, false, payload)
	}
	return t
}

// PublishState publishes device state to the IoT core broker
func (c *_IoTMQTTClient) PublishState(qos byte, payload interface{}) mqtt.Token {
	c.RLock()
	defer c.RUnlock()
	t := c.Publish(c.stateTopic, 1, false, payload)
	return t
}

func (c *_IoTMQTTClient) resubscribeConfig() mqtt.Token {
	// At present IoT core has only one downstream topic.  So we can just
	// use the "default" mechanism to set a function to receive those
	// messages here.
	if c.onConfig == nil {
		return &mqtt.DummyToken{}
	}
	configClosure := func(client mqtt.Client, message mqtt.Message) {
		c.onConfig(IoTMQTTClient(c), message)
	}
	return c.Subscribe(c.configTopic, 1, configClosure)
}

// SubscribeConfig registers a receiver for configuration data and subscribes
// to the appropriate topic.
func (c *_IoTMQTTClient) SubscribeConfig(onConfig ConfigHandler) {
	c.RLock()
	defer c.RUnlock()
	c.onConfig = onConfig
	c.resubscribeConfig().Wait()
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
