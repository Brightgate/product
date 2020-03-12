/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package vaultgcpauth contains routines to help authenticate to Vault using
// the GCE-type GCP authentication.
package vaultgcpauth

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-hclog"
	vault "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/command/agent/auth"
	"github.com/hashicorp/vault/command/agent/auth/gcp"
	"github.com/hashicorp/vault/command/agent/sink"

	gommonlog "github.com/labstack/gommon/log"
)

func vaultAuthManual(glog *gommonlog.Logger, vc *vault.Client, path, role string) error {
	baseURL := "http://metadata/computeMetadata/v1/instance/service-accounts/default/identity"
	u, err := url.Parse(baseURL)
	if err != nil {
		panic(err)
	}
	urlData := u.Query()
	urlData.Set("audience", "vault/"+role)
	urlData.Set("format", "full")
	u.RawQuery = urlData.Encode()

	request, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}

	jwt, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	vLoginURL := fmt.Sprintf("/v1/%s/login", path)
	vReq := vc.NewRequest(http.MethodPost, vLoginURL)
	vReq.BodyBytes = []byte(fmt.Sprintf(`{"role": "%s", "jwt": "%s"}`, role, jwt))
	vResp, err := vc.RawRequest(vReq)
	if err != nil {
		return err
	}
	secret, err := vault.ParseSecret(vResp.Body)
	if err != nil {
		return err
	}
	token, err := secret.TokenID()
	if err != nil {
		return err
	}
	vc.SetToken(token)

	return nil
}

// This has to match the Logger interface from hashicorp/go-hclog.
type gommonHCLogAdapter struct {
	glog *gommonlog.Logger

	implied []interface{}
}

func hcLevelToGommon(level hclog.Level) gommonlog.Lvl {
	switch level {
	case hclog.Trace, hclog.Debug:
		return gommonlog.DEBUG
	case hclog.Info:
		return gommonlog.INFO
	case hclog.Warn:
		return gommonlog.WARN
	case hclog.Error:
		return gommonlog.ERROR
	}
	return gommonlog.OFF
}

func (l *gommonHCLogAdapter) Log(level hclog.Level, msg string, args ...interface{}) {
	var j gommonlog.JSON = make(map[string]interface{})
	j["message"] = msg
	args = append(l.implied, args...)
	for i := 0; i < len(args); i += 2 {
		switch k := args[i].(type) {
		case string:
			j[k] = args[i+1]
		default:
			j[fmt.Sprintf("%s", k)] = args[i+1]
		}
	}

	switch level {
	case hclog.Trace, hclog.Debug:
		l.glog.Debugj(j)
	case hclog.Info:
		l.glog.Infoj(j)
	case hclog.Warn:
		l.glog.Warnj(j)
	case hclog.Error:
		l.glog.Errorj(j)
	case hclog.NoLevel:
		l.glog.Printj(j)
	}
}

func (l *gommonHCLogAdapter) Trace(msg string, args ...interface{}) {
	l.Log(hclog.Trace, msg, args...)
}

func (l *gommonHCLogAdapter) Debug(msg string, args ...interface{}) {
	l.Log(hclog.Debug, msg, args...)
}

func (l *gommonHCLogAdapter) Info(msg string, args ...interface{}) {
	l.Log(hclog.Info, msg, args...)
}

func (l *gommonHCLogAdapter) Warn(msg string, args ...interface{}) {
	l.Log(hclog.Warn, msg, args...)
}

func (l *gommonHCLogAdapter) Error(msg string, args ...interface{}) {
	l.Log(hclog.Error, msg, args...)
}

func (l *gommonHCLogAdapter) IsTrace() bool {
	return l.IsDebug()
}

func (l *gommonHCLogAdapter) IsDebug() bool {
	return l.glog.Level() == gommonlog.DEBUG
}

func (l *gommonHCLogAdapter) IsInfo() bool {
	return l.glog.Level() <= gommonlog.INFO
}

func (l *gommonHCLogAdapter) IsWarn() bool {
	return l.glog.Level() <= gommonlog.WARN
}

