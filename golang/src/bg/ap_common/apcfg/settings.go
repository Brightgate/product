/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package apcfg

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"bg/common/cfgapi"
)

type callbackFn func(name, val string) error

type settingType interface {
	Set(string) error
	String() string
	Type() string
	Reset()
}

type setting struct {
	name     string
	val      settingType
	defval   string
	dynamic  bool
	callback callbackFn
}

var (
	identity string
	settings = make(map[string]*setting)
)

// Boolean settings
type boolSetting struct {
	val    *bool
	defval bool
}

func (b boolSetting) Set(val string) error {
	x, err := strconv.ParseBool(val)
	if err == nil {
		*b.val = x
	}
	return err
}

func (b boolSetting) String() string {
	return fmt.Sprintf("%v", *b.val)
}

func (b boolSetting) Type() string {
	return "bool"
}

func (b boolSetting) Reset() {
	*b.val = b.defval
}

// Integer settings
type intSetting struct {
	val    *int
	defval int
}

func (i intSetting) Set(val string) error {
	x, err := strconv.Atoi(val)
	if err == nil {
		*i.val = x
	}
	return err
}

func (i intSetting) String() string {
	return fmt.Sprintf("%v", *i.val)
}

func (i intSetting) Type() string {
	return "int"
}

func (i intSetting) Reset() {
	*i.val = i.defval
}

// String settings
type stringSetting struct {
	val    *string
	defval string
}

func (s stringSetting) Set(val string) error {
	*s.val = val
	return nil
}

func (s stringSetting) String() string {
	return *s.val
}

func (s stringSetting) Type() string {
	return "string"
}

func (s stringSetting) Reset() {
	*s.val = s.defval
}

// time.Duration settings
type durationSetting struct {
	val    *time.Duration
	defval time.Duration
}

func (d durationSetting) Set(val string) error {
	x, err := time.ParseDuration(val)
	if err == nil {
		*d.val = x
	}
	return err
}

func (d durationSetting) String() string {
	return d.val.String()
}

func (d durationSetting) Type() string {
	return "duration"
}

func (d durationSetting) Reset() {
	*d.val = d.defval
}

func registerSetting(name string, s settingType, dynamic bool, cb callbackFn) {
	if _, ok := settings[name]; ok {
		log.Fatalf("duplicate setting: %s\n", name)
	}

	settings[name] = &setting{
		name:     name,
		val:      s,
		defval:   s.String(),
		dynamic:  dynamic,
		callback: cb,
	}
}

// UpdateSetting will change the value of a setting, and invoke any associated
// callback.
func UpdateSetting(setting, val string) error {
	var err error

	s, ok := settings[setting]
	if !ok {
		err = fmt.Errorf("unrecognized setting: %s", setting)
	} else {
		if s.callback != nil {
			err = s.callback(s.name, val)
		}
		if err == nil {
			err = s.val.Set(val)
		}
	}

	if err == nil {
		log.Printf("Changing setting %s to %v", s.name, val)
	} else {
		log.Printf("Can't change %s to %s: %v", setting, val, err)
	}
	return err
}

// Respond to a change to a @/settings/ property belonging to this daemon.
// Non-dynamic settings are not updated in-core, but will receive their new
// values when the daemon restarts.
func updateSetting(path []string, val string, expires *time.Time) {
	if len(path) != 3 || path[0] != "settings" || path[1] != identity {
		return
	}

	setting := path[2]
	s, ok := settings[setting]
	if ok && !s.dynamic {
		log.Printf("change to static setting: %s", setting)
	} else {
		UpdateSetting(setting, val)
	}
}

// Respond to the deletion of a @/settings/ property belonging to this daemon by
// resetting it to its default value.
func deleteSetting(path []string) {
	if len(path) != 3 || path[0] != "settings" || path[1] != identity {
		return
	}

	setting := path[2]
	s, ok := settings[setting]
	if ok && !s.dynamic {
		log.Printf("reset static setting: %s", setting)
	} else {
		log.Printf("Resetting setting %s to %v", s.name, s.defval)
		s.val.Reset()
		if s.callback != nil {
			s.callback(s.name, s.defval)
		}
	}
}

// Bool allocates and initializes a boolean setting
func Bool(name string, defval bool, dynamic bool, callback callbackFn) *bool {
	val := defval
	s := boolSetting{val: &val, defval: defval}
	registerSetting(name, s, dynamic, callback)
	return &val
}

// Int allocates and initializes an integer setting
func Int(name string, defval int, dynamic bool, callback callbackFn) *int {
	val := defval
	s := intSetting{val: &val, defval: defval}
	registerSetting(name, s, dynamic, callback)
	return &val
}

// String allocates and initializes a string setting
func String(name string, defval string, dynamic bool, callback callbackFn) *string {
	val := defval
	s := stringSetting{val: &val, defval: defval}
	registerSetting(name, s, dynamic, callback)
	return &val
}

// Duration allocates and initializes a time.Duration setting
func Duration(name string, defval time.Duration, dynamic bool,
	callback callbackFn) *time.Duration {
	val := defval
	s := durationSetting{val: &val, defval: defval}
	registerSetting(name, s, dynamic, callback)
	return &val
}

func settingsInit(hdl *cfgapi.Handle, config *APConfig) {
	identity = config.name

	if len(settings) == 0 {
		return
	}
	root := "@/settings/" + identity

	// Register all possible settings with configd.  This will add them to
	// the list of configuration paths allowed by the validation table, but
	// will not make any changes to the config tree itself.
	for _, s := range settings {
		path := root + "/" + s.name
		if err := hdl.AddPropValidation(path, s.val.Type()); err != nil {
			log.Fatalf("Failed to initialize setting '%s': %v\n", root, err)
		}
	}

	// Fetch any setting values stored in the config tree, and use those to
	// initialize the in-core settings.
	initial, err := hdl.GetProps(root)
	if err != nil && err != cfgapi.ErrNoProp {
		log.Printf("failed to get initial settings: %v", err)
	} else if initial != nil {
		for setting, prop := range initial.Children {
			UpdateSetting(setting, prop.Value)
		}
	}

	config.HandleChange(`^`+root, updateSetting)
	config.HandleDelete(`^`+root, deleteSetting)
}

