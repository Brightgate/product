/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// This code is heavily based on github.com/tomazk/envcfg.

package vaulttags

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	vault "github.com/hashicorp/vault/api"
)

const (
	structTag = "vault"
)

type warner interface {
	Warn(...interface{})
	Warnf(string, ...interface{})
}

type kv2Handle struct {
	enginePath string
	component  string
	logical    *vault.Logical
	logger     warner
}

// Note that Get is inefficient when it comes to pulling multiple keys from the
// same secret, as it will read the secret each time.  This is also non-atomic,
// which is unlikely to be problematic for our purposes, but isn't great.
func (t kv2Handle) Get(name string) (string, error) {
	// name is an agglomeration of the secret path and the key.  Strictly
	// speaking, either portion could have a slash in it, but for our
	// purposes, we expect neither to.
	nameSlice := strings.Split(name, "/")
	if len(nameSlice) != 2 || nameSlice[0] == "" || nameSlice[1] == "" {
		return "", fmt.Errorf("'vault' struct tag %q should be 'secret/key'", name)
	}
	secretPath := t.component + "/" + nameSlice[0]
	key := nameSlice[1]

	// The /data/ component is because it's a v2 (versioned) kv engine.
	fullPath := fmt.Sprintf("%s/data/%s", t.enginePath, secretPath)
	secret, err := t.logical.Read(fullPath)
	if err != nil {
		return "", err
	}
	if secret == nil {
		t.logger.Warnf("No vault secret at '%s'", fullPath)
		return "", nil
	}
	if secret.Warnings != nil {
		t.logger.Warnf("Vault returned warnings: %s", secret.Warnings)
	}
	if secret.Data == nil {
		// Someone created the secret with just metadata.
		return "", fmt.Errorf("No data for vault secret at '%s'", fullPath)
	}
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		// Not sure how this would happen in real life.
		return "", fmt.Errorf("No vault data at '%s'", fullPath)
	}
	value, ok := data[key].(string)
	if !ok {
		// Not sure how this would happen, either.
		return "", fmt.Errorf("Value for key '%s' at '%s' is not a string",
			key, fullPath)
	}
	return value, nil
}

// Unmarshal will connect to the Vault server and pull the keys out of the
// secrets as tagged in the definition of the passed-in struct.  We make the
// mount path of the secret engine configurable, as well as a "component" path
// component which is used after the mount path, but before the name of the
// secret proper.  The connection to Vault is also passed in, as well as a
// mechanism for logging warnings.
func Unmarshal(enginePath, component string, logical *vault.Logical, logger warner, v interface{}) error {
	structType, err := makeSureTypeIsSupported(v)
	if err != nil {
		return err
	}
	if err := makeSureStructFieldTypesAreSupported(structType); err != nil {
		return err
	}
	makeSureValueIsInitialized(v)

	structVal := getStructValue(v)

	t := kv2Handle{
		enginePath: enginePath,
		component:  component,
		logical:    logical,
		logger:     logger,
	}

	if err := unmarshalAllStructFields(structVal, t); err != nil {
		return err
	}

	return nil
}

func getTag(structField reflect.StructField) (string, error) {
	if tag := structField.Tag.Get(structTag); tag != "" {
		return tag, nil
	}
	return "", fmt.Errorf("no tag for element '%s'", structField.Name)
}

func unmarshalString(fieldVal reflect.Value, structField reflect.StructField, t kv2Handle) error {
	tag, err := getTag(structField)
	if err != nil {
		return nil // Should we just return the error (missing tag)?
	}

	val, err := t.Get(tag)
	if err != nil {
		return err
	}

	fieldVal.SetString(val)
	return nil
}

func unmarshalSingleField(fieldVal reflect.Value, structField reflect.StructField, t kv2Handle) error {
	if !fieldVal.CanSet() { // unexported field can not be set
		return nil
	}
	switch structField.Type.Kind() {
	case reflect.String:
		return unmarshalString(fieldVal, structField, t)
	}
	return nil
}

func unmarshalAllStructFields(structVal reflect.Value, t kv2Handle) error {
	for i := 0; i < structVal.NumField(); i++ {
		if err := unmarshalSingleField(structVal.Field(i), structVal.Type().Field(i), t); err != nil {
			return err
		}
	}
	return nil
}

func getStructValue(v interface{}) reflect.Value {
	str := reflect.ValueOf(v)
	for {
		if str.Kind() == reflect.Struct {
			break
		}
		str = str.Elem()
	}
	return str
}

func makeSureValueIsInitialized(v interface{}) {
	if reflect.TypeOf(v).Elem().Kind() != reflect.Ptr {
		return
	}
	if reflect.ValueOf(v).Elem().IsNil() {
		reflect.ValueOf(v).Elem().Set(reflect.New(reflect.TypeOf(v).Elem().Elem()))
	}
}

func makeSureTypeIsSupported(v interface{}) (reflect.Type, error) {
	if reflect.TypeOf(v).Kind() != reflect.Ptr {
		return nil, errors.New("we need a pointer")
	}
	if reflect.TypeOf(v).Elem().Kind() == reflect.Ptr && reflect.TypeOf(v).Elem().Elem().Kind() == reflect.Struct {
		return reflect.TypeOf(v).Elem().Elem(), nil
	} else if reflect.TypeOf(v).Elem().Kind() == reflect.Struct && reflect.ValueOf(v).Elem().CanAddr() {
		return reflect.TypeOf(v).Elem(), nil
	}
	return nil, errors.New("we need a pointer to struct or pointer to pointer to struct")
}

func isSupportedStructField(k reflect.StructField) bool {
	switch k.Type.Kind() {
	case reflect.String:
		return true
	default:
		return false
	}

}
func makeSureStructFieldTypesAreSupported(structType reflect.Type) error {
	for i := 0; i < structType.NumField(); i++ {
		if !isSupportedStructField(structType.Field(i)) {
			return fmt.Errorf("unsupported struct field type: %v", structType.Field(i).Type)
		}
	}
	return nil
}

