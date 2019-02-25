/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package zaperr implements an interface for structured errors similar to zap's
// interface for structured logging.  These errors also implement the interfaces
// necessary to be logged through zap.
package zaperr

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapError is the structured error type.  It is exported for lint reasons.
type ZapError struct {
	msg string
	kv  []interface{}
}

func (ze ZapError) Error() string {
	return ze.msg
}

// MarshalLogObject is largely a copy of zap.SugaredLogger.sweetenFields(), as
// an attempt to implement the suggestions in uber-go/zap#529.  The biggest
// problem is that we don't have a good way of handling any errors that come up
// during the marshaling.  The original code calls DPanic() on the base logger,
// but we don't have access to that, so we just add fields to the current
// stream.  It's possible that doing the core wrapping as suggested might help.
func (ze ZapError) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	var invalid invalidPairs

	enc.AddString("msg", ze.msg)
	for i := 0; i < len(ze.kv); {
		// This is a strongly-typed field. Consume it and move on.
		if field, ok := ze.kv[i].(zapcore.Field); ok {
			field.AddTo(enc)
			i++
			continue
		}

		// Make sure this element isn't a dangling key
		if i == len(ze.kv)-1 {
			zap.Any("ignored", ze.kv[i]).AddTo(enc)
			break
		}

		// Consume this value and the next, treating them as a key-value
		// pair.  If the key isn't a string, add this pair to the slice
		// of invalid pairs.
		key, val := ze.kv[i], ze.kv[i+1]
		if keyStr, ok := key.(string); !ok {
			// Subsequent errors are likely, so allocate once up
			// front.
			if cap(invalid) == 0 {
				invalid = make(invalidPairs, 0, len(ze.kv)/2)
			}
			invalid = append(invalid, invalidPair{i, key, val})
		} else {
			zap.Any(keyStr, val).AddTo(enc)
		}

		i += 2
	}

	// If we encountered any invalid key-value pairs, log them
	if len(invalid) > 0 {
		field := zap.Array("invalid", invalid)
		field.AddTo(enc)
	}

	return nil
}

// ZapErrorArray is a type allowing for arrays of ZapErrors to be marshaled
// properly.  It's exported because clients need to use it explicitly if they're
// building an array of errors; []error is handled by the core zap code, and
// []ZapError silently emits nothing.
type ZapErrorArray []ZapError

// MarshalLogArray implements zapcore.ArrayMarshaler.
func (zea ZapErrorArray) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for i := range zea {
		enc.AppendObject(zea[i])
	}
	return nil
}

type invalidPair struct {
	position   int
	key, value interface{}
}

func (p invalidPair) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddInt64("position", int64(p.position))
	zap.Any("key", p.key).AddTo(enc)
	zap.Any("value", p.value).AddTo(enc)
	return nil
}

type invalidPairs []invalidPair

func (ps invalidPairs) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for i := range ps {
		enc.AppendObject(ps[i])
	}
	return nil
}

// Errorw returns an error which contains a message and an array of key/value
// pairs, which can be logged in structured (and even nested) fashion by zap.
func Errorw(msg string, args ...interface{}) ZapError {
	return ZapError{
		msg: msg,
		kv:  args,
	}
}
