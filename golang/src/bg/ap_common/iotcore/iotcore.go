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
	"crypto/rsa"
	"fmt"
	"log"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// IoTMQTTClient is an extension of MQTT.client, adding additional information
// needed for Google IoT Core clients.  This includes a set of parameters
// needed to build the ClientID, as well as data required to build the JWT
// which is used as the connection password.  The JWT must periodically be
// refreshed, and this structure allows that to happen
//
// XXX Presently, a missing feature in the MQTT.client means that we need to
// tear down and rebuild the client in order to change the password, and this
// class facilitates that as well.  This is highly disruptive, so it should
// be ripped out as soon as possible, or we should fix the upstream library.
//
type IoTMQTTClient struct {
	broker                string
	project               string
	region                string
	registryID            string
	deviceID              string
	privKey               *rsa.PrivateKey
	signedJWT             string
	clientID              string // computed from above
	mqttOpts              mqtt.ClientOptions
	defaultPublishHandler func(mqtt.Client, mqtt.Message)
	mqtt.Client
}

// JWTExpiry represents the number of seconds until the JWT is expired.
const JWTExpiry = 3600

func (c *IoTMQTTClient) String() string {
	return fmt.Sprintf("IoTMQTTClient %v", c.clientID)
}

func (c *IoTMQTTClient) makeJWT() (string, error) {
	jwtSigningMethod := jwt.GetSigningMethod("RS256")
	if jwtSigningMethod == nil {
		return "", errors.New("Couldn't find signing method")
	}
	jwtClaims := &jwt.StandardClaims{
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Unix() + JWTExpiry,
		Audience:  c.project,
	}
	j := jwt.NewWithClaims(jwtSigningMethod, jwtClaims)
	signedJWT, err := j.SignedString(c.privKey)
	if err != nil {
		return "", errors.Wrap(err, "Couldn't sign JWT")
	}
	return signedJWT, nil
}

// Periodically refresh the JWT token
func (c *IoTMQTTClient) refreshJWT() {
	log.Printf("refreshJWT()\n")
	signedJWT, err := c.makeJWT()
	if err != nil {
		log.Printf("refreshJWT failed: %s\n", err)
		// Try again sooner.
		time.AfterFunc(time.Second*JWTExpiry/10, c.refreshJWT)
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
		if token := c.Client.Connect(); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}
	}
	time.AfterFunc(time.Second*JWTExpiry/2, c.refreshJWT)
}

func (c *IoTMQTTClient) makeClient() {
	// Setup MQTT client options
	opts := mqtt.NewClientOptions().AddBroker("ssl://mqtt.googleapis.com:8883")
	opts.SetKeepAlive(5 * time.Minute)
	opts.SetDefaultPublishHandler(c.defaultPublishHandler)
	opts.SetPingTimeout(10 * time.Second)

	opts.SetUsername("unused")
	opts.SetPassword(c.signedJWT)
	opts.SetClientID(c.clientID)

	mqttc := mqtt.NewClient(opts)
	c.Client = mqttc
}

// NewMQTTClient will create a new Google Cloud IoT Core client.  It also
// exposes the mqtt.Client API.
//
// XXX might need to work on defaultPublishHandler more
// XXX rework to take private key as bytes
func NewMQTTClient(project string, region string, registryID string,
	deviceID string, privKey *rsa.PrivateKey,
	defaultPublishHandler func(mqtt.Client, mqtt.Message)) (mqtt.Client, error) {

	var err error

	clientID := fmt.Sprintf(
		"projects/%s/locations/%s/registries/%s/devices/%s",
		project, region, registryID, deviceID)

	c := &IoTMQTTClient{
		project:               project,
		region:                region,
		registryID:            registryID,
		deviceID:              deviceID,
		privKey:               privKey,
		clientID:              clientID,
		defaultPublishHandler: defaultPublishHandler,
	}

	// Call makeJWT (to try to drive a hard error if something is misconfigured)
	c.signedJWT, err = c.makeJWT()
	if err != nil {
		return nil, err
	}
	c.makeClient()
	// Start timer driver JWT refresh
	time.AfterFunc(time.Second*JWTExpiry/2, c.refreshJWT)
	return c, nil
}

// MQTTLogToZap is a convenience function for connecting the MQTT's somewhat
// clunky logger to Zap.
func MQTTLogToZap(logger *zap.Logger) {
	mqtt.DEBUG, _ = zap.NewStdLogAt(logger, zapcore.DebugLevel)
	// mqtt's WARN msgs aren't very helpful, and trigger stack traces with
	// the default config, so we use InfoLevel
	mqtt.WARN, _ = zap.NewStdLogAt(logger, zapcore.InfoLevel)
	mqtt.ERROR, _ = zap.NewStdLogAt(logger, zapcore.ErrorLevel)
	mqtt.CRITICAL, _ = zap.NewStdLogAt(logger, zapcore.PanicLevel)
}
