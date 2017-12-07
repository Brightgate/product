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

// Test the accuracy of various classification models in predicting the device
// ID of simulated IoT devices. Also report the reliability and calibration of
// the device ID probability. Prospective models should be tested here before
// being integrated with ap.identifierd or any other daemon.
//
// Simulated device data follows these assumptions:
//   1) Each device is characterized by a number of features (numFeatures).
//   2) Each sample of a given device will exhibit 'signal' percent (ie 80%) of
//      numFeatures.
//   3) Device feature sets may intersect:
//        a) Devices from the same manufactuerer exhibit 'noiseSameMfg' percent
//           (ie 40%) of each feature set within the same manufacturer group.
//        b) Devices from different manufacturers exhibit 'noiseDiffMfg' percent
//           (ie 20%) of each feature set from different manufacturers.
//
// To use this utility you should first build it:
// $ go build path/to/Product/util/model-sim.go
//
// Examples:
// # Test Multinomial Naive Bayes and TensorFlow linear classifier with defaults
// $ ./model-sim -models mult,tf
//
// # Test TensorFlow linear classifier with 5 devices per mfg, 15 samples per dev
// $ ./model-sim -devs 5 -samples 10 -models tf
//
// # Randomize the number of devices, samples, and features (args are used as maximums)
// $ ./model-sim -devs 5 -samples 10 -models tf -randomize
package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"go/build"
	"image/color"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"bg/ap_common/model"

	tf "github.com/tensorflow/tensorflow/tensorflow/go"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

const (
	// Samples of the same device are highly correlated
	signal = 80
	// Samples of different devices from the same mfg are somewhat correlated
	noiseSameMfg = 40
	// Samples of devices from different mfgs are not correlated (except for noise)
	noiseDiffMfg = 20
	// Number of bins for accuracy plots
	numBins = 10

	tfTrainPath = "tf_data_train.csv"
	tfTestPath  = "tf_data_test.csv"
)

