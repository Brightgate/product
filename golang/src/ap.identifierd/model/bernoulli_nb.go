package model

import (
	"fmt"
	"math"

	"github.com/sjwhitworth/golearn/base"
)

// This is (mostly) a cut-and-paste copy of golearn/naive/bernoulli_nb.go except:
//   1) We precompute the class prior probabilities.
//   2) Fix a "index out of bounds" panic.
//   3) Apply softmax (https://en.wikipedia.org/wiki/Softmax_function) to class
//      scores to return class probabilities.
//

// BernoulliNBClassifier holds the data necessary for classification.
type BernoulliNBClassifier struct {
	base.BaseEstimator
	// Log(Prior probability) for each class
	classPrior map[string]float64
	// Conditional probability for each term. This vector should be
	// accessed in the following way: p(f|c) = condProb[c][f].
	// Logarithm is used in order to avoid underflow.
	condProb map[string][]float64
	// Number of instances in each class. This is necessary in order to
	// calculate the laplace smooth value during the Predict step.
	classInstances map[string]int
	// Number of instances used in training.
	trainingInstances int
	// Number of features used in training
	features int
	// Attributes used to Train
	attrs []base.Attribute
}

// NewBernoulliNBClassifier creates a new Bernoulli Naive Bayes Classifier.
func NewBernoulliNBClassifier() *BernoulliNBClassifier {
	nb := BernoulliNBClassifier{}
	nb.condProb = make(map[string][]float64)
	nb.features = 0
	nb.trainingInstances = 0
	return &nb
}

// Fit computes the probabilities for the Bernoulli Naive Bayes model.
func (nb *BernoulliNBClassifier) Fit(X base.FixedDataGrid) {
	classAttrs := X.AllClassAttributes()
	allAttrs := X.AllAttributes()
	featAttrs := base.AttributeDifference(allAttrs, classAttrs)
	for i := range featAttrs {
		if _, ok := featAttrs[i].(*base.BinaryAttribute); !ok {
			panic(fmt.Sprintf("%v: Should be BinaryAttribute", featAttrs[i]))
		}
	}
	featAttrSpecs := base.ResolveAttributes(X, featAttrs)

	// Check that only one classAttribute is defined
	if len(classAttrs) != 1 {
		panic("Only one class Attribute can be used")
	}

	// Number of features and instances in this training set
	_, nb.trainingInstances = X.Size()
	nb.attrs = featAttrs
	nb.features = len(featAttrs)

	// Number of instances in class
	nb.classInstances = make(map[string]int)

	// Log(Prior probability) for each class
	nb.classPrior = make(map[string]float64)

	// Number of documents with given term (by class)
	docsContainingTerm := make(map[string][]int)

	// This algorithm could be vectorized after binarizing the data
	// matrix. Since mat64 doesn't have this function, a iterative
	// version is used.
	X.MapOverRows(featAttrSpecs, func(docVector [][]byte, r int) (bool, error) {
		class := base.GetClass(X, r)
		if _, ok := docsContainingTerm[class]; !ok {
			docsContainingTerm[class] = make([]int, nb.features)
		}

		// increment number of instances in class
		nb.classInstances[class]++

		for feat := 0; feat < len(docVector); feat++ {
			v := docVector[feat]
			// In Bernoulli Naive Bayes the presence and absence of
			// features are considered. All non-zero values are
			// treated as presence.
			if v[0] > 0 {
				// Update number of times this feature appeared within
				// given label.
				t, ok := docsContainingTerm[class]
				if !ok {
					panic(fmt.Sprintf("missing class %s", class))
				}
				t[feat]++
			}
		}
		return true, nil
	})

	// Calculate Log(prior) and conditional probabilities for each class
	for class, classCount := range nb.classInstances {
		nb.classPrior[class] = math.Log(float64(classCount) / float64(nb.trainingInstances))
		nb.condProb[class] = make([]float64, nb.features)
		for feat := 0; feat < nb.features; feat++ {
			classTerms, _ := docsContainingTerm[class]
			numDocs := classTerms[feat]
			docsInClass, _ := nb.classInstances[class]

			classCondProb, _ := nb.condProb[class]
			// Calculate conditional probability with laplace smoothing
			classCondProb[feat] = float64(numDocs+1) / float64(docsInClass+1)
		}
	}
}

// PredictOne uses the trained model to predict the test vector's class and class
// probability.
func (nb *BernoulliNBClassifier) PredictOne(vector [][]byte) (string, float64) {
	if nb.features == 0 {
		panic("Fit should be called before predicting")
	}

	if len(vector) != nb.features {
		panic("Different dimensions in Train and Test sets")
	}

	bestScore := -math.MaxFloat64
	bestIdx := 0
	bestClass := ""

	scale := math.MaxFloat64
	scores := make([]float64, 0)

	for class := range nb.classInstances {
		// Init classScore with log(prior)
		classScore := nb.classPrior[class]
		for f := 0; f < nb.features; f++ {
			if vector[f][0] > 0 {
				// Test document has feature c
				classScore += math.Log(nb.condProb[class][f])
			} else {
				if nb.condProb[class][f] == 1.0 {
					// special case when prob = 1.0, consider laplace
					// smooth
					classScore += math.Log(1.0 / float64(nb.classInstances[class]+1))
				} else {
					classScore += math.Log(1.0 - nb.condProb[class][f])
				}
			}
		}

		scores = append(scores, classScore)
		if classScore < scale {
			scale = classScore
		}

		if classScore > bestScore {
			bestScore = classScore
			bestIdx = len(scores) - 1
			bestClass = class
		}
	}

	// Apply softmax
	var norm float64
	for i, s := range scores {
		eScore := math.Exp(s - scale)
		scores[i] = eScore
		norm += eScore
	}

	return bestClass, scores[bestIdx] / norm
}

// Predict classifies all instances in the FixedDataGrid, and is just a wrapper
// for the PredictOne function.
func (nb *BernoulliNBClassifier) Predict(what base.FixedDataGrid) (base.FixedDataGrid, base.FixedDataGrid) {
	classes := base.GeneratePredictionVector(what)
	featAttrSpecs := base.ResolveAttributes(what, nb.attrs)

	scores := base.NewDenseInstances()
	a := base.NewFloatAttribute("Class Score")
	spec := scores.AddAttribute(a)
	_, rowCount := what.Size()
	scores.Extend(rowCount)

	what.MapOverRows(featAttrSpecs, func(row [][]byte, i int) (bool, error) {
		c, s := nb.PredictOne(row)
		scores.Set(spec, i, base.PackFloatToBytes(s))
		base.SetClass(classes, i, c)
		return true, nil
	})

	return classes, scores
}
