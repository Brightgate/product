/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package mockcfg provides a mocked configuration client, which can be
// regarded as a peer to apcfg and clcfg.  It is intended for use only when
// authoring tests.  It is useful when constructing tests where a populated
// config tree is necessary, and where minor side-effects need to be
// observed.  For tests which require the internal logic of ap.configd
// (for example property type validation), mockcfg is insufficient.
package mockcfg

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	"bg/common/cfgapi"
	"bg/common/cfgtree"

	"github.com/pkg/errors"
)

// MockCmdHdl implements the CmdHdl interface, and is returned when one or more
// operations are submitted to Execute().  This handle can be used to check on
// the status of a pending operation, or to block until the operation completes
// or times out; since this is a mock, it is simply a container for a result
// and an error.
type mockCmdHdl struct {
	err  error
	rval string
}

func (h *mockCmdHdl) Status(ctx context.Context) (string, error) {
	return h.rval, h.err
}

func (h *mockCmdHdl) Wait(ctx context.Context) (string, error) {
	return h.rval, h.err
}

func (h *mockCmdHdl) Cancel(ctx context.Context) error {
	return nil
}

// MockExec represents an instance of this mock cfgapi implementation.
type MockExec struct {
	PTree *cfgtree.PTree
	Logf  func(format string, args ...interface{})
}

// Do-nothing routine satisfying interface for MockExec.Logf
func nullLogf(fmt string, arg ...interface{}) {
}

// NewMockExec returns a MockExec with no PTree initialized.
// Operations against such a MockExec will return ErrNoConfig.
func NewMockExec() *MockExec {
	return &MockExec{
		Logf: nullLogf,
	}
}

// NewMockExecEmptyTree returns a MockExec with a valid but empty PTree.
func NewMockExecEmptyTree() *MockExec {
	ptree, err := cfgtree.NewPTree("@/", []byte("{}"))
	if err != nil {
		panic(err)
	}
	return &MockExec{
		PTree: ptree,
		Logf:  nullLogf,
	}
}

// NewMockExecFromDefaults returns a MockExec with a PTree loaded from
// the default ap.configd PTree with @/apversion added.
func NewMockExecFromDefaults() *MockExec {
	testData, err := ioutil.ReadFile("../ap.configd/configd.json")
	if err != nil {
		panic(err)
	}
	testDataJSON := make(map[string]interface{})
	err = json.Unmarshal(testData, &testDataJSON)
	if err != nil {
		panic(err)
	}
	testDefaults, err := json.Marshal(testDataJSON["Defaults"])
	if err != nil {
		panic(err)
	}
	ptree, err := cfgtree.NewPTree("@/", testDefaults)
	if err != nil {
		panic(err)
	}
	ptree.ChangesetInit()
	err = ptree.Add("@/apversion", "mockcfg-TESTING", nil)
	if err != nil {
		panic(err)
	}
	ptree.ChangesetCommit()

	return &MockExec{
		PTree: ptree,
		Logf:  nullLogf,
	}

}

// LoadJSON loads the PTree indicated by jsonData into the MockExec's PTree.
func (m *MockExec) LoadJSON(jsonData []byte) error {
	if m.PTree != nil {
		return m.PTree.Replace(jsonData)
	}
	ptree, err := cfgtree.NewPTree("@/", jsonData)
	if err != nil {
		return err
	}
	m.PTree = ptree
	return nil
}

// Ping tests liveness of the server.  For this mock it's a no-op,
// although this code could be extended to mock Ping failures.
func (m *MockExec) Ping(ctx context.Context) error {
	return nil
}

// Translate cfgtree error space to cfgapi error space
func xlateError(err error) error {
	if err == cfgtree.ErrNoProp {
		err = cfgapi.ErrNoProp
	} else if err == cfgtree.ErrExpired {
		err = cfgapi.ErrExpired
	} else if err == cfgtree.ErrNotLeaf {
		err = cfgapi.ErrNotLeaf
	}
	return err
}

