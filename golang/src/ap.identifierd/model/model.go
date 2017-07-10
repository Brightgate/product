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
	"log"
	"os"
	"sync"

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
	clnt map[uint64]client

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

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
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

// PredictBayes predicts identities for the observations using Naive Bayes
func (o *Observations) PredictBayes(nb *naive.BernoulliNBClassifier) {
	o.Lock()
	defer o.Unlock()

	dataf, err := Binarize(o.inst)
	if err != nil {
		log.Println("failed to binarize:", err)
	}

	predictions := nb.Predict(dataf)
	for h, c := range o.clnt {
		c.nbIdentity = base.GetClass(predictions, c.rowIdx)
		log.Printf("New NB identity for %s: %s\n", network.Uint64ToHWAddr(h), c.nbIdentity)
	}
}

// PredictID3 predicts identities for the observations using ID3
func (o *Observations) PredictID3(id3 *trees.ID3DecisionTree) {
	o.Lock()
	defer o.Unlock()

	dataf, err := Discretise(o.inst)
	if err != nil {
		log.Println("failed to discretise:", err)
	}

	predictions, _ := id3.Predict(dataf)
	for h, c := range o.clnt {
		c.id3Identity = base.GetClass(predictions, c.rowIdx)
		log.Printf("New ID3 identity for %s: %s\n", network.Uint64ToHWAddr(h), c.id3Identity)
	}
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
		clnt:    make(map[uint64]client),
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

// Discretise transforms attributes into CategoricalAttributes
func Discretise(src base.FixedDataGrid) (base.FixedDataGrid, error) {
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
func NewTree(trainData base.FixedDataGrid) (*trees.ID3DecisionTree, error) {
	dataf, err := Discretise(trainData)
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

// Binarize transforms attributes into BinaryAttributes
func Binarize(src base.FixedDataGrid) (base.FixedDataGrid, error) {
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
func NewBayes(trainData base.FixedDataGrid) (*naive.BernoulliNBClassifier, error) {
	dataf, err := Binarize(trainData)
	if err != nil {
		return nil, fmt.Errorf("could not binarize: %s", err)
	}

	naiveBayes := naive.NewBernoulliNBClassifier()
	naiveBayes.Fit(dataf)
	return naiveBayes, nil
}
