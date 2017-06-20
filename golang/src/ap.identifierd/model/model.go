/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package model is for training new models (currently only ID3 decision trees
// and Naive Bayes) and making predictions about client identities
package model

import (
	"fmt"
	"log"
	"sync"

	"ap_common/network"

	"github.com/sjwhitworth/golearn/base"
	"github.com/sjwhitworth/golearn/filters"
	"github.com/sjwhitworth/golearn/naive"
	"github.com/sjwhitworth/golearn/trees"
)

type client struct {
	rowIdx      int
	nbIdentity  string
	id3Identity string
}

// Observations contains the observed data for each client
type Observations struct {
	sync.Mutex
	inst *base.DenseInstances
	spec []base.AttributeSpec

	// hwaddr -> client
	clnt map[uint64]client

	// DNS requests only contain the IP addr, so we maintin a map ipaddr -> hwaddr
	ipMap map[uint32]uint64

	// attribute name -> column index
	attrMap map[string]int
}

// GetHWaddr will return the MAC address of the client using the supplied IP address
func (o *Observations) GetHWaddr(ip uint32) (uint64, bool) {
	o.Lock()
	defer o.Unlock()
	hwaddr, ok := o.ipMap[ip]
	return hwaddr, ok
}

// AddIP associates ip to hwaddr
func (o *Observations) AddIP(ip uint32, hwaddr uint64) {
	o.Lock()
	defer o.Unlock()
	o.ipMap[ip] = hwaddr
}

// RemoveIP removes ip and its associated hwaddr
func (o *Observations) RemoveIP(ip uint32) {
	o.Lock()
	defer o.Unlock()
	delete(o.ipMap, ip)
}

// SetByName sets the attribute 'attr' to the value 'val' for the client
// specified by 'hwaddr'.
func (o *Observations) SetByName(hwaddr uint64, attr, val string) {
	o.Lock()
	defer o.Unlock()

	// Get this client's row index, or create a new row
	if _, ok := o.clnt[hwaddr]; !ok {
		o.inst.Extend(1)
		_, rows := o.inst.Size()
		o.clnt[hwaddr] = client{rowIdx: rows - 1}
	}
	row := o.clnt[hwaddr].rowIdx

	// Get the attribute's column index. GoLearn doesn't allow adding new
	// attributes to FixedDataGrid
	col, ok := o.attrMap[attr]
	if !ok {
		return
	}

	o.inst.Set(o.spec[col], row, o.spec[col].GetAttribute().GetSysValFromString(val))
}

// PredictBayes identities for the observations using Naive Bayes
func (o *Observations) PredictBayes(nb *naive.BernoulliNBClassifier) {
	o.Lock()
	defer o.Unlock()

	dataf, err := binarize(o.inst)
	if err != nil {
		log.Println("failed to binarize:", err)
	}

	predictions := nb.Predict(dataf)
	for h, c := range o.clnt {
		c.nbIdentity = base.GetClass(predictions, c.rowIdx)
		log.Printf("New NB identity for %s: %s\n", network.Uint64ToHWAddr(h), c.nbIdentity)
	}
}

// PredictID3 identities for the observations using ID3
func (o *Observations) PredictID3(id3 *trees.ID3DecisionTree) {
	o.Lock()
	defer o.Unlock()

	dataf, err := discretise(o.inst)
	if err != nil {
		log.Println("failed to discretise:", err)
	}

	predictions, _ := id3.Predict(dataf)
	for h, c := range o.clnt {
		c.id3Identity = base.GetClass(predictions, c.rowIdx)
		log.Printf("New ID3 identity for %s: %s\n", network.Uint64ToHWAddr(h), c.id3Identity)
	}
}

// NewObservations creates an empty Observations
func NewObservations() *Observations {
	ret := &Observations{
		inst:    base.NewDenseInstances(),
		spec:    make([]base.AttributeSpec, 0),
		clnt:    make(map[uint64]client),
		ipMap:   make(map[uint32]uint64),
		attrMap: make(map[string]int),
	}
	return ret
}

// LoadTrainingData reads the CSV file at 'path' and returns a new
// DenseInstances for training, while also adding the training data's attributes
// to 'testData'.
func LoadTrainingData(path string, testData *Observations) (*base.DenseInstances, error) {
	trainData, err := base.ParseCSVToInstances(path, true)
	if err != nil {
		return nil, fmt.Errorf("failed to parse training data in %s: %s", path, err)
	}

	// Could call trainData.AllAttributes(), but the resulting slice is ordered
	// by AttributeGroup, making it difficult to add new rows of observed data
	attrs := base.ParseCSVGetAttributes(path, true)

	testData.Lock()
	defer testData.Unlock()
	for i, a := range attrs {
		testData.spec = append(testData.spec, testData.inst.AddAttribute(a))
		testData.attrMap[a.GetName()] = i
	}

	last := len(attrs) - 1
	err = testData.inst.AddClassAttribute(attrs[last])
	if err != nil {
		return nil, fmt.Errorf("failed to add class attribute: %s", err)
	}

	// We need to add one value to the class attribute, which is a
	// CategoricalAttribute, because GetStringFromSysVal() panics if the attribute
	// has no values. Perhaps we should fix this to just print the empty string
	testData.spec[last].GetAttribute().GetSysValFromString("Unknown")

	return trainData, nil
}

func discretise(src base.FixedDataGrid) (base.FixedDataGrid, error) {
	// Discretise the dataset with Chi-Merge
	// XXX Need to understand the magic parameter
	filt := filters.NewChiMergeFilter(src, 0.999)
	for _, a := range base.NonClassFloatAttributes(src) {
		filt.AddAttribute(a)
	}

	if err := filt.Train(); err != nil {
		return nil, fmt.Errorf("could not train Chi-Merge filter: %s", err)
	}

	ret := base.NewLazilyFilteredInstances(src, filt)
	return ret, nil
}

// NewTree returns a new ID3 decision tree trained with trainData
func NewTree(trainData *base.DenseInstances) (*trees.ID3DecisionTree, error) {
	dataf, err := discretise(trainData)
	if err != nil {
		return nil, fmt.Errorf("could not discretise: %s", err)
	}

	// XXX Need to understand the magic parameter which controls train-prune split.
	id3Tree := trees.NewID3DecisionTree(0.4)
	err = id3Tree.Fit(dataf)
	if err != nil {
		return nil, fmt.Errorf("could not train ID3 tree: %s", err)
	}
	return id3Tree, nil
}

func binarize(src base.FixedDataGrid) (base.FixedDataGrid, error) {
	filt := filters.NewBinaryConvertFilter()
	for _, a := range base.NonClassAttributes(src) {
		filt.AddAttribute(a)
	}

	if err := filt.Train(); err != nil {
		return nil, fmt.Errorf("could not train binary filter: %s", err)
	}

	ret := base.NewLazilyFilteredInstances(src, filt)
	return ret, nil
}

// NewBayes returns a new Naive Bayes clasifier trained with trainData
func NewBayes(trainData *base.DenseInstances) (*naive.BernoulliNBClassifier, error) {
	dataf, err := binarize(trainData)
	if err != nil {
		return nil, fmt.Errorf("could not binarize: %s", err)
	}

	naiveBayes := naive.NewBernoulliNBClassifier()
	naiveBayes.Fit(dataf)
	return naiveBayes, nil
}
