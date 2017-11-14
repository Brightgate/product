#
# COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.

"""
This script can be used in two ways. First, as a means to train and export a
LinearClassifier model for identifierd to consume. The exported model should be
checked into the source tree:

python Product/util/tf-train-export.py -train Product/ap_identities.csv -ap

Second, as a script to call from model-sim.go for evaluating the
LinearClassifier.

To use this script you will need to install the TensorFlow Python API:
https://www.tensorflow.org/install/
"""

import argparse
import collections
import numpy as np
import os
import shutil
import tensorflow as tf

# Keep in sync with ap_common/device/device.go: IDBase
DEVID_BASE = 2

# The same key must be used when running inference.
FEATURES_KEY = "x"

# For model checkpoints. We will train from scratch each time so tmp is fine.
MODEL_DIR_BASE = "/tmp/tf_models/"

# For exported SavedModel
SAVED_MODEL_BASE = ""

TRAIN_STEPS = 5000
BATCH_SIZE = 32

# Temporary. Move to Dataset API in TensorFlow 1.4
Dataset = collections.namedtuple('Dataset', ['data', 'labels'])


def load_csv_with_header(filename, rebase):
    arr = np.loadtxt(filename, delimiter=",", skiprows=1, dtype=np.int)
    data = arr[:, :-1]
    labels = arr[:, -1]
    if rebase:
        labels -= DEVID_BASE

    return Dataset(data=data, labels=labels)


def linear_classifier(train_path, test_path):
    model_dir = MODEL_DIR_BASE + "linear_classifier"

    # Train from scratch
    if os.path.exists(model_dir):
        shutil.rmtree(model_dir)
    os.makedirs(model_dir, exist_ok=True)

    training_set = load_csv_with_header(train_path, args.ap)
    num_features = len(training_set.data[0])
    num_classes = len(np.unique(training_set.labels))

    # Specify that all features have real-value data
    feature_columns = [tf.feature_column.numeric_column(
        key=FEATURES_KEY,
        shape=[num_features])]

    # These optimizers were sub-optimal:
    #   optimizer=tf.train.AdadeltaOptimizer()
    #   optimizer=tf.train.AdagradOptimizer(learning_rate=0.01)
    #   optimizer=tf.train.AdamOptimizer(learning_rate=0.09, epsilon=1.1)
    #   optimizer=tf.train.GradientDescentOptimizer(learning_rate=0.01)
    #   optimizer=tf.train.MomentumOptimizer(learning_rate=1.0, momentum=0.01),
    #   optimizer=tf.train.RMSPropOptimizer(learning_rate=0.01),
    #
    # These optimizers were near optimal but the returned probability estimates
    # were poorly calibrated:
    #   optimizer=tf.train.ProximalAdagradOptimizer(
    #       learning_rate=0.01,
    #       l1_regularization_strength=0.1)
    #   optimizer=tf.train.ProximalGradientDescentOptimizer(
    #       learning_rate=0.001,
    #       l1_regularization_strength=0.1)
    classifier = tf.estimator.LinearClassifier(
        feature_columns=feature_columns,
        n_classes=num_classes,
        optimizer=tf.train.FtrlOptimizer(
            learning_rate=1.0,
            l1_regularization_strength=10.0),
        model_dir=model_dir)

    # Define the training inputs
    train_input_fn = tf.estimator.inputs.numpy_input_fn(
        x={FEATURES_KEY: training_set.data},
        y=training_set.labels,
        num_epochs=None,
        shuffle=True)

    # Train model.
    classifier.train(input_fn=train_input_fn, steps=TRAIN_STEPS)

    if test_path is not None:
        test_set = load_csv_with_header(test_path, args.ap)
        test_input_fn = tf.estimator.inputs.numpy_input_fn(
            x={FEATURES_KEY: test_set.data},
            y=test_set.labels,
            num_epochs=1,
            shuffle=False)

        ev = classifier.evaluate(input_fn=test_input_fn)
        print("TensorFlow Linear accuracy: {0:f}".format(ev["accuracy"]))

    # This will save the model in a format which expects serialized tf.Example
    # instances as input to FEATURES_KEY. There is supposedly a way to avoid
    # using tf.Example but it doesn't work for me. It seens the issue is
    # https://github.com/tensorflow/tensorflow/issues/12367
    #
    # This doesn't work:
    # features = {FEATURES_KEY: tf.placeholder(tf.int32, [num_features])}
    # serving_fn = tf.estimator.export.build_raw_serving_input_receiver_fn(
    #   features=features)
    feature_spec = tf.feature_column.make_parse_example_spec(feature_columns)
    serving_fn = tf.estimator.export.build_parsing_serving_input_receiver_fn(
        feature_spec=feature_spec)
    ret_path = classifier.export_savedmodel(
        export_dir_base=SAVED_MODEL_BASE + "linear_model_deviceID",
        serving_input_receiver_fn=serving_fn,
        as_text=args.as_text)

    print("Linear model saved to {}".format(ret_path.decode()))


parser = argparse.ArgumentParser()
parser.add_argument("-train", type=str, help="path to training data")
parser.add_argument("-test", type=str, help="path to test data")
parser.add_argument("-ap", action="store_true",
                    help="indicates the training data is 'ap_identities.csv'")
parser.add_argument("-as_text", action="store_true",
                    help="store the exported graph as text")
args = parser.parse_args()

linear_classifier(args.train, args.test)
