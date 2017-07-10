package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"

	"ap.identifierd/model"

	"github.com/sjwhitworth/golearn/base"
	"github.com/sjwhitworth/golearn/evaluation"
	"github.com/sjwhitworth/golearn/trees"
)

const (
	signal = 80
	noise  = 20
)

var (
	numMfg      = flag.Int("mfgs", 20, "Number of manufacturers.")
	numDev      = flag.Int("devs", 2, "Number of devices per manufacturer.")
	numSamples  = flag.Int("samples", 10, "Number of samples per device.")
	numFeatures = flag.Int("features", 30, "Number of features per device.")
	split       = flag.Float64("split", .4, "Train-test split")
	randomize   = flag.Bool("randomize", false, "Randomize 'devs', 'samples', and 'features'.")

	models = flag.String("models", "",
		"Comma separate list of models to evaluate.\n\tCurrently support 'tree', 'bern', 'multi'.")
)

func vector(size int, prob int) []string {
	ret := make([]string, size)
	for i := 0; i < size; i++ {
		if rand.Intn(100) < prob {
			ret[i] = "1"
		} else {
			ret[i] = "0"
		}
	}
	return ret
}

func generateData() (*base.DenseInstances, error) {
	data := make([][]string, 0)
	devices := make([]string, 0)
	numCols := 0
	numRows := 0
	for i := 0; i < *numMfg; i++ {
		if *randomize {
			*numDev = rand.Intn(*numDev) + 1
		}
		for d := 0; d < *numDev; d++ {
			if *randomize {
				*numFeatures = rand.Intn(*numFeatures) + 1
			}

			for r := 0; r < numRows; r++ {
				data[r] = append(data[r], vector(*numFeatures, noise)...)
			}

			if *randomize {
				*numSamples = rand.Intn(*numSamples) + 1
			}

			for s := 0; s < *numSamples; s++ {
				record := make([]string, 0)

				// This is the Manufacturer ID. Including this hurts multinomial Bayes
				record = append(record, strconv.Itoa(i))

				record = append(record, vector(numCols, noise)...)
				record = append(record, vector(*numFeatures, signal)...)

				devices = append(devices, fmt.Sprintf("%d_dev%d", i, d))
				data = append(data, record)
				numRows++
			}
			numCols += *numFeatures
		}
	}

	rawData := base.NewDenseInstances()
	attrSpec := make([]base.AttributeSpec, 0)
	for i := 0; i < numCols+1; i++ {
		attr := base.NewFloatAttribute(fmt.Sprintf("Attr%d", i))
		attrSpec = append(attrSpec, rawData.AddAttribute(attr))
	}

	clsAttr := base.NewCategoricalAttribute()
	clsAttr.SetName("Device")
	attrSpec = append(attrSpec, rawData.AddAttribute(clsAttr))
	if err := rawData.AddClassAttribute(clsAttr); err != nil {
		return nil, err
	}
	rawData.Extend(numRows)

	for i := 0; i < numRows; i++ {
		for j := 0; j < numCols+1; j++ {
			rawData.Set(attrSpec[j], i, attrSpec[j].GetAttribute().GetSysValFromString(data[i][j]))
		}
		rawData.Set(attrSpec[numCols+1], i, attrSpec[numCols+1].GetAttribute().GetSysValFromString(devices[i]))
	}
	return rawData, nil
}

func runTree(rawData *base.DenseInstances) error {
	rawDataFilt, err := model.Discretise(rawData)
	if err != nil {
		return fmt.Errorf("failed to discretise: %v", err)
	}
	trainData, testData := base.InstancesTrainTestSplit(rawDataFilt, *split)

	id3Tree := trees.NewID3DecisionTree(0.6)
	err = id3Tree.Fit(trainData)
	if err != nil {
		return fmt.Errorf("failed to fit ID3 tree: %v", err)
	}

	predictions, err := id3Tree.Predict(testData)
	if err != nil {
		return fmt.Errorf("failed to predict with ID3 tree: %v", err)
	}

	cf, err := evaluation.GetConfusionMatrix(testData, predictions)
	if err != nil {
		return fmt.Errorf("unable to get confusion matrix: %v", err)
	}
	fmt.Printf("ID3 Tree overall accuracy: %f\n", evaluation.GetAccuracy(cf))
	return nil
}

func runBern(rawData *base.DenseInstances) error {
	rawDataFilt, err := model.Binarize(rawData)
	if err != nil {
		return fmt.Errorf("failed to binarize: %v", err)
	}
	trainData, testData := base.InstancesTrainTestSplit(rawDataFilt, *split)

	naiveBayes, err := model.NewBayes(trainData)
	if err != nil {
		return fmt.Errorf("failed to make Naive Bayes: %v", err)
	}

	predictions := naiveBayes.Predict(testData)
	if err != nil {
		return fmt.Errorf("failed to predict: %v", err)
	}

	cf, err := evaluation.GetConfusionMatrix(testData, predictions)
	if err != nil {
		return fmt.Errorf("unable to get confusion matrix: %v", err)
	}
	fmt.Printf("Bernoulli NB overall accuracy: %f\n", evaluation.GetAccuracy(cf))
	return nil
}

func runMulti(rawData *base.DenseInstances) error {
	trainData, testData := base.InstancesTrainTestSplit(rawData, *split)
	multiBayes := model.NewMultinomialNBClassifier()
	multiBayes.Fit(trainData)

	predictions := multiBayes.Predict(testData)
	cf, err := evaluation.GetConfusionMatrix(testData, predictions)
	if err != nil {
		return fmt.Errorf("unable to get confusion matrix: %v", err)
	}
	fmt.Printf("Multinomial NB overall accuracy: %f\n", evaluation.GetAccuracy(cf))
	return nil
}

func main() {
	flag.Parse()

	rawData, err := generateData()
	if err != nil {
		fmt.Println("failed to generate data:", err)
		os.Exit(1)
	}

	if strings.Contains(*models, "tree") {
		if err := runTree(rawData); err != nil {
			fmt.Println("ID3 tree fit/predict failed:", err)
		}
	}

	if strings.Contains(*models, "bern") {
		if err := runBern(rawData); err != nil {
			fmt.Println("Bernoulli NB fit/predict failed:", err)
		}
	}

	if strings.Contains(*models, "multi") {
		if err := runMulti(rawData); err != nil {
			fmt.Println("Multinomial NB fit/predict failed:", err)
		}
	}
}