var (
	numMfg      = flag.Int("mfgs", 20, "Number of manufacturers.")
	numDev      = flag.Int("devs", 2, "Number of devices per manufacturer.")
	numSamples  = flag.Int("samples", 10, "Number of samples per device.")
	numFeatures = flag.Int("features", 30, "Number of features per device.")
	split       = flag.Float64("split", .4, "Train-test split")
	randomize   = flag.Bool("randomize", false, "Randomize 'devs', 'samples', and 'features'.")

	models = flag.String("models", "tf",
		"Comma separate list of models to evaluate.\n\tCurrently support "+
			"'tf' (TensorFlow linear classifier).")

	reliabilityPlot *plot.Plot

	// TensorFlow LinearClassifier.
	tflcPath = regexp.MustCompile(`Linear model saved to (.*?)\n`)
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

func tfCSV(data [][]string, path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %s", path, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)

	for _, row := range data {
		w.Write(row)
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("failed to write %s: %s", path, err)
	}
	return nil
}

func tfData(data [][]string, devices []string) error {
	trainData := make([][]string, 0)
	testData := make([][]string, 0)
	header := make([]string, 0)
	numCols := len(data[0])
	trainRows := 0
	testRows := 0
	devMap := make(map[string]string)
	devNum := 0

	for i := 0; i < numCols; i++ {
		header = append(header, fmt.Sprintf("Attr%d", i))
	}
	header = append(header, "Device ID")
	trainData = append(trainData, header)
	testData = append(testData, header)

	for i, row := range data {
		if len(row) != numCols {
			return fmt.Errorf("row length %d != %d", len(row), numCols)
		}

		dev := devices[i]
		if _, ok := devMap[dev]; !ok {
			devMap[dev] = fmt.Sprintf("%d", devNum)
			devNum++
		}
		row = append(row, devMap[dev])

		if rand.Float64() < *split {
			testRows++
			testData = append(testData, row)
		} else {
			trainRows++
			trainData = append(trainData, row)
		}
	}

	if err := tfCSV(trainData, tfTrainPath); err != nil {
		return fmt.Errorf("failed to write training data: %s", err)
	}

	if err := tfCSV(testData, tfTestPath); err != nil {
		return fmt.Errorf("failed to write testing data: %s", err)
	}
	return nil
}

func generateData() ([][]string, []string) {
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

	return data, devices
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
func computeMetrics(acc map[int]*accuracyBin) ([][]float64, float64) {
	var ece float64
	rel := make([][]float64, numBins)
	binTotal := 0

	for i := 0; i < numBins; i++ {
		binTotal += acc[i].total
	}

	for i := 0; i < numBins; i++ {
		rel[i] = make([]float64, 2)
		if acc[i].total == 0 {
			continue
		}

		accuracy := float64(acc[i].correct) / float64(acc[i].total)
		confidence := float64(acc[i].probSum) / float64(acc[i].total)

		rel[i][0] = confidence
		rel[i][1] = accuracy
		ece += (float64(acc[i].total) / float64(binTotal)) *
			math.Abs(accuracy-confidence)
	}

	return rel, ece
}

func binUpdate(trueClass, predClass string, classProb float64, acc map[int]*accuracyBin) {
	binNum := int(classProb * float64(numBins))
	if binNum == 10 {
		binNum--
	}
	acc[binNum].total++
	acc[binNum].probSum += classProb

	if trueClass == predClass {
		acc[binNum].correct++
	}
}

func runTensorFlow() ([][]float64, error) {
	var output bytes.Buffer
	output.WriteString("TensorFlow LinearClassifier:\n")

	// Train and export the model in Python, then load the saved model.
	cmdPath := build.Default.GOPATH + "/../util/tf-train-export.py"
	cmd := exec.Command("python", cmdPath, "-train", tfTrainPath)
	stdOutstdErr, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("TensorFlow training failed with %s:\n%s", err,
			stdOutstdErr)
	}

	path := tflcPath.FindStringSubmatch(string(stdOutstdErr))[1]
	savedModel, err := tf.LoadSavedModel(path, []string{"serve"}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to LoadSavedModel: %s", err)
	}

	testForInputs := savedModel.Graph.Operation(model.TFInput)
	if testForInputs == nil {
		return nil, fmt.Errorf("wrong input path %s", model.TFInput)
	}

	testForProbs := savedModel.Graph.Operation(model.TFProb)
	if testForProbs == nil {
		return nil, fmt.Errorf("wrong output path %s", model.TFProb)
	}

	testForClasses := savedModel.Graph.Operation(model.TFClassID)
	if testForClasses == nil {
		return nil, fmt.Errorf("wrong output path", model.TFClassID)
	}

	// Load the testing set which was created with tfData. The JSON string is
	// a tf.Example protobuf. The key 'x' to our only feature must match the name
	// we gave our feature column during training.
	//
	// To turn the string into the correct tf.Example format we need to create
	// a graph with certain op's, then send the output of running that graph to
	// our model for input.
	testFile, err := os.Open(tfTestPath)
	if err != nil {
		log.Fatalf("failed to open data file %s\n", tfTestPath)
	}
	defer testFile.Close()
	reader := csv.NewReader(testFile)

	// Discard the header
	reader.Read()

	acc := make(map[int]*accuracyBin)
	for i := 0; i < numBins; i++ {
		acc[i] = &accuracyBin{}
	}

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("failed to read from %s: %s\n", tfTestPath, err)
		}

		last := len(row) - 1
		trueClass := row[last]
		example, err := model.FormatTFExample(model.TFFeaturesKey, strings.Join(row[:last], ","))
		if err != nil {
			return nil, fmt.Errorf("failed to make tf.Example: %s", err)
		}

		feeds := make(map[tf.Output]*tf.Tensor)
		feeds[testForInputs.Output(0)] = example[0]

		fetchProb := make([]tf.Output, 1)
		fetchProb[0] = testForProbs.Output(0)

		fetchClass := make([]tf.Output, 1)
		fetchClass[0] = testForClasses.Output(0)

		var runResult []*tf.Tensor
		var runErr error

		// Fetch the class first. The runResult is the class id which provides
		// an index into the fetched probabilities
		runResult, runErr = savedModel.Session.Run(feeds, fetchClass, nil)
		if runErr != nil {
			return nil, fmt.Errorf("fetching class ID failed: %s", runErr)
		}
		predClass := runResult[0].Value().([]int64)[0]

		runResult, runErr = savedModel.Session.Run(feeds, fetchProb, nil)
		if runErr != nil {
			return nil, fmt.Errorf("fetching probabilities failed: %s", runErr)
		}
		predProb := runResult[0].Value().([][]float32)[0]
		classProb := float64(predProb[predClass])

		binUpdate(trueClass, strconv.FormatInt(predClass, 10), classProb, acc)
	}

	correctCount := 0
	totalCount := 0
	for i := 0; i < numBins; i++ {
		correctCount += acc[i].correct
		totalCount += acc[i].total
	}

	output.WriteString(fmt.Sprintf("\tAccuracy: %f\n", float32(correctCount)/float32(totalCount)))
	rel, ece := computeMetrics(acc)
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
	var tfRel [][]float64

	flag.Parse()
	data, devices := generateData()
	if strings.Contains(*models, "tf") {
		if err = tfData(data, devices); err != nil {
			fmt.Printf("failed to create TensorFlow data: %s\n", err)
		} else if tfRel, err = runTensorFlow(); err != nil {
			fmt.Printf("TensorFlow failed: %s\n", err)
		}
	}

	if err != nil {
		os.Exit(1)
	}

	err = plotutil.AddLinePoints(reliabilityPlot,
		"TFLC", reliabilityPoints(tfRel))
	if err != nil {
		fmt.Println("failed to add points: %s\n", err)
	}

	err = reliabilityPlot.Save(5*vg.Inch, 5*vg.Inch, "model-reliability.png")
	if err != nil {
		fmt.Println("failed to save plot: %s\n", err)
	}

}
