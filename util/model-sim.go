/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"

	"ap.identifierd/model"

	"github.com/gonum/plot"
	"github.com/gonum/plot/plotter"
	"github.com/gonum/plot/plotutil"
	"github.com/gonum/plot/vg"

	"github.com/sjwhitworth/golearn/base"
	"github.com/sjwhitworth/golearn/evaluation"
	"github.com/sjwhitworth/golearn/trees"
)

const (
	// Samples of the same device are highly correlated
	signal = 80
	// Samples of different devices from the same mfg are somewhat correlated
	noiseSameMfg = 40
	// Samples of devices from different mfgs are not correlated (except for noise)
	noiseDiffMfg = 20
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

	reliabilityPlot *plot.Plot
)

type accuracyBin struct {
	correct int
	total   int
	probSum float64
}

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

		mfgFeatures := 0
		mfgRows := 0
		for d := 0; d < *numDev; d++ {
			if *randomize {
				*numFeatures = rand.Intn(*numFeatures) + 1
				*numSamples = rand.Intn(*numSamples) + 1
			}

			// Pad the existing rows to account for the features added below
			for r := 0; r < numRows-mfgRows; r++ {
				data[r] = append(data[r], vector(*numFeatures, noiseDiffMfg)...)
			}

			for r := numRows - mfgRows; r < numRows; r++ {
				data[r] = append(data[r], vector(*numFeatures, noiseSameMfg)...)
			}

			for s := 0; s < *numSamples; s++ {
				record := make([]string, 0)

				// Manufacturer ID is a binary attribute. The first numMfg
				// columns are block diagonal
				record = append(record, vector(*numMfg, 0)...)
				record[i] = "1"

				// Features from a different manufacturer
				record = append(record, vector(numCols, noiseDiffMfg)...)

				// Features from the same mfg but different device
				record = append(record, vector(mfgFeatures, noiseSameMfg)...)

				// Features for this device
				record = append(record, vector(*numFeatures, signal)...)

				devices = append(devices, fmt.Sprintf("%d_dev%d", i, d))
				data = append(data, record)
				numRows++
				mfgRows++
			}
			mfgFeatures += *numFeatures
		}
		numCols += mfgFeatures
	}

	totalCols := *numMfg + numCols
	rawData := base.NewDenseInstances()
	attrSpec := make([]base.AttributeSpec, 0)
	for i := 0; i < totalCols; i++ {
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
		for j := 0; j < totalCols; j++ {
			rawData.Set(attrSpec[j], i, attrSpec[j].GetAttribute().GetSysValFromString(data[i][j]))
		}
		rawData.Set(attrSpec[totalCols], i, attrSpec[totalCols].GetAttribute().GetSysValFromString(devices[i]))
	}

	return rawData, nil
}

// Generate points (x_i, y_i) to plot a reliability diagram. Start by binning
// the data into 10 bins. Then
//   x_i = accuracy
//       = fraction of test cases in bin i where the predicted class is the true class
//   y_i = confidence
//       = average probability of test cases in bin i
//
// Reliability diagrams are nice visual tools to evaluate model reliability, but
// they don't take into account the number of test cases in each bin. The
// Expected Calibration Error is a summary statistic which is the weighted average
// of the difference between accuracy and confidence. Smaller ECE is better.
//
// Reference: "On Calibration of Modern Neural Networks" by C. Guo, G. Pleiss,
// Y. Sun, and K. Weinberger
func reliability(data, pred, prob base.FixedDataGrid) ([][]float64, float64, error) {
	numBins := 10
	acc := make(map[int]*accuracyBin)

	_, dataRows := data.Size()
	_, predRows := pred.Size()
	_, probRows := prob.Size()
	if dataRows != predRows || predRows != probRows {
		return nil, 0, fmt.Errorf("number of rows differ: %d, %d, %d",
			dataRows, predRows, probRows)
	}

	for i := 0; i < numBins; i++ {
		acc[i] = &accuracyBin{}
	}

	for i := 0; i < dataRows; i++ {
		classProb, err := strconv.ParseFloat(prob.RowString(i), 64)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to parse class probability %q: %s",
				prob.RowString(i), err)
		}
		binNum := int(classProb * float64(numBins))
		if binNum == 10 {
			binNum--
		}
		acc[binNum].total++
		acc[binNum].probSum += classProb

		realClass := base.GetClass(data, i)
		predClass := base.GetClass(pred, i)
		if realClass == predClass {
			acc[binNum].correct++
		}
	}

	var ece float64
	rel := make([][]float64, numBins)
	for i := 0; i < numBins; i++ {
		rel[i] = make([]float64, 2)
		if acc[i].total == 0 {
			continue
		}

		accuracy := float64(acc[i].correct) / float64(acc[i].total)
		confidence := float64(acc[i].probSum) / float64(acc[i].total)

		rel[i][0] = confidence
		rel[i][1] = accuracy
		ece += (float64(acc[i].total) / float64(dataRows)) * math.Abs(accuracy-confidence)
	}

	return rel, ece, nil
}

