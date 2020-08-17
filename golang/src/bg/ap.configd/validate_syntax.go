/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

	"bg/common/cfgapi"
	"bg/common/mfg"
	"bg/common/network"
	"bg/common/wifi"
)

type propDescription struct {
	Path  string
	Type  string
	Level string
}

// Each field in a property path is represented by a Validation Node.
type vnode struct {
	path     string
	keyType  string             // datatype of the path field
	keyText  string             // text of the path field
	level    cfgapi.AccessLevel // access level required to modify
	children map[string]*vnode  // list of child nodes
	valType  string             // for leaf nodes, data type of the value
}

// validate that the provided string is a legal instance of this datatype
type typeValidate func(string) error

var (
	vRoot = &vnode{
		path:     "@",
		keyType:  "const",
		keyText:  "@",
		level:    cfgapi.AccessInternal,
		valType:  "none",
		children: make(map[string]*vnode),
	}

	expansions = map[string][]string{
		`%policy_src%`: {`site`, `rings/%ring%`, `clients/%macaddr%`},
		`%policy_sc%`:  {`site`, `clients/%macaddr%`},
		`%policy_sr%`:  {`site`, `rings/%ring%`},
		`%policy_rc%`:  {`rings/%ring%`, `clients/%macaddr%`},
	}

	validationFuncs = map[string]typeValidate{
		"null":        validateNull,
		"bool":        validateBool,
		"cidr":        validateCIDR,
		"privatecidr": validatePrivateCIDR,
		"fwtarget":    validateForwardTarget,
		"const":       validateString,
		"dnsaddr":     validateDNS,
		"duration":    validateDuration,
		"email":       validateString,
		"float":       validateFloat,
		"hostname":    validateHostname,
		"int":         validateInt,
		"ipaddr":      validateIP,
		"ipoptport":   validateIPOptPort,
		"keymgmt":     validateKeyMgmt,
		"macaddr":     validateMac,
		"nic":         validateNic,
		"nickind":     validateNicKind,
		"nicstate":    validateNicState,
		"passphrase":  validatePassphrase,
		"phone":       validateString,
		"port":        validatePort,
		"proto":       validateProto,
		"nodeid":      validateNodeID,
		"ring":        validateRing,
		"sshaddr":     validateSSHAddr,
		"ssid":        validateSSID,
		"string":      validateString,
		"time":        validateTime,
		"time_unit":   validateTimeUnit,
		"tribool":     validateTribool,
		"uid":         validateString,
		"user":        validateString,
		"uuid":        validateUUID,
		"wifiband":    validateWifiBand,
		"wifiwidth":   validateWifiWidth,
	}
)

func validateNull(val string) error {
	var err error
	if len(val) != 0 {
		err = fmt.Errorf("cannot be set to a non-null value")
	}
	return err
}

func validateBool(val string) error {
	var err error

	v := strings.ToLower(val)
	if v != "true" && v != "false" {
		err = fmt.Errorf("'%s' is neither true nor false", val)
	}

	return err
}

