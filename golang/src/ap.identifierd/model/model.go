/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package model is for training new models and making predictions about client
// identities
package model

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"ap_common/network"

	"github.com/sjwhitworth/golearn/base"
	"github.com/sjwhitworth/golearn/filters"
	"github.com/sjwhitworth/golearn/trees"
)

const collectionDuration = 30 * time.Minute

type entity struct {
	identity string
	timeout  time.Time
	attrs    map[string]bool
}

// Entities is a vessel to collect data about clients. The data can be exported
// for later use as training data. For each new client we will collect data for
// 30 minutes. If a consumer of Entities (currently only ap.identifierd) is
// restarted then the timeout is reset.
type Entities struct {
	sync.Mutex
	dataMap map[uint64]*entity
}

// Prediction is a struct to communicate a new prediction from the named model.
// See Observations.Predict()
type Prediction struct {
	Model       string
	HwAddr      uint64
	Identity    string
	Probability float64
}

type client struct {
	rowIdx     int
	nbIdentity *Prediction
}

// Observations contains a subset of the data we observe from a client.
type Observations struct {
	sync.Mutex
	inst *base.DenseInstances
	spec []base.AttributeSpec

	// hwaddr -> client
	clients map[uint64]*client

	// Trained models used for prediction and the attributes used to train them.
	naiveBayes *MultinomialNBClassifier
	naiveAttrs []base.Attribute

	// attribute name -> column index
	attrMap map[string]int
}

func (e *Entities) getEntityLocked(hwaddr uint64) *entity {
	_, ok := e.dataMap[hwaddr]
	if !ok {
		e.dataMap[hwaddr] = newEntity()
	}
	return e.dataMap[hwaddr]
}

// AddIdentityHint records the client's hostname as seen by DHCP
func (e *Entities) AddIdentityHint(hwaddr uint64, name string) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	if d.identity == "" {
		d.identity = name
	}
}

// AddAttr adds the attribute 'a'
func (e *Entities) AddAttr(hwaddr uint64, a string) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	if time.Now().Before(d.timeout) {
		d.attrs[a] = true
	}
}

// WriteCSV exports an Entities struct to a CSV file, overwriting the file at
// 'path' if it already exists.
func (e *Entities) WriteCSV(path string) error {
	e.Lock()
	defer e.Unlock()

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)

	// Compute the union of all the attributes.
	union := make(map[string]bool)
	for _, ent := range e.dataMap {
		for a := range ent.attrs {
			union[a] = true
		}
	}

	// Make an attribute name -> column index map
	attrMap := make(map[string]int)

	// The first row in the CSV is the attribute header. The last item in the row
	// is the class attribute.
	header := make([]string, 0)
	header = append(header, "MAC Address")

	for q := range union {
		header = append(header, q)
		attrMap[q] = len(header) - 1
	}
	header = append(header, "Identity")
	w.Write(header)

	// Make a row for each entity
	for hw, ent := range e.dataMap {
		record := make([]string, len(header))
		for i := range header {
			record[i] = "0"
		}

		record[0] = network.Uint64ToHWAddr(hw).String()
		record[len(record)-1] = ent.identity

		for a := range ent.attrs {
			record[attrMap[a]] = "1"
		}
		w.Write(record)
	}

	w.Flush()
	return w.Error()
}

// loadTrainingData reads the CSV file at 'path' and returns a new
// DenseInstances for training, while also adding the training data's attributes
// to the Observations.
func (o *Observations) loadTrainingData(path string) (*base.DenseInstances, error) {
	trainData, err := base.ParseCSVToInstances(path, true)
	if err != nil {
		return nil, fmt.Errorf("failed to parse training data in %s: %s", path, err)
	}

	// Could call trainData.AllAttributes(), but the resulting slice is ordered
	// by AttributeGroup, making it difficult to add new rows of observed data
	attrs := base.ParseCSVGetAttributes(path, true)
	for i, a := range attrs {
		o.spec = append(o.spec, o.inst.AddAttribute(a))
		o.attrMap[a.GetName()] = i
	}

	last := len(attrs) - 1
	err = o.inst.AddClassAttribute(attrs[last])
	if err != nil {
		return nil, fmt.Errorf("failed to add class attribute: %s", err)
	}

	return trainData, nil
}

// Train loads the training data and trains the models. Eventually, we want to
// read a trained model from disk.
func (o *Observations) Train(path string) error {
	var err error

	o.Lock()
	defer o.Unlock()

	trainData, err := o.loadTrainingData(path)
	if err != nil {
		return fmt.Errorf("failed to load training data: %s", err)
	}

	o.naiveBayes = NewBayes(trainData)
	classAttrs := trainData.AllClassAttributes()
	allAttrs := trainData.AllAttributes()
	o.naiveAttrs = base.AttributeDifference(allAttrs, classAttrs)

	return nil
}

