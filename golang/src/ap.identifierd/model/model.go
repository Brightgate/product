/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package model is for training new models (currently only ID3 decision trees,
// Bernoulli Naive Bayes, and Multinomial Naive Bayes) and making predictions
// about client identities
package model

import (
	"encoding/csv"
	"fmt"
	"os"
	"sync"
	"time"

	"ap_common/network"

	"github.com/sjwhitworth/golearn/base"
	"github.com/sjwhitworth/golearn/filters"
	"github.com/sjwhitworth/golearn/naive"
	"github.com/sjwhitworth/golearn/trees"
)

type entity struct {
	identity string

	// Keep these separate for easier to read CSV files. Can this be done
	// with AttributeGroup?
	dnsAttr  map[string]bool
	portAttr map[string]bool
}

// Entities is a vessel to collect data about clients. The data can be exported
// for later use as training data.
type Entities struct {
	sync.Mutex
	dataMap map[uint64]*entity
}

// Prediction is a struct to communicate a new prediction from the named model.
// See Observations.Predict()
type Prediction struct {
	Model    string
	HwAddr   uint64
	Identity string
}

type client struct {
	rowIdx      int
	nbIdentity  string
	id3Identity string
}

// Observations contains a subset of the data we observe from a client.
type Observations struct {
	sync.Mutex
	inst *base.DenseInstances
	spec []base.AttributeSpec

	// hwaddr -> client
	clients map[uint64]*client

	// Trained prospective models used for prediction, the attributes used to
	// train them, and corresponding filters for 'inst'.
	naiveBayes    *naive.BernoulliNBClassifier
	naiveAttrs    []base.Attribute
	naiveFiltered *base.LazilyFilteredInstances
	id3Tree       *trees.ID3DecisionTree
	id3Filtered   *base.LazilyFilteredInstances

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
func (e *Entities) AddIdentityHint(hwaddr uint64, mfg, name string) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.identity = name
}

// AddDNS records a DNS question name.
func (e *Entities) AddDNS(hwaddr uint64, qname string) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.dnsAttr[qname] = true
}

// AddPort records an open port.
func (e *Entities) AddPort(hwaddr uint64, port string) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.portAttr[port] = true
}