func validateTribool(val string) error {
	var err error

	v := strings.ToLower(val)
	if v != "true" && v != "false" && v != "unknown" {
		err = fmt.Errorf("'%s' is not true, false, or unknown", val)
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

func validateNicState(val string) error {
	var err error

	if _, ok := wifi.DeviceStates[val]; !ok {
		err = fmt.Errorf("'%s' is not a valid nic state", val)
	}
	return err
}

func validateNodeID(val string) error {
	var err error

	if !mfg.ValidExtSerial(val) {
		if _, err = uuid.FromString(val); err != nil {
			err = fmt.Errorf("'%s' not a valid nodeid: %v", val, err)
		}
	}
	return err
}

func validateRing(val string) error {
	var err error

	if cfgapi.ValidRings[val] == false {
		err = fmt.Errorf("'%s' is not a valid ring", val)
	}
	return err
}

func validateKeyMgmt(val string) error {
	var err error

	lower := strings.ToLower(val)
	if lower != "wpa-psk" && lower != "wpa-eap" {
		err = fmt.Errorf("'%s' is not a valid key management", val)
	}
	return err
}

func validateMac(val string) error {
	_, err := net.ParseMAC(val)
	if err != nil {
		err = fmt.Errorf("'%s' is not a valid MAC address: %v",
			val, err)
	} else if val != strings.ToLower(val) {
		err = fmt.Errorf("'%s': MAC addresses must be all lowercase",
			val)
	}

	return err
}

func validateNic(val string) error {
	// This is really the inverse of platform.NicID(), but in this context
	// we don't know the platform type.  The best we can do now is flag
	// those values that aren't valid on any platform.
	if val == "wan" || strings.HasPrefix(val, "lan") ||
		strings.HasPrefix(val, "wlan") ||
		strings.HasPrefix(val, "eth") {
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

func validatePrivateCIDR(val string) error {
	ip, _, err := net.ParseCIDR(val)
	if err != nil {
		err = fmt.Errorf("'%s' is not a valid CIDR: %v", val, err)
	} else if !network.IsPrivate(ip) {
		err = fmt.Errorf("'%s' is not a private subnet: %v", val, err)
	}
	return err
}

// Validate a forward target, which is <client_mac>[/port]
func validateForwardTarget(val string) error {
	var err error

	f := strings.Split(val, "/")
	if len(f) > 2 {
		err = fmt.Errorf("must be <mac>[/port]")
	} else {
		err = validateMac(f[0])
		if err == nil && len(f) == 2 {
			err = validatePort(f[1])
		}
	}
	if err != nil {
		err = fmt.Errorf("'%s' is not a valid forward target: %v",
			val, err)
	}

	return err
}

func validateProto(val string) error {
	var err error

	if !strings.EqualFold(val, "tcp") && !strings.EqualFold(val, "udp") {
		err = fmt.Errorf("'%s' is not a valid protocol", val)
	}
	return err
}

func validatePort(val string) error {
	port, err := strconv.Atoi(val)
	if err != nil || port <= 0 || port >= 65536 {
		err = fmt.Errorf("'%s' is not a valid port number", val)
	}
	return err
}

// Validate 'ip[:port]'
func validateIPOptPort(val string) error {
	var err error

	f := strings.Split(val, ":")

	if len(f) == 1 {
		err = validateIP(val)

	} else if len(f) == 2 {
		if err = validateIP(f[0]); err == nil {
			err = validatePort(f[1])
		}

	} else {
		err = fmt.Errorf("'%s' is not a valid <ip>[:<port>]", val)
	}
	return err
}

func validateHostname(val string) error {
	var err error

	if !network.ValidDNSLabel(val) || strings.ToLower(val) == "localhost" {
		err = fmt.Errorf("'%s' is not a valid hostname", val)
	}
	return err
}

func validateDNS(val string) error {
	var err error

	if !network.ValidDNSName(val) || strings.ToLower(val) == "localhost" {
		err = fmt.Errorf("'%s' is not a valid DNS name", val)
	}
	return err
}

func validateSSHAddr(val string) error {
	var err error

	fields := strings.Split(val, ":")
	addr := fields[0]
	if !network.ValidDNSName(addr) && net.ParseIP(addr) == nil {
		err = fmt.Errorf("'%s' is not a valid address", addr)
	} else if len(fields) == 2 {
		err = validatePort(fields[1])
	} else if len(fields) > 2 {
		err = fmt.Errorf("ssh address must contain <addr>[:<port>]")
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

func validateWifiBand(val string) error {
	var err error

	if val != "2.4GHz" && val != "5GHz" {
		err = fmt.Errorf("invalid wifi band")
	}

	return err
}

func validateWifiWidth(val string) error {
	var err error

	if val != "20" && val != "40" && val != "80" {
		err = fmt.Errorf("invalid wifi width")
	}

	return err
}

func validateDuration(val string) error {
	var err error

	if _, err = time.ParseDuration(val); err != nil {
		err = fmt.Errorf("invalid duration: %v", err)
	}

	return err
}

func validateTimeUnit(val string) error {
	var err error

	l := strings.ToLower(val)
	if l != "second" && l != "minute" && l != "hour" && l != "day" {
		err = fmt.Errorf("invalid time unit")
	}

	return err
}

func getValidationFunc(propType string) (typeValidate, error) {
	var err error

	propType = strings.TrimPrefix(propType, "list:")
	rval, ok := validationFuncs[propType]
	if !ok {
		err = fmt.Errorf("unknown type: %s", propType)
	}

	return rval, err
}

func validate(valType, valInstance string) error {
	vfunc, err := getValidationFunc(valType)
	if err == nil {
		var vals []string
		if strings.HasPrefix(valType, "list:") {
			vals = strings.Split(valInstance, ",")
		} else {
			vals = []string{valInstance}
		}
		for _, val := range vals {
			val = strings.TrimSpace(val)
			if err = vfunc(val); err != nil {
				break
			}

		}
	}
	return err
}

// Walking a concrete path, find the vnode that matches this field in the path.
func getNextVnode(parent *vnode, field string) *vnode {
	for _, node := range parent.children {
		if node.keyType == "const" {
			if node.keyText == field {
				return node
			}
		} else if validate(node.keyType, field) == nil {
			return node
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
func validatePropVal(prop string, val string, level cfgapi.AccessLevel) error {
	if val == "" {
		return fmt.Errorf("missing value")
	}

	node, err := getMatchingVnode(prop)
	if err == nil {
		if len(node.children) > 0 {
			err = fmt.Errorf("%s is not a leaf property", prop)

		} else if level < node.level {
			err = fmt.Errorf("modifying %s requires level '%s' "+
				"or better",
				prop, cfgapi.AccessLevelNames[node.level])

		} else if err = validate(node.valType, val); err != nil {
			err = fmt.Errorf("invalid value: %v", err)
		}
	}

	return err
}

// Recursively examine all descendents of this property, looking for any that
// are not modifiable at the given level.  The routine returns the path to the
// first such property found.
func validateChildren(node *vnode, level cfgapi.AccessLevel) (string, cfgapi.AccessLevel) {
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
func validatePropDel(prop string, level cfgapi.AccessLevel) error {
	node, err := getMatchingVnode(prop)
	if err == nil {
		// We need to verify that this, and all descendent, nodes may be
		// modified at this access level.
		if p, l := validateChildren(node, level); p != "" {
			err = fmt.Errorf("%s requires '%s' access to delete",
				p, cfgapi.AccessLevelNames[l])
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
	path := "@"
	for _, f := range fields[1:] {
		var keyType string

		if len(f) == 0 {
			continue
		}

		path = path + "/" + f

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
			if _, err := getValidationFunc(t); err != nil {
				return nil, err
			}
			keyType = t
		} else {
			// Fields without %% are constant strings
			keyType = "const"
		}

		// Make sure we don't put conflicting entries into the tree.
		for _, sib := range parent.children {
			var conflicts bool
			if keyType == "const" && sib.keyType != "const" {
				conflicts = (validate(sib.keyType, f) == nil)
			} else if sib.keyType == "const" && keyType != "const" {
				conflicts = (validate(keyType, sib.keyText) == nil)
			}
			if conflicts {
				return nil, fmt.Errorf("%s incompatible with %s",
					path, sib.path)
			}
		}

		node = &vnode{
			path:     path,
			keyText:  f,
			keyType:  keyType,
			level:    cfgapi.AccessInternal,
			children: make(map[string]*vnode),
		}
		slog.Debugf("new node: %s", path)
		parent.children[f] = node
	}

	return node, nil
}

func addSetting(setting, valType string) error {
	if !strings.HasPrefix(setting, "@/settings/") {
		return fmt.Errorf("invalid settings path: %s", setting)
	}

	if _, err := getValidationFunc(valType); err != nil {
		return err
	}

	// Unlike addOneProperty(), it is not an error to add the same setting
	// twice.  It most likely means that a daemon has been restarted.
	node, err := newVnode(setting)
	if err == nil {
		node.valType = valType
		node.level = cfgapi.AccessDeveloper
	}

	return err
}

// Add a new property to the validation tree
func addOneProperty(prop, val, level string) error {
	if _, err := getValidationFunc(val); err != nil {
		return err
	}

	accessLevel, ok := cfgapi.AccessLevels[strings.ToLower(level)]
	if !ok {
		return fmt.Errorf("invalid level '%s' for %s", level, prop)
	}

	node, err := newVnode(prop)
	if err == nil {
		if node.valType != "" {
			err = fmt.Errorf("duplicate property: %s", prop)
		} else {
			node.valType = val
			node.level = accessLevel
		}
	}

	return err
}

// Iterate over all the property paths in the provided list.  If any of the
// fields in the path match an "expandable field", replace that one path with
// each of the possible replacement paths.
func expandMultiProperties(in []string) []string {
	out := make([]string, 0)

	for _, p := range in {
		exp := make([]string, 0)
		for stub, replacements := range expansions {
			f := strings.SplitN(p, stub, 2)
			if len(f) == 2 {
				for _, rep := range replacements {
					exp = append(exp, f[0]+rep+f[1])
				}
			}
		}
		if len(exp) == 0 {
			out = append(out, p)
		} else {
			out = append(out, exp...)
		}
	}

	return out
}

func validationInit(descriptions []propDescription) error {
	var rval error

	for _, d := range descriptions {
		paths := expandMultiProperties([]string{d.Path})

		for _, p := range paths {
			err := addOneProperty(p, d.Type, d.Level)
			if err != nil {
				slog.Errorf("failed to add property %s: %v",
					p, err)
				rval = err
			}
		}
	}

	return rval
}

