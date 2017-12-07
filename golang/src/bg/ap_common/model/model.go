/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package model

import (
	"fmt"
	"regexp"

	tf "github.com/tensorflow/tensorflow/tensorflow/go"
	"github.com/tensorflow/tensorflow/tensorflow/go/op"
)

// saved_model_cli should be able to give us the names below by doing
//
//   $ saved_model_cli show --dir <model-dir> --tag_set serve --signature_def serving_default
//
// but the resulting output doesn't give the feature key or the class ID path.
// Manually inspect the saved model .pbtxt file to fill in what we need.

// TFFeaturesKey is the key used in the TensorFlow feature column
const TFFeaturesKey = "x"

// TFInput is the name of the input tensors in the saved model
const TFInput = "input_example_tensor"

// TFClassID is the name of the output tensor containing class IDs
const TFClassID = "linear/head/predictions/class_ids"

// TFProb is the name of the output tensor containing class probabilities
const TFProb = "linear/head/predictions/probabilities"

// DNSQ matches DNS questions.
// See github.com/miekg/dns/types.go: func (q *Question) String() {}
var DNSQ = regexp.MustCompile(`;(.*?)\t`)

// FormatPortString formats a port attribute
func FormatPortString(protocol string, port int32) string {
	return fmt.Sprintf("%s %d", protocol, port)
}

// FormatMfgString formats a manufacturer attribute
func FormatMfgString(mfg int) string {
	return fmt.Sprintf("Mfg%d", mfg)
}

// FormatTFExample constructs a TensorFlow graph to transform the data into a
// tf.Example which can be used as input to the LinearClassifier exported by
// tf-train-export.py.
//
// In the future we might be able to skip this step. See
// https://github.com/tensorflow/tensorflow/issues/12367
func FormatTFExample(featKey, featList string) ([]*tf.Tensor, error) {
	s := op.NewScope()
	c := op.Const(s, []string{"{ features: { feature: { " + featKey +
		": { float_list: { value: [" + featList + "] }}}}}"})
	exampleOp := op.DecodeJSONExample(s, c)
	graph, err := s.Finalize()
	if err != nil {
		return nil, fmt.Errorf("failed to finalize graph: %s", err)
	}

	sess, err := tf.NewSession(graph, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %s", err)
	}

	example, err := sess.Run(nil, []tf.Output{exampleOp}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to run session: %s", err)
	}

	if err := sess.Close(); err != nil {
		return nil, fmt.Errorf("failed to close session: %s", err)
	}

	return example, nil
}