// WriteCSV exports an Entities struct to a CSV file, overwriting the file at
// 'path' if it already exists
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
	unionDNS := make(map[string]bool)
	unionPort := make(map[string]bool)
	for _, ent := range e.dataMap {
		for q := range ent.dnsAttr {
			unionDNS[q] = true
		}

		for p := range ent.portAttr {
			unionPort[p] = true
		}
	}

	// Make an attribute name -> column index map
	attrMap := make(map[string]int)

	// The first row in the CSV is the attribute header. The last item in the row
	// is the class attribute.
	header := make([]string, 0)
	header = append(header, "MAC Address")
	attrMap["MAC Address"] = len(header) - 1

	for q := range unionDNS {
		header = append(header, q)
		attrMap[q] = len(header) - 1
	}
	for p := range unionPort {
		header = append(header, p)
		attrMap[p] = len(header) - 1
	}

	header = append(header, "Identity")
	attrMap["Identity"] = len(header) - 1
	w.Write(header)

	// Make a row for each entity
	for a, ent := range e.dataMap {
		record := make([]string, len(header))
		for i := range header {
			record[i] = "0"
		}

		hwaddr := network.Uint64ToHWAddr(a)
		record[0] = hwaddr.String()
		record[len(record)-1] = ent.identity

		for q := range ent.dnsAttr {
			record[attrMap[q]] = "1"
		}
		for p := range ent.portAttr {
			record[attrMap[p]] = "1"
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

	// We need to add one value to the class attribute, which is a
	// CategoricalAttribute, because GetStringFromSysVal() panics if the attribute
	// has no values. Perhaps we should fix this to just print the empty string
	o.spec[last].GetAttribute().GetSysValFromString("Unknown")

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

	disFilt, err := Discretise(trainData)
	if err != nil {
		return fmt.Errorf("failed to create discrete filter: %s", err)
	}

	trainDataFiltered := base.NewLazilyFilteredInstances(trainData, disFilt)
	if o.id3Tree, err = NewTree(trainDataFiltered); err != nil {
		return fmt.Errorf("failed to make new ID3 tree: %s", err)
	}
	o.id3Filtered = base.NewLazilyFilteredInstances(o.inst, disFilt)

	binFilt, err := Binarize(trainData)
	if err != nil {
		return fmt.Errorf("failed to create binary filter: %s", err)
	}

	trainDataFiltered = base.NewLazilyFilteredInstances(trainData, binFilt)
	o.naiveBayes = NewBayes(trainDataFiltered)
	classAttrs := trainDataFiltered.AllClassAttributes()
	allAttrs := trainDataFiltered.AllAttributes()
	o.naiveAttrs = base.AttributeDifference(allAttrs, classAttrs)
	o.naiveFiltered = base.NewLazilyFilteredInstances(o.inst, binFilt)

	return nil
}

// SetByName sets the attribute 'attr' to the value 'val' for the client
// specified by 'hwaddr'.
func (o *Observations) SetByName(hwaddr uint64, attr, val string) {
	o.Lock()
	defer o.Unlock()

	// Get this client's row index, or create a new row
	if _, ok := o.clients[hwaddr]; !ok {
		o.inst.Extend(1)
		_, rows := o.inst.Size()
		o.clients[hwaddr] = &client{rowIdx: rows - 1}
	}
	row := o.clients[hwaddr].rowIdx

	// Get the attribute's column index. GoLearn doesn't allow adding new
	// attributes to FixedDataGrid
	col, ok := o.attrMap[attr]
	if !ok {
		return
	}

	o.inst.Set(o.spec[col], row, o.spec[col].GetAttribute().GetSysValFromString(val))
}

// PredictBayes predicts identities for the observations using Naive Bayes
func (o *Observations) predictBayes(ch chan Prediction) {
	predictions := o.naiveBayes.Predict(o.naiveFiltered)
	for h, c := range o.clients {
		newID := base.GetClass(predictions, c.rowIdx)
		if newID != c.nbIdentity {
			c.nbIdentity = newID
			ch <- Prediction{Model: "Naive Bayes", HwAddr: h, Identity: newID}
		}
	}
}

// PredictID3 predicts identities for the observations using ID3
func (o *Observations) predictID3(ch chan Prediction) {
	predictions, _ := o.id3Tree.Predict(o.id3Filtered)
	for h, c := range o.clients {
		newID := base.GetClass(predictions, c.rowIdx)
		if newID != c.id3Identity {
			c.id3Identity = newID
			ch <- Prediction{Model: "ID3 Tree", HwAddr: h, Identity: newID}
		}
	}
}

// Predict periodically runs predictions over the entire set of Observations.
// When a client's predicted identity changes a new Prediction is sent on the
// channel returned to the caller.
func (o *Observations) Predict() <-chan Prediction {
	predCh := make(chan Prediction)

	go func(ch chan Prediction) {
		tick := time.NewTicker(time.Duration(time.Minute))
		for {
			<-tick.C
			o.Lock()
			o.predictID3(ch)
			o.predictBayes(ch)
			o.Unlock()
		}
	}(predCh)
	return predCh
}

// GetBayesIdentity returns a prediction for hwaddr.
func (o *Observations) GetBayesIdentity(hwaddr uint64) string {
	o.Lock()
	defer o.Unlock()

	c, ok := o.clients[hwaddr]
	if !ok {
		return "Unknown"
	}

	featAttrSpecs := base.ResolveAttributes(o.naiveFiltered, o.naiveAttrs)

	vector := make([][]byte, len(featAttrSpecs))
	for i := 0; i < len(vector); i++ {
		vector[i] = o.naiveFiltered.Get(featAttrSpecs[i], c.rowIdx)
	}
	return o.naiveBayes.PredictOne(vector)

}

func newEntity() *entity {
	ret := &entity{
		dnsAttr:  make(map[string]bool),
		portAttr: make(map[string]bool),
	}
	return ret
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
func NewBayes(trainData base.FixedDataGrid) *naive.BernoulliNBClassifier {
	naiveBayes := naive.NewBernoulliNBClassifier()
	naiveBayes.Fit(trainData)
	return naiveBayes
}
