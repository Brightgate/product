/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/satori/uuid"

	"bg/ap_common/apcfg"
	"bg/ap_common/network"
)

type propDescription struct {
	Path  string
	Type  string
	Level string
}

// Each field in a property path is represented by a Validation Node.
type vnode struct {
	keyType  string            // datatype of the path field
	keyText  string            // text of the path field
	level    apcfg.AccessLevel // access level required to modify
	children map[string]*vnode // list of child nodes
	valType  string            // for leaf nodes, data type of the value
}

// validate that the provided string is a legal instance of this datatype
type typeValidate func(string) error

var (
	vRoot = &vnode{
		keyType:  "const",
		keyText:  "@",
		level:    apcfg.AccessInternal,
		valType:  "none",
		children: make(map[string]*vnode),
	}

	validationFuncs = map[string]typeValidate{
		"const":      validateString,
		"bool":       validateBool,
		"int":        validateInt,
		"float":      validateFloat,
		"string":     validateString,
		"time":       validateTime,
		"uuid":       validateUUID,
		"ring":       validateRing,
		"nickind":    validateNicKind,
		"macaddr":    validateMac,
		"nic":        validateNic,
		"ipaddr":     validateIP,
		"cidr":       validateCIDR,
		"hostname":   validateHostname,
		"dnsaddr":    validateDNS,
		"ssid":       validateSSID,
		"passphrase": validatePassphrase,
		"auth":       validateAuth,
		"wifimode":   validateWifiMode,
		"wifiband":   validateWifiBand,
		"user":       validateString,
		"uid":        validateString,
		"email":      validateString,
		"phone":      validateString,
	}
)

func validateBool(val string) error {
	var err error

	v := strings.ToLower(val)
	if v != "true" && v != "false" {
		err = fmt.Errorf("'%s' is neither true nor false", val)
	}

	return err
}

func validateInt(val string) error {
	_, err := strconv.ParseInt(val, 10, 64)

	return err
}

func validateFloat(val string) error {
	_, err := strconv.ParseFloat(val, 64)

	return err
}

func validateString(val string) error {
	if len(val) == 0 {
		return fmt.Errorf("missing string value")
	}

	return nil
}

func validateTime(val string) error {
	formats := []string{
		time.RFC3339,
		"200601021504",
		"2006010215",
		"20060102",
		"01-02-15:04-2006",
		"Jan 2 15:04 2006",
		"Jan 2 15:04",
	}

	for _, f := range formats {
		if _, err := time.Parse(f, val); err == nil {
			return nil
		}
	}

	return fmt.Errorf("'%s' is not a valid time", val)
}

func validateUUID(val string) error {
	_, err := uuid.FromString(val)
	if err != nil {
		err = fmt.Errorf("'%s' not a valid uuid: %v", val, err)
	}
	return err
}

func validateNicKind(val string) error {
	var err error

	l := strings.ToLower(val)
	if l != "wired" && l != "wireless" {
		err = fmt.Errorf("'%s' is not a valid nic kind", val)
	}
	return err
}

func validateRing(val string) error {
	var err error

	if val != "" && apcfg.ValidRings[val] == false {
		err = fmt.Errorf("'%s' is not a valid ring", val)
	}
	return err
}

func validateMac(val string) error {
	_, err := net.ParseMAC(val)
	if err != nil {
		err = fmt.Errorf("'%s' is not a valid MAC address: %v",
			val, err)
	}
	return err
}

func validateNic(val string) error {
	// This is really the inverse of platform.NicID(), but in this context
	// we don't know the platform type.  The best we can do now is flag
	// those values that aren't valid on any platform.
	if val == "wan" || strings.HasPrefix(val, "lan") ||
		strings.HasPrefix(val, "wlan") {
		return nil
	}

	return validateMac(val)
}

func validateIP(val string) error {
	var err error

	if ip := net.ParseIP(val); ip == nil {
		err = fmt.Errorf("'%s' is not a valid IP address", val)
	}
	return err
}

func validateCIDR(val string) error {
	_, _, err := net.ParseCIDR(val)
	if err != nil {
		err = fmt.Errorf("'%s' is not a valid CIDR: %v", val, err)
	}
	return err
}

func validateHostname(val string) error {
	var err error

	if !network.ValidDNSLabel(val) {
		err = fmt.Errorf("'%s' is not a valid hostname", val)
	}
	return err
}

func validateDNS(val string) error {
	var err error

	if !network.ValidDNSName(val) {
		err = fmt.Errorf("'%s' is not a valid DNS name", val)
	}
	return err
}

func validateSSID(val string) error {
	var err error

	if len(val) == 0 || len(val) > 32 {
		err = fmt.Errorf("SSID must be between 1 and 32 characters")
	} else {
		for _, c := range val {
			// XXX: this is overly strict, but safe.  We'll need to
			// support a broader range eventually.
			if c > unicode.MaxASCII || !unicode.IsPrint(c) {
				err = fmt.Errorf("invalid characters in SSID")
			}
		}
	}

	return err
}

func validatePassphrase(val string) error {
	var err error

	if len(val) == 64 {
		re := regexp.MustCompile(`^[a-fA-F0-9]+$`)
		if !re.Match([]byte(val)) {
			err = fmt.Errorf("64-character passphrases must be" +
				" hex strings")
		}
	} else if len(val) < 8 || len(val) > 63 {
		err = fmt.Errorf("passphrase must be between 8 and 63 characters")
	} else {
		for _, c := range val {
			if c > unicode.MaxASCII || !unicode.IsPrint(c) {
				err = fmt.Errorf("Invalid characters in passphrase")
			}
		}
	}
	return err
}

