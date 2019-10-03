/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/certificate"
	"github.com/go-acme/lego/challenge"
	"github.com/go-acme/lego/challenge/dns01"
	"github.com/go-acme/lego/lego"
	legolog "github.com/go-acme/lego/log"
	dnsexec "github.com/go-acme/lego/providers/dns/exec"
	dnsgoog "github.com/go-acme/lego/providers/dns/gcloud"

	"go.uber.org/zap"
)

// LegoHandler is an interface that abstracts what we need out of lego.
type LegoHandler interface {
	obtain(certificate.ObtainRequest) (*certificate.Resource, error)
	getPoolSize() int
	getPoolFillAmount() int
	getExpirationOverride() time.Duration
	getGracePeriod() time.Duration
	getLimiter() *time.Ticker
	createMap([]string)
	getToken(string) string
	getDomains(string) []string
}

type legoHandle struct {
	client             *lego.Client
	poolSize           int
	poolFill           int
	expirationOverride time.Duration
	gracePeriod        time.Duration

	// Maps domains to a string that uniquely identifies the order that
	// encompasses those domains.
	solveToken map[string]string
	// The reverse mapping: a map of the order identifier to the domains
	// that belong to that order.
	revSolveToken map[string][]string
	// A lock covering both the above maps.
	sync.Mutex
}

func (h *legoHandle) obtain(request certificate.ObtainRequest) (*certificate.Resource, error) {
	return h.client.Certificate.Obtain(request)
}

func (h *legoHandle) getPoolSize() int {
	return h.poolSize
}

func (h *legoHandle) getPoolFillAmount() int {
	return h.poolFill
}

func (h *legoHandle) getExpirationOverride() time.Duration {
	return h.expirationOverride
}

func (h *legoHandle) getGracePeriod() time.Duration {
	return h.gracePeriod
}

func (h *legoHandle) getLimiter() *time.Ticker {
	// The Let's Encrypt endpoints can only be hit 20 times a second.
	return time.NewTicker(time.Second / 20)
}

func (h *legoHandle) createMap(domains []string) {
	tok := domains[0] // Can be whatever, as long as it's unique

	h.Lock()
	defer h.Unlock()

	// Forward mapping
	for _, domain := range domains {
		h.solveToken[domain] = tok
	}

	// Reverse mapping
	h.revSolveToken[tok] = domains
}

func (h *legoHandle) getToken(domain string) string {
	h.Lock()
	defer h.Unlock()
	return h.solveToken[domain]
}

func (h *legoHandle) getDomains(token string) []string {
	h.Lock()
	defer h.Unlock()
	return h.revSolveToken[token]
}

func newLegoHandle(client *lego.Client) *legoHandle {
	return &legoHandle{
		client:             client,
		poolSize:           environ.PoolSize,
		poolFill:           environ.PoolFillAmount,
		gracePeriod:        time.Duration(environ.GracePeriod),
		expirationOverride: time.Duration(environ.ExpirationOverride),
		solveToken:         make(map[string]string),
		revSolveToken:      make(map[string][]string),
	}
}

// LegoLog wraps a zap.SugaredLogger to provide the interface that Lego's logger
// wants, so that we can replace it, and have all the logs come through a single
// stream.
type LegoLog struct {
	slog *zap.SugaredLogger
}

// Fatal implements lego/log.StdLogger
func (ll LegoLog) Fatal(args ...interface{}) {
	ll.slog.Fatal(args...)
}

// Fatalf implements lego/log.StdLogger
func (ll LegoLog) Fatalf(format string, args ...interface{}) {
	ll.slog.Fatalf(format, args...)
}

// Fatalln implements lego/log.StdLogger
func (ll LegoLog) Fatalln(args ...interface{}) {
	args = append(args, "\n")
	ll.slog.Fatal(args...)
}

// Print implements lego/log.StdLogger
func (ll LegoLog) Print(args ...interface{}) {
	ll.slog.Info(args...)
}

// Printf implements lego/log.StdLogger.  Since lego's default logger prepends
// the log level to the message itself, we extract that and use it to determine
// the right zap logging level.
func (ll LegoLog) Printf(format string, args ...interface{}) {
	if strings.HasPrefix(format, "[INFO] ") {
		ll.slog.Infof(format[7:], args...)

		// We have no other way of getting at this information.
		// See https://github.com/xenolf/lego/issues/771
		if strings.Contains(format, " AuthURL: ") {
			url, ok := args[1].(string)
			if ok {
				authURLs = append(authURLs, url)
			}
		}
	} else if strings.HasPrefix(format, "[WARN] ") {
		ll.slog.Warnf(format[7:], args...)
	} else {
		ll.slog.Infof(format, args...)
	}
}

// Println implements lego/log.StdLogger
func (ll LegoLog) Println(args ...interface{}) {
	args = append(args, "\n")
	ll.slog.Info(args...)
}

func legoSetup() (*legoHandle, *lego.Config) {
	legolog.Logger = LegoLog{slog}

	config, client, err := acmeSetup(environ.AcmeConfig, environ.AcmeURL)
	if err != nil {
		slog.Fatalw("Failed to set up ACME connection info", "error", err)
	}
	lh := newLegoHandle(client)

	var provider challenge.Provider
	if environ.DNSExec != "" {
		provider, err = dnsexec.NewDNSProviderConfig(
			&dnsexec.Config{Program: environ.DNSExec})
	} else {
		provider, err = dnsgoog.NewDNSProviderServiceAccount(
			environ.DNSCredFile)
	}
	if err != nil {
		slog.Fatalw("Failed to set DNS challenge provider", "error", err)
	}

	challengeOptions := make([]dns01.ChallengeOption, 0)
	if environ.DNSSkipPreCheck {
		challengeOptions = append(challengeOptions,
			dns01.AddPreCheck(func(fqdn, value string) (bool, error) {
				return true, nil
			}))
	} else {
		solvedMap := make(map[string]bool)
		var solvedMapLock sync.Mutex
		wrapFunc := func(domain, fqdn, value string, orig dns01.PreCheckFunc) (bool, error) {
			token := lh.getToken(domain)
			solvedMapLock.Lock()
			solved := solvedMap[token]
			solvedMapLock.Unlock()
			if !solved {
				domains := lh.getDomains(token)
				delay := time.Duration(environ.DNSDelayPreCheck) * time.Second
				slog.Infof("[%s] Waiting %s before checking DNS propagation",
					strings.Join(domains, ", "), delay.Round(time.Second))
				time.Sleep(delay)
				solvedMapLock.Lock()
				solvedMap[token] = true
				solvedMapLock.Unlock()
			}
			return orig(fqdn, value)
		}
		challengeOptions = append(challengeOptions, dns01.WrapPreCheck(wrapFunc))
	}

	if environ.RecursiveNameserver != "" {
		challengeOptions = append(challengeOptions,
			dns01.AddRecursiveNameservers(
				[]string{environ.RecursiveNameserver}))
	}

	err = client.Challenge.SetDNS01Provider(provider, challengeOptions...)
	if err != nil {
		slog.Fatalw("Failed to set DNS challenge provider", "error", err)
	}
	slog.Info(checkMark + "Set up ACME connection info for " + environ.AcmeURL)

	return lh, config
}