// Execute takes a slice of PropertyOp structures and executes them.
func (m *MockExec) Execute(ctx context.Context, ops []cfgapi.PropertyOp) cfgapi.CmdHdl {
	if m.PTree == nil {
		return &mockCmdHdl{err: cfgapi.ErrNoConfig}
	}

	hdl := &mockCmdHdl{}

	var rErr error
	var rVal string

	m.Logf("mockcfg: starting on %d ops", len(ops))
	m.PTree.ChangesetInit()
	for _, op := range ops {
		m.Logf("mockcfg:    %s", op)
		switch op.Op {
		case cfgapi.PropGet:
			var node *cfgtree.PNode
			node, rErr = m.PTree.GetNode(op.Name)
			if rErr != nil {
				break
			}
			// hdl.rval = node.Value
			jsonBytes, err := json.Marshal(node)
			if err != nil {
				// XXX I dunno what the right error is
				rErr = cfgapi.ErrBadTree
				break
			}
			rVal = string(jsonBytes)
		case cfgapi.PropCreate:
			rErr = m.PTree.Add(op.Name, op.Value, op.Expires)
		case cfgapi.PropDelete:
			_, rErr = m.PTree.Delete(op.Name)
		case cfgapi.PropSet:
			rErr = m.PTree.Set(op.Name, op.Value, op.Expires)
		case cfgapi.PropTest:
			_, rErr = m.PTree.GetNode(op.Name)
		case cfgapi.PropTestEq:
			var node *cfgtree.PNode
			node, rErr = m.PTree.GetNode(op.Name)
			if rErr != nil {
				break
			}
			if node.Value != op.Value {
				rErr = cfgapi.ErrNotEqual
			}
		default:
			panic(fmt.Sprintf("unknown op type %v", op))
		}

		// Stop execution on first error
		if rErr != nil {
			break
		}
	}

	hdl.err = xlateError(rErr)
	hdl.rval = rVal
	if hdl.err == nil {
		_ = m.PTree.ChangesetCommit()
		m.Logf("mockcfg:    rval %q", hdl.rval)
	} else {
		m.PTree.ChangesetRevert()
		m.Logf("mockcfg:    FAIL rval %q err %v", hdl.rval, hdl.err)
	}
	return hdl
}

// ExecuteAt takes a slice of PropertyOp structures and executes them.
// This mock ignores access levels.
func (m *MockExec) ExecuteAt(ctx context.Context, ops []cfgapi.PropertyOp, level cfgapi.AccessLevel) cfgapi.CmdHdl {
	return m.Execute(ctx, ops)
}

// HandleChange is not implemented by this mock at this time.
func (m *MockExec) HandleChange(path string, handler func([]string, string, *time.Time)) error {
	return nil
}

// HandleDelete is not implemented by this mock at this time.
func (m *MockExec) HandleDelete(path string, handler func([]string)) error {
	return nil
}

// HandleExpire is not implemented by this mock at this time.
func (m *MockExec) HandleExpire(path string, handler func([]string)) error {
	return nil
}

// Close is a no-op for this mock
func (m *MockExec) Close() {
}

// PropExists tests that a property called pname exists in the
// tree.
//
// This is intended to be used in test development.
func (m *MockExec) PropExists(pname string) error {
	_, err := m.PTree.GetNode(pname)
	return errors.Wrapf(err, "PropExists: %s", pname)
}

// PropAbsent tests that a property called pname does not exists in the tree.
//
// This is intended to be used in test development.
func (m *MockExec) PropAbsent(pname string) error {
	_, err := m.PTree.GetNode(pname)
	if err == cfgtree.ErrNoProp {
		return nil
	} else if err == nil {
		return errors.Errorf("PropAbsent: Property %s exists", pname)
	}
	return errors.Wrapf(err, "PropAbsent: unexpected error")
}

// PropEq tests that a property called pname exists in the tree and
// that it has value expected.
//
// This is intended to be used in test development.
func (m *MockExec) PropEq(pname, expected string) error {
	value, err := m.PTree.GetProp(pname)
	if err != nil {
		return errors.Wrapf(err, "PropEq: unexpected error")
	} else if value != expected {
		return errors.Errorf("PropEq: %s: expected %q != %q", pname, expected, value)
	}
	return nil
}