func validateAuth(val string) error {
	var err error

	if val != "wpa-psk" && val != "wpa-eap" {
		err = fmt.Errorf("only wpa-psk and wpa-eap are supported")
	}

	return err
}

func validateWifiBand(val string) error {
	var err error

	if val != "2.4GHz" && val != "5GHz" {
		err = fmt.Errorf("invalid wifi band")
	}

	return err
}

func validateWifiMode(val string) error {
	modes := []string{"a", "b", "g", "n", "ac"}

	for _, mode := range modes {
		if val == mode {
			return nil
		}
	}

	return fmt.Errorf("invalid wifi mode")
}

// Walking a concrete path, find the vnode that matches this field in the path.
func getNextVnode(parent *vnode, field string) *vnode {
	for _, node := range parent.children {
		if node.keyType == "const" {
			if node.keyText == field {
				return node
			}
		} else {
			vfunc := validationFuncs[node.keyType]
			if vfunc(field) == nil {
				return node
			}
		}
	}

	return nil
}

// Given a path, find the vnode that matches the final element in the path.
// This may be either a leaf node or an internal node.
func getMatchingVnode(prop string) (*vnode, error) {
	fields := strings.Split(prop, "/")
	if fields[0] != "@" {
		return nil, fmt.Errorf("%s doesn't start with @", prop)
	}

	node := vRoot
	path := ""
	for _, f := range fields[1:] {
		if len(f) == 0 {
			continue
		}
		path = path + node.keyText + "/"
		if node = getNextVnode(node, f); node == nil {
			return nil, fmt.Errorf("%s not a valid child of %s",
				f, path)
		}
	}

	return node, nil
}

// Given a property->value, validate that the property path is valid, that the
// value matches the expected type for this property, and that the caller is
// allowed to perform the update.
func validatePropVal(prop string, val string, level apcfg.AccessLevel) error {
	if val == "" {
		return fmt.Errorf("missing value")
	}

	node, err := getMatchingVnode(prop)
	if err == nil {
		if len(node.children) > 0 {
			err = fmt.Errorf("%s is not a leaf property", prop)

		} else if level < node.level {
			err = fmt.Errorf("%s requires level '%s' or better",
				prop, apcfg.AccessLevelNames[node.level])

		} else {
			vfunc := validationFuncs[node.valType]
			if err = vfunc(val); err != nil {
				err = fmt.Errorf("invalid value: %v", err)
			}
		}
	}

	return err
}

// Recursively examine all descendents of this property, looking for any that
// are not modifiable at the given level.  The routine returns the path to the
// first such property found.
func validateChildren(node *vnode, level apcfg.AccessLevel) (string, apcfg.AccessLevel) {
	if level < node.level {
		return node.keyText, node.level
	}

	for _, child := range node.children {
		if p, l := validateChildren(child, level); p != "" {
			return node.keyText + "/" + p, l
		}
	}
	return "", -1
}

// Before attempting to delete a property, verify that the path is legal.  (This
// check should be a formality, since we should not have allowed an illegal path
// to be created in the first place.)  More importantly, we check to be sure
// that a user does not delete a subtree in which any of the properties are not
// user settable
func validatePropDel(prop string, level apcfg.AccessLevel) error {
	node, err := getMatchingVnode(prop)
	if err == nil {
		// We need to verify that this, and all descendent, nodes may be
		// modified at this access level.
		if p, l := validateChildren(node, level); p != "" {
			err = fmt.Errorf("%s requires '%s' access to delete",
				prop+"/"+p, apcfg.AccessLevelNames[l])
		}
	}

	return err
}

func validateProp(prop string) error {
	_, err := getMatchingVnode(prop)

	return err
}

// Given a property path, add vnodes for each field to the validation tree.
// Return the leaf vnode.
func newVnode(prop string) (*vnode, error) {
	fields := strings.Split(prop, "/")
	if fields[0] != "@" {
		return nil, fmt.Errorf("%s doesn't start with @/", prop)
	}

	node := vRoot
	for _, f := range fields[1:] {
		var keyType string

		parent := node
		if node = parent.children[f]; node != nil {
			continue
		}

		if f[0] == '%' {
			// If a field is enclosed with %%, it contains a datatype
			end := len(f) - 1
			if f[end] != '%' {
				return nil, fmt.Errorf("%s missing closing '%%'", f)
			}

			t := f[1:end]
			if _, ok := validationFuncs[t]; !ok {
				return nil, fmt.Errorf("unknown type: %s", t)
			}
			keyType = t
		} else {
			// Fields without <> are constant strings
			keyType = "const"
		}
		node = &vnode{
			keyText:  f,
			keyType:  keyType,
			level:    apcfg.AccessInternal,
			children: make(map[string]*vnode),
		}
		parent.children[f] = node
	}

	return node, nil
}

// Add a new property to the validation tree
func addOneProperty(prop, val, level string) error {
	node, err := newVnode(prop)
	if err != nil {
		return err
	}

	if v, ok := apcfg.AccessLevels[strings.ToLower(level)]; ok {
		node.level = v
	} else {
		err = fmt.Errorf("invalid level '%s' for %s", level, prop)
	}

	if _, ok := validationFuncs[val]; !ok {
		err = fmt.Errorf("unknown value type: %s", val)
	}

	if node.valType != "" {
		err = fmt.Errorf("duplicate property: %s", prop)
	} else {
		node.valType = val
	}

	return err
}

func validationInit(descriptions []propDescription) error {
	for _, d := range descriptions {
		if err := addOneProperty(d.Path, d.Type, d.Level); err != nil {
			return err
		}
	}

	return nil
}
