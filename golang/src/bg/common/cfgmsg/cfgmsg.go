/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cfgmsg

import (
	"fmt"

	"bg/common/cfgapi"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
)

// APIVersion represents the cfgapi version.  This currently includes the
// inter-daemon API, appliance-to-cloud API, and the structure of the config
// file.  XXX: we really need to think about a more sane versioning scheme.
var APIVersion = Version{Major: cfgapi.Version}

var opToMsgType = map[int]ConfigOp_Operation{
	cfgapi.PropGet:    ConfigOp_GET,
	cfgapi.PropSet:    ConfigOp_SET,
	cfgapi.PropCreate: ConfigOp_CREATE,
	cfgapi.PropDelete: ConfigOp_DELETE,
	cfgapi.PropTest:   ConfigOp_TEST,
	cfgapi.PropTestEq: ConfigOp_TESTEQ,
}

// NewPingQuery generates a ping query
func NewPingQuery() *ConfigQuery {
	opType := ConfigOp_PING

	ops := []*ConfigOp{
		&ConfigOp{
			Property:  "ping",
			Operation: opType,
		},
	}
	query := ConfigQuery{
		Timestamp: ptypes.TimestampNow(),
		Debug:     "-",
		Version:   &APIVersion,
		Ops:       ops,
	}

	return &query
}

// NewPropQuery takes a slice of PropertyOp structures and creates a
// corresponding ConfigQuery protobuf
func NewPropQuery(ops []cfgapi.PropertyOp) (*ConfigQuery, error) {
	get := false
	msgOps := make([]*ConfigOp, len(ops))
	for i, op := range ops {
		get = get || (op.Op == cfgapi.PropGet)

		opType, ok := opToMsgType[op.Op]
		if !ok {
			return nil, cfgapi.ErrBadOp
		}

		var tspb *timestamp.Timestamp
		if op.Expires != nil {
			var err error
			tspb, err = ptypes.TimestampProto(*op.Expires)
			if err != nil {
				return nil, cfgapi.ErrBadTime
			}
		}
		msgOps[i] = &ConfigOp{
			Operation: opType,
			Property:  op.Name,
			Value:     op.Value,
			Expires:   tspb,
		}
	}
	if get && len(ops) > 1 {
		return nil, fmt.Errorf("GET ops must be singletons")
	}

	query := ConfigQuery{
		Timestamp: ptypes.TimestampNow(),
		Debug:     "-",
		Version:   &APIVersion,
		Ops:       msgOps,
	}

	return &query, nil
}

// Parse extracts the returned payload from a successful config operation.  For
// failed operations, it converts the error code embedded in the message into a
// Go error.
func (r *ConfigResponse) Parse() (string, error) {
	var msg string
	var err error

	switch r.Response {
	case ConfigResponse_OK:
		msg = r.Value
	case ConfigResponse_NOCMD:
		err = cfgapi.ErrBadCmd
	case ConfigResponse_QUEUED:
		err = cfgapi.ErrQueued
	case ConfigResponse_INPROGRESS:
		err = cfgapi.ErrInProgress
	case ConfigResponse_FAILED:
		err = fmt.Errorf("%v", r.Errmsg)
	case ConfigResponse_UNSUPPORTED:
		err = cfgapi.ErrBadOp
	case ConfigResponse_NOPROP:
		err = cfgapi.ErrNoProp
	case ConfigResponse_NOTEQUAL:
		err = cfgapi.ErrNotEqual
	case ConfigResponse_BADVERSION:
		var version string
		if r.MinVersion != nil {
			version = fmt.Sprintf("%d or greater", r.MinVersion.Major)
		} else {
			version = fmt.Sprintf("%d", r.Version.Major)
		}
		err = fmt.Errorf("ap.configd requires version %s",
			version)

	case ConfigResponse_BADTIME:
		err = cfgapi.ErrBadTime
	default:
		err = fmt.Errorf("bad response code: %d", r.Response)
	}

	return msg, err
}