// SetByName sets the attribute 'attr' to the value '1' for the client
// specified by 'hwaddr'.
func (o *Observations) SetByName(hwaddr uint64, attr string) {
	o.Lock()
	defer o.Unlock()

	// Get this client's row index, or create a new row
	if _, ok := o.clients[hwaddr]; !ok {
		o.inst.Extend(1)
		_, rows := o.inst.Size()
		o.clients[hwaddr] = &client{
			rowIdx:     rows - 1,
			nbIdentity: &Prediction{"Naive Bayes", hwaddr, "Unknown", 0.0},
		}
	}
	row := o.clients[hwaddr].rowIdx

	// Get the attribute's column index. GoLearn doesn't allow adding new
	// attributes to FixedDataGrid
	col, ok := o.attrMap[attr]
	if !ok {
		return
	}

	o.inst.Set(o.spec[col], row, o.spec[col].GetAttribute().GetSysValFromString("1"))
}

// PredictBayes predicts identities for the observations using Naive Bayes
func (o *Observations) predictBayes(ch chan *Prediction) {
	predictions, probabilities := o.naiveBayes.Predict(o.inst)
	for _, c := range o.clients {
		newID := base.GetClass(predictions, c.rowIdx)
		idProb, err := strconv.ParseFloat(probabilities.RowString(c.rowIdx), 64)
		if err != nil {
			log.Printf("Failed to parse identity probability %q: %s\n",
				probabilities.RowString(c.rowIdx), err)
			continue
		}

		// The model returns the most probable identity. If the identity has
		// changed then the old identity is now less probable than (or equal to)
		// the new identity, so send an update. If the identity hasn't changed
		// but the model's confience has, send an update
		if newID != c.nbIdentity.Identity || idProb != c.nbIdentity.Probability {
			c.nbIdentity.Identity = newID
			c.nbIdentity.Probability = idProb
			ch <- c.nbIdentity
		}
	}
}

// Predict periodically runs predictions over the entire set of Observations.
// When a client's predicted identity changes a new Prediction is sent on the
// channel returned to the caller.
func (o *Observations) Predict() <-chan *Prediction {
	predCh := make(chan *Prediction)

	go func(ch chan *Prediction) {
		tick := time.NewTicker(time.Minute)
		for {
			<-tick.C
			o.Lock()
			o.predictBayes(ch)
			o.Unlock()
		}
	}(predCh)
	return predCh
}

// GetBayesIdentity returns a prediction and probability for hwaddr.
func (o *Observations) GetBayesIdentity(hwaddr uint64) (string, float64) {
	o.Lock()
	defer o.Unlock()

	c, ok := o.clients[hwaddr]
	if !ok {
		return "Unknown", 0.0
	}

	featAttrSpecs := base.ResolveAttributes(o.inst, o.naiveAttrs)

	vector := make([][]byte, len(featAttrSpecs))
	for i := 0; i < len(vector); i++ {
		vector[i] = o.inst.Get(featAttrSpecs[i], c.rowIdx)
	}
	return o.naiveBayes.PredictOne(vector)
}

func newEntity() *entity {
	ret := &entity{
		timeout: time.Now().Add(collectionDuration),
		attrs:   make(map[string]bool),
	}
	return ret
}

// FormatPortString formats a port attribute
func FormatPortString(protocol string, port int32) string {
	return fmt.Sprintf("%s %d", protocol, port)
}

// FormatMfgString formats a manufacturer attribute
func FormatMfgString(mfg int) string {
	return fmt.Sprintf("Mfg%d", mfg)
}

// NewEntities creates an empty Entities
func NewEntities() *Entities {
	ret := &Entities{
		dataMap: make(map[uint64]*entity),
	}
	return ret
}

// NewObservations creates an empty Observations
func NewObservations() *Observations {
	ret := &Observations{
		inst:    base.NewDenseInstances(),
		spec:    make([]base.AttributeSpec, 0),
		clients: make(map[uint64]*client),
		attrMap: make(map[string]int),
	}
	return ret
}

// Discretise transforms attributes into CategoricalAttributes
func Discretise(src base.FixedDataGrid) (*filters.ChiMergeFilter, error) {
	// XXX Need to understand the magic parameter
	filt := filters.NewChiMergeFilter(src, 0.999)
	for _, a := range base.NonClassFloatAttributes(src) {
		filt.AddAttribute(a)
	}

	if err := filt.Train(); err != nil {
		return nil, fmt.Errorf("could not train Chi-Merge filter: %s", err)
	}

	return filt, nil
}

// NewTree returns a new ID3 decision tree trained with trainData
func NewTree(trainData base.FixedDataGrid) (*trees.ID3DecisionTree, error) {
	// XXX Need to understand the magic parameter which controls train-prune split.
	id3Tree := trees.NewID3DecisionTree(0.4)
	err := id3Tree.Fit(trainData)
	if err != nil {
		return nil, fmt.Errorf("could not train ID3 tree: %s", err)
	}
	return id3Tree, nil
}

// Binarize transforms attributes into BinaryAttributes
func Binarize(src base.FixedDataGrid) (*filters.BinaryConvertFilter, error) {
	filt := filters.NewBinaryConvertFilter()
	for _, a := range base.NonClassAttributes(src) {
		filt.AddAttribute(a)
	}

	if err := filt.Train(); err != nil {
		return nil, fmt.Errorf("could not train binary filter: %s", err)
	}

	return filt, nil
}

// NewBayes returns a new Naive Bayes clasifier trained with trainData
func NewBayes(trainData base.FixedDataGrid) *MultinomialNBClassifier {
	naiveBayes := NewMultinomialNBClassifier()
	naiveBayes.Fit(trainData)
	return naiveBayes
}