func (l *gommonHCLogAdapter) IsError() bool {
	return l.glog.Level() <= gommonlog.ERROR
}

func (l *gommonHCLogAdapter) ImpliedArgs() []interface{} {
	return l.implied
}

func (l *gommonHCLogAdapter) With(args ...interface{}) hclog.Logger {
	var extra interface{}
	if len(args)%2 != 0 {
		extra = args[len(args)-1]
		args = args[:len(args)-1]
	}
	newLogger := *l
	newLogger.implied = append(newLogger.implied, args...)
	if extra != nil {
		newLogger.implied = append(newLogger.implied, hclog.MissingKey, extra)
	}

	return &newLogger
}

func (l *gommonHCLogAdapter) Name() string {
	return l.glog.Prefix()
}

func (l *gommonHCLogAdapter) Named(name string) hclog.Logger {
	oldPrefix := l.glog.Prefix()
	var prefix string
	if oldPrefix != "" {
		prefix = oldPrefix + "." + name
	} else {
		prefix = name
	}
	return gommonToHCLog(l.glog, prefix)
}

func (l *gommonHCLogAdapter) ResetNamed(name string) hclog.Logger {
	return gommonToHCLog(l.glog, name)
}

func (l *gommonHCLogAdapter) SetLevel(level hclog.Level) {
	l.glog.SetLevel(hcLevelToGommon(level))
}

func (l *gommonHCLogAdapter) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	if opts == nil {
		opts = &hclog.StandardLoggerOptions{}
	}
	return log.New(l.StandardWriter(opts), "", 0)
}

func (l *gommonHCLogAdapter) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	// This would look a lot like go-hclog/stdlog.go
	panic("StandardWriter not implemented")
	return nil
}

func gommonToHCLog(glog *gommonlog.Logger, prefix string) hclog.Logger {
	nlog := gommonlog.New(prefix)
	nlog.SetLevel(glog.Level())
	return &gommonHCLogAdapter{
		glog: nlog,
	}
}

type vaultClientSink struct {
	client *vault.Client
	log    *gommonlog.Logger
}

func (s *vaultClientSink) WriteToken(token string) error {
	s.client.SetToken(token)
	return nil
}

// VaultAuth sets up authentication to Vault using GCP.  It will renew the token
// until expiration, and then fetch a new one.  The tokens will be set on the
// passed-in Vault client.
//
// XXX httpd uses gommon.log and rpcd (and others) use zap.  They'll need to
// pre-convert to an hclog.Logger.
func VaultAuth(ctx context.Context, glog *gommonlog.Logger, vc *vault.Client, path, role string) error {
	hclogger := gommonToHCLog(glog, "vault-authenticator")
	authConfig := &auth.AuthConfig{
		Logger:    hclogger,
		MountPath: path,
		Config: map[string]interface{}{
			"type":            "gce",
			"role":            role,
			"service_account": "default", // configurable?
		},
	}
	authMethod, err := gcp.NewGCPAuthMethod(authConfig)
	if err != nil {
		return err
	}

	authHandlerConfig := &auth.AuthHandlerConfig{
		Client:                       vc,
		Logger:                       hclogger,
		EnableReauthOnNewCredentials: true,
	}
	authHandler := auth.NewAuthHandler(authHandlerConfig)

	vcSink := &vaultClientSink{client: vc, log: glog}
	sinkConfig := &sink.SinkConfig{
		Client: vc,
		Logger: hclogger,
		Sink:   vcSink,
	}

	ssConfig := &sink.SinkServerConfig{
		Context: ctx,
		Client:  vc,
		Logger:  hclogger,
	}
	sinkServer := sink.NewSinkServer(ssConfig)

	go authHandler.Run(ctx, authMethod)
	go sinkServer.Run(ctx, authHandler.OutputCh, []*sink.SinkConfig{sinkConfig})

	// Don't return until we've gotten our first token.
	for count := 0; vc.Token() == ""; count++ {
		time.Sleep(250 * time.Millisecond)
		if count > 20 {
			return fmt.Errorf("Couldn't authenticate to Vault within 5 seconds")
		}
	}

	return nil
}
