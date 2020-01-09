/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cfgapi

import (
	"fmt"

	"bg/common/cfgmsg"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
)

var (
	errToCode = map[error]cfgmsg.ConfigResponse_OpResponse{
		ErrBadCmd:     cfgmsg.ConfigResponse_NOCMD,
		ErrQueued:     cfgmsg.ConfigResponse_QUEUED,
		ErrInProgress: cfgmsg.ConfigResponse_INPROGRESS,
		ErrBadOp:      cfgmsg.ConfigResponse_UNSUPPORTED,
		ErrNoProp:     cfgmsg.ConfigResponse_NOPROP,
		ErrBadTime:    cfgmsg.ConfigResponse_BADTIME,
		ErrBadVer:     cfgmsg.ConfigResponse_BADVERSION,
		ErrNotEqual:   cfgmsg.ConfigResponse_NOTEQUAL,
		ErrNoConfig:   cfgmsg.ConfigResponse_NOCONFIG,
		ErrBadTree:    cfgmsg.ConfigResponse_BADTREE,
	}

	apiToMsg = map[int]cfgmsg.ConfigOp_Operation{
		PropGet:           cfgmsg.ConfigOp_GET,
		PropSet:           cfgmsg.ConfigOp_SET,
		PropCreate:        cfgmsg.ConfigOp_CREATE,
		PropDelete:        cfgmsg.ConfigOp_DELETE,
		PropTest:          cfgmsg.ConfigOp_TEST,
		PropTestEq:        cfgmsg.ConfigOp_TESTEQ,
		AddPropValidation: cfgmsg.ConfigOp_ADDVALID,
		TreeReplace:       cfgmsg.ConfigOp_REPLACE,
	}

	codeToErr map[cfgmsg.ConfigResponse_OpResponse]error
	msgToAPI  map[cfgmsg.ConfigOp_Operation]int

	// CfgmsgVersion is the API version packed for wire transport
	CfgmsgVersion = cfgmsg.Version{Major: Version, Minor: 0}
)

// GenerateConfigResponse takes a return-value string and an error, and
// constructs a cfgmsg.ConfigResponse protobuf that can be transmitted over the
// wire.
func GenerateConfigResponse(rval string, err error) *cfgmsg.ConfigResponse {
	r := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		Version:   &CfgmsgVersion,
		Debug:     "-",
	}

	if err == nil {
		r.Response = cfgmsg.ConfigResponse_OK
		r.Value = rval
	} else if code, ok := errToCode[err]; ok {
		r.Response = code
		r.Errmsg = rval
		if code == cfgmsg.ConfigResponse_BADVERSION {
			r.Version.Major = Version
		}
	} else {
		r.Response = cfgmsg.ConfigResponse_FAILED
		r.Errmsg = fmt.Sprintf("%v", err)
	}

	return r
}

// ParseConfigResponse examines a cfgmsg.ConfigResponse protobuf, and returns a
// return-value string and/or an error.
func ParseConfigResponse(r *cfgmsg.ConfigResponse) (string, error) {
	var rval string
	var err error

	code := r.Response
	switch code {

	case cfgmsg.ConfigResponse_OK:
		rval = r.Value

	case cfgmsg.ConfigResponse_FAILED:
		err = fmt.Errorf("%v", r.Errmsg)

	case cfgmsg.ConfigResponse_BADVERSION:
		var v string
		if r.MinVersion != nil {
			v = fmt.Sprintf("%d or greater", r.MinVersion.Major)
		} else {
			v = fmt.Sprintf("%d", r.Version.Major)
		}
		err = fmt.Errorf("ap.configd requires version %s", v)

	default:
		if rerr, ok := codeToErr[code]; ok {
			err = rerr
		} else {
			err = fmt.Errorf("bad response code: %d", code)
		}
	}

	return rval, err
}

// NewPingQuery generates a ping query
func NewPingQuery() *cfgmsg.ConfigQuery {
	opType := cfgmsg.ConfigOp_PING

	ops := []*cfgmsg.ConfigOp{
		&cfgmsg.ConfigOp{
			Property:  "ping",
			Operation: opType,
		},
	}
	query := cfgmsg.ConfigQuery{
		Timestamp: ptypes.TimestampNow(),
		Debug:     "-",
		Version:   &CfgmsgVersion,
		Ops:       ops,
	}

	return &query
}

// PropOpsToQuery takes a slice of PropertyOp structures and creates a
// corresponding ConfigQuery protobuf
func PropOpsToQuery(ops []PropertyOp) (*cfgmsg.ConfigQuery, error) {
	get := false
	msgOps := make([]*cfgmsg.ConfigOp, len(ops))
	for i, op := range ops {
		get = get || (op.Op == PropGet)

		opType, ok := apiToMsg[op.Op]
		if !ok {
			return nil, ErrBadOp
		}

		var tspb *timestamp.Timestamp
		if op.Expires != nil {
			var err error
			tspb, err = ptypes.TimestampProto(*op.Expires)
			if err != nil {
				return nil, ErrBadTime
			}
		}
		msgOps[i] = &cfgmsg.ConfigOp{
			Operation: opType,
			Property:  op.Name,
			Value:     op.Value,
			Expires:   tspb,
		}
	}
	if get && len(ops) > 1 {
		return nil, fmt.Errorf("GET ops must be singletons")
	}

	query := cfgmsg.ConfigQuery{
		Timestamp: ptypes.TimestampNow(),
		Debug:     "-",
		Version:   &CfgmsgVersion,
		Ops:       msgOps,
	}

	return &query, nil
}

// QueryToPropOps translates a cfgmsg protobuf into the equivalent slice of
// cfgapi operations.
func QueryToPropOps(q *cfgmsg.ConfigQuery) ([]PropertyOp, error) {
	var err error

	ops := make([]PropertyOp, 0)
	for _, cfgOp := range q.Ops {
		cfgOpcode, ok := msgToAPI[cfgOp.Operation]
		if !ok {
			err = fmt.Errorf("unrecognized operation")
			break
		}

		op := PropertyOp{
			Op:    cfgOpcode,
			Name:  cfgOp.GetProperty(),
			Value: cfgOp.GetValue(),
		}
		if cfgOp.Expires != nil {
			t, _ := ptypes.Timestamp(cfgOp.Expires)
			op.Expires = &t
		}

		ops = append(ops, op)
	}

	return ops, err
}

func init() {

	codeToErr = make(map[cfgmsg.ConfigResponse_OpResponse]error)
	for e, c := range errToCode {
		codeToErr[c] = e
	}

	msgToAPI = make(map[cfgmsg.ConfigOp_Operation]int)
	for a, m := range apiToMsg {
		msgToAPI[m] = a
	}
}