func runTree(rawData *base.DenseInstances) error {
	var output bytes.Buffer
	output.WriteString("ID3 Tree:\n")

	filt, err := model.Discretise(rawData)
	if err != nil {
		return fmt.Errorf("failed to discretise: %v", err)
	}
	rawDataFilt := base.NewLazilyFilteredInstances(rawData, filt)
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
	output.WriteString(fmt.Sprintf("\tAccuracy: %f\n", evaluation.GetAccuracy(cf)))
	fmt.Print(output.String())
	return nil
}

func runBern(rawData *base.DenseInstances) ([][]float64, error) {
	var output bytes.Buffer
	output.WriteString("Bernoulli NB:\n")

	filt, err := model.Binarize(rawData)
	if err != nil {
		return nil, fmt.Errorf("failed to create binary filter: %v", err)
	}
	rawDataFilt := base.NewLazilyFilteredInstances(rawData, filt)
	trainData, testData := base.InstancesTrainTestSplit(rawDataFilt, *split)

	naiveBayes := model.NewBayes(trainData)
	predictions, probabilities := naiveBayes.Predict(testData)

	cf, err := evaluation.GetConfusionMatrix(testData, predictions)
	if err != nil {
		return nil, fmt.Errorf("unable to get confusion matrix: %v", err)
	}
	output.WriteString(fmt.Sprintf("\tAccuracy: %f\n", evaluation.GetAccuracy(cf)))

	rel, ece, err := reliability(testData, predictions, probabilities)
	if err != nil {
		return nil, fmt.Errorf("unable to quantify reliability: %v", err)
	}
	output.WriteString(fmt.Sprintf("\tECE: %f\n", ece))
	fmt.Print(output.String())

	return rel, nil
}

func runMulti(rawData *base.DenseInstances) ([][]float64, error) {
	var output bytes.Buffer
	output.WriteString("Multinomial NB:\n")

	trainData, testData := base.InstancesTrainTestSplit(rawData, *split)

	multiBayes := model.NewMultinomialNBClassifier()
	multiBayes.Fit(trainData)
	predictions, probabilities := multiBayes.Predict(testData)

	cf, err := evaluation.GetConfusionMatrix(testData, predictions)
	if err != nil {
		return nil, fmt.Errorf("unable to get confusion matrix: %v", err)
	}
	output.WriteString(fmt.Sprintf("\tAccuracy: %f\n", evaluation.GetAccuracy(cf)))

	rel, ece, err := reliability(testData, predictions, probabilities)
	if err != nil {
		return nil, fmt.Errorf("unable to quantify reliability: %v", err)
	}
	output.WriteString(fmt.Sprintf("\tECE: %f\n", ece))
	fmt.Print(output.String())

	return rel, nil
}

func reliabilityPoints(rel [][]float64) plotter.XYs {
	pts := make(plotter.XYs, len(rel))
	for i := range pts {
		pts[i].X = rel[i][0]
		pts[i].Y = rel[i][1]
	}
	return pts
}

func init() {
	var err error
	reliabilityPlot, err = plot.New()
	if err != nil {
		panic(fmt.Sprintf("failed to make new plot: %s\n", err))
	}

	reliabilityPlot.Title.Text = "Model Reliability"
	reliabilityPlot.X.Label.Text = "Confidence"
	reliabilityPlot.Y.Label.Text = "Accuracy"
	reliabilityPlot.X.Min = 0
	reliabilityPlot.X.Max = 1
	reliabilityPlot.Y.Min = 0
	reliabilityPlot.Y.Max = 1

	// A well calibrated model should be close to the line y = x
	line := plotter.NewFunction(func(x float64) float64 { return x })
	line.Dashes = []vg.Length{vg.Points(4), vg.Points(5)}
	line.Width = vg.Points(4)
	line.Color = color.RGBA{R: 255, A: 255}
	reliabilityPlot.Add(line)
}

func main() {
	var err error
	var bernRel, multRel [][]float64

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
		if bernRel, err = runBern(rawData); err != nil {
			fmt.Println("Bernoulli NB fit/predict failed:", err)
		}
	}

	if strings.Contains(*models, "multi") {
		if multRel, err = runMulti(rawData); err != nil {
			fmt.Println("Multinomial NB fit/predict failed:", err)
		}
	}

	if err != nil {
		os.Exit(1)
	}

	err = plotutil.AddLinePoints(reliabilityPlot,
		"Bern", reliabilityPoints(bernRel),
		"Multi", reliabilityPoints(multRel))
	if err != nil {
		fmt.Println("failed to add points: %s\n", err)
	}

	err = reliabilityPlot.Save(5*vg.Inch, 5*vg.Inch, "model-reliability.png")
	if err != nil {
		fmt.Println("failed to save plot: %s\n", err)
	}

}
