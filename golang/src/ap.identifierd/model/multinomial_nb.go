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
	"math"

	"github.com/sjwhitworth/golearn/base"
)

// MultinomialNBClassifier holds the data necessary for classification.
type MultinomialNBClassifier struct {
	base.BaseEstimator
	// Log(Prior probability) for each class
	classPrior map[string]float64
	// Number of instances in each class.
	classInstances map[string]int
	// Log(Conditional probability) for each term. This vector should be
	// accessed in the following way: condProb[c][f] = Log(p(f|c)).
	// Logarithm is used in order to avoid underflow.
	condProb map[string][]float64
	// Number of instances used in training.
	trainingInstances int
	// Number of features used in training
	features int
	// Attributes used to train
	attrs []base.Attribute
}

// NewMultinomialNBClassifier create a new Multinomial Naive Bayes Classifier.
func NewMultinomialNBClassifier() *MultinomialNBClassifier {
	nb := MultinomialNBClassifier{}
	nb.condProb = make(map[string][]float64)
	nb.features = 0
	nb.trainingInstances = 0
	return &nb
}

func totalFreq(freq []float64) float64 {
	var sum float64

	// The +1 is for Laplace smoothing
	for _, f := range freq {
		sum += (f + 1)
	}
	return sum
}

// Fit computes the probabilities for the Multinomial Naive Bayes model.
func (nb *MultinomialNBClassifier) Fit(X base.FixedDataGrid) {
	classAttrs := X.AllClassAttributes()
	allAttrs := X.AllAttributes()
	featAttrs := base.AttributeDifference(allAttrs, classAttrs)
	for i := range featAttrs {
		if _, ok := featAttrs[i].(*base.FloatAttribute); !ok {
			panic(fmt.Sprintf("%v: Should be FloatAttribute", featAttrs[i]))
		}
	}
	featAttrSpecs := base.ResolveAttributes(X, featAttrs)

	if len(classAttrs) != 1 {
		panic("Only one class Attribute can be used")
	}

	_, nb.trainingInstances = X.Size()
	nb.attrs = featAttrs
	nb.features = len(featAttrs)

	// Number of instances in each class
	nb.classInstances = make(map[string]int)

	// Log(Prior probability) for each class
	nb.classPrior = make(map[string]float64)

	// For each class, the i-th position is the number of occurrences of feature
	// i in training instances from the class. We use float64 because GoLearn doesn't
	// have an 'IntegerAttribute'
	termFreq := make(map[string][]float64)
	X.MapOverRows(featAttrSpecs, func(docVector [][]byte, r int) (bool, error) {
		class := base.GetClass(X, r)
		if _, ok := termFreq[class]; !ok {
			termFreq[class] = make([]float64, nb.features)
		}

		nb.classInstances[class]++

		for feat := 0; feat < len(docVector); feat++ {
			val := base.UnpackBytesToFloat(docVector[feat])
			if val < 0 {
				panic(fmt.Sprintf("features should be positive: %d < 0", val))
			}
			termFreq[class][feat] += val
		}
		return true, nil
	})

	// Calculate Log(prior) and Log(conditional) for each class
	for class, classCount := range nb.classInstances {
		nb.classPrior[class] = math.Log(float64(classCount) / float64(nb.trainingInstances))
		nb.condProb[class] = make([]float64, nb.features)
		total := totalFreq(termFreq[class])
		for feat := 0; feat < nb.features; feat++ {
			// condProb is using Laplace smoothing
			nb.condProb[class][feat] = math.Log((termFreq[class][feat] + 1) / total)
		}
	}

}

// PredictOne classifies one instances.
func (nb *MultinomialNBClassifier) PredictOne(vector [][]byte) string {
	if nb.features == 0 {
		panic("Fit should be called before predicting")
	}

	if len(vector) != nb.features {
		panic("Different dimensions in Train and Test sets")
	}

	// Currently only the predicted class is returned.
	bestScore := -math.MaxFloat64
	bestClass := ""

	for class := range nb.classInstances {
		classScore := nb.classPrior[class]
		for feat := 0; feat < nb.features; feat++ {
			if base.UnpackBytesToFloat(vector[feat]) > 0 {
				classScore += nb.condProb[class][feat]
			}
		}

		if classScore > bestScore {
			bestScore = classScore
			bestClass = class
		}
	}

	return bestClass
}

// Predict classifies all instances in the FixedDataGrid, and is just a wrapper
// for the PredictOne function.
func (nb *MultinomialNBClassifier) Predict(what base.FixedDataGrid) base.FixedDataGrid {
	ret := base.GeneratePredictionVector(what)
	featAttrSpecs := base.ResolveAttributes(what, nb.attrs)

	what.MapOverRows(featAttrSpecs, func(row [][]byte, i int) (bool, error) {
		base.SetClass(ret, i, nb.PredictOne(row))
		return true, nil
	})

	return ret
}
