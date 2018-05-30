/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cloudiotsvc

import (
	"context"
	"fmt"
	"net/http"

	"cloud.google.com/go/pubsub"
	"github.com/pkg/errors"
	"golang.org/x/oauth2/google"
	cloudiot "google.golang.org/api/cloudiot/v1"
)

// Service facilitates mocking the IoT service
type Service interface {
	GetRegistry() (*cloudiot.DeviceRegistry, error)
	GetDevice(string) (*cloudiot.Device, error)
	SubscribeEvents(ctx context.Context) (*pubsub.Subscription, error)
	SubscribeState(ctx context.Context) (*pubsub.Subscription, error)
}

type serviceImpl struct {
	oauthHTTPClient *http.Client
	pubsubClient    *pubsub.Client
	project         string
	region          string
	registry        string
	cloudIoT        *cloudiot.Service
	plrSvc          *cloudiot.ProjectsLocationsRegistriesService
}

func newServiceImpl(ctx context.Context, oauthHTTPClient *http.Client,
	pubsubClient *pubsub.Client, project, region, registry string) (*serviceImpl, error) {

	googlesvc, err := cloudiot.New(oauthHTTPClient)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to make Cloud IoT Service")
	}
	plrs := cloudiot.NewProjectsLocationsRegistriesService(googlesvc)

	svc := &serviceImpl{
		oauthHTTPClient: oauthHTTPClient,
		pubsubClient:    pubsubClient,
		project:         project,
		region:          region,
		registry:        registry,
		cloudIoT:        googlesvc,
		plrSvc:          plrs,
	}
	return svc, nil
}

// NewDefaultService creates a service using Google Application Default
// Credentials (see https://cloud.google.com/docs/authentication/production)
func NewDefaultService(ctx context.Context,
	project, region, registry string) (Service, error) {
	var svc Service
	var err error

	oauthHTTPClient, err := google.DefaultClient(ctx,
		"https://www.googleapis.com/auth/cloudiot")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to make http client")
	}
	pubsubClient, err := pubsub.NewClient(ctx, project)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to make pubsub client")
	}

	svc, err = newServiceImpl(ctx, oauthHTTPClient, pubsubClient,
		project, region, registry)
	return svc, err
}

// NewService allows plugging in alternate credentials, such as we
// use in our test suite.
func NewService(ctx context.Context,
	oauthHTTPClient *http.Client, pubsubClient *pubsub.Client,
	project, region, registry string) (Service, error) {

	var svc Service
	var err error
	svc, err = newServiceImpl(ctx, oauthHTTPClient, pubsubClient,
		project, region, registry)
	return svc, err
}

// GetRegistry returns the Service's registry configuration
func (c *serviceImpl) GetRegistry() (*cloudiot.DeviceRegistry, error) {
	id := fmt.Sprintf("projects/%s/locations/%s/registries/%s",
		c.project, c.region, c.registry)
	return c.plrSvc.Get(id).Do()
}

// GetDevice returns the device record for a device in the registry
func (c *serviceImpl) GetDevice(deviceID string) (*cloudiot.Device, error) {
	id := fmt.Sprintf("projects/%s/locations/%s/registries/%s/devices/%s",
		c.project, c.region, c.registry, deviceID)
	return c.plrSvc.Devices.Get(id).Do()
}

func (c *serviceImpl) subscribeCommon(ctx context.Context,
	subName, topicName string) (*pubsub.Subscription, error) {
	var sub *pubsub.Subscription

	sub = c.pubsubClient.Subscription(subName)
	ok, err := sub.Exists(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to test if subscription %s exists", subName)
	}
	if !ok {
		topic := c.pubsubClient.Topic(topicName)
		sub, err = c.pubsubClient.CreateSubscription(ctx, subName,
			pubsub.SubscriptionConfig{Topic: topic})
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to CreateSubscription %s", subName)
		}
	}
	return sub, nil
}

// SubscribeEvents creates (if needed) and returns a pubsub Subscription for the events
// topic for the Service's registry
func (c *serviceImpl) SubscribeEvents(ctx context.Context) (*pubsub.Subscription, error) {
	// The following implements our convention for topics and subscription names
	topicName := "iot-" + c.registry + "-events"
	subName := topicName + "-cl-eventd"
	return c.subscribeCommon(ctx, subName, topicName)
}

// SubscribeState creates (if needed) and returns a pubsub Subscription for the states
// topic for the Service's registry
func (c *serviceImpl) SubscribeState(ctx context.Context) (*pubsub.Subscription, error) {
	// The following implements our convention for topics and subscription names
	topicName := "iot-" + c.registry + "-state"
	subName := topicName + "-cl-eventd"
	return c.subscribeCommon(ctx, subName, topicName)
}
