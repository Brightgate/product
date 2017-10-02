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

package phishtank

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
)

/*

Examples of use:

reader := NewReader(
	Phishtank("online-valid.csv"),
	Whitelist("whitelist.csv"),
	MDL("domainlist.csv"))
s := reader.Read(Dns, Url)
if s.Score("www.badsite.com", Dns) < 0 {
    fmt.Println("this site looks phishy!")
}

*/

// CSVSource is an interface that allows both local CSV files and remote CSV
// files to be read into a MasterExtraction.
type CSVSource interface {
	ReadTo(dest *MasterExtraction, kinds []Kind)
}

// CSVSource contains information about how to parse a CSV datasource.
// Implements Source.
type BasicCSVSource struct {
	BasicSource
	// contains the indices of the columns relevant to each extraction type
	columnMap map[Kind][]int
}

func (c BasicCSVSource) Scorer() Scorer {
	return NewReader(c).Scorer()
}

// ReadTo reads any data from the source of the relevant kinds to dest
func (s BasicCSVSource) ReadTo(dest *MasterExtraction, kinds []Kind) {
	log.Printf("Reading from %s", s.name)
	f, err := os.Open(s.filepath)
	if err != nil {
		log.Printf("Couldn't open '%s': %v", s.filepath, err)
		return
	}
	defer f.Close()

	r := csv.NewReader(f)
	linesRead := 0
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error parsing csv record %d: %v",
				linesRead, err)
			break
		}
		for _, kind := range kinds { // for each relevant kind
			for _, col := range s.columnMap[kind] {
				s.Extract(record[col], kind, dest)
			}
		}
		linesRead++
	}
	log.Printf("Read %d lines from %s\n", linesRead, s.name)
}

// Extract parses str as type kind and puts the extracted information into dest
func (s BasicCSVSource) Extract(str string, kind Kind, dest *MasterExtraction) {
	var toStore string
	u := toUrl(str)
	if u == nil {
		return
	}

	switch kind {
	case Dns:
		toStore = strings.ToLower(u.Host)
	case Url:
		toStore = u.String()
	case Ipv4:
		ip := net.ParseIP(u.Host)
		if ip != nil {
			// check if v4
			if ip4 := ip.To4(); ip4 != nil {
				toStore = ip4.String()
			}
		}
	case Ipv6:
		ip := net.ParseIP(u.Host)
		if ip != nil {
			// check if v6
			if ip6 := ip.To16(); ip6 != nil {
				toStore = ip6.String()
			}
		}
	}
	if toStore != "" {
		ext := dest.Extraction(kind)
		ext.Lock()
		defer ext.Unlock()
		ext._Data[toStore] += s.Weight(kind)
	}
}

// Whitelist returns the CSVSource representing a basic whitelist, containing
// whitelisted domains.
func Whitelist(path string) BasicCSVSource {
	return BasicCSVSource{
		BasicSource{
			name:          "whitelist",
			filepath:      path,
			defaultWeight: 100,
			weightMap:     map[Kind]int{},
		},
		map[Kind][]int{
			Dns: []int{0},
		},
	}
}

// Generic allows for parsing a file at path, with a weight function always
// returning weight, and the relevant information for all kinds stored in col.
// Primarily used for testing and easy customization.
func Generic(path string, weight int, col int) BasicCSVSource {
	return BasicCSVSource{
		BasicSource{
			name:          "testing",
			filepath:      path,
			defaultWeight: weight,
			weightMap:     map[Kind]int{},
		},
		map[Kind][]int{
			Dns:  []int{col},
			Url:  []int{col},
			Ipv4: []int{col},
			Ipv6: []int{col},
		},
	}
}

// Extraction is the parsed version of CSV data of a particular type.
type Extraction struct {
	sync.RWMutex // XXX maybe one a mutex for data and one for sources
	sources      []Source
	_Data        map[string]int
}

// Score returns the weight for s.
func (e Extraction) Score(s string) int {
	e.RLock()
	defer e.RUnlock()
	return e._Data[s]
}

// MasterExtraction stores information about a CSV file by type. Can be used to
// store data from multiple sources. Implements Scorer.
type MasterExtraction struct {
	sync.RWMutex
	// Perhaps also include kinds []Kind, so that updates can know which types
	// of data are meant to be stored
	sources []CSVSource
	dns     *Extraction
	url     *Extraction
	ipv4    *Extraction
	ipv6    *Extraction
}

// Extraction, when given a kind, returns the extraction of that kind
func (m *MasterExtraction) Extraction(kind Kind) *Extraction {
	switch kind {
	case Dns:
		return m.dns
	case Url:
		return m.url
	case Ipv4:
		return m.ipv4
	case Ipv6:
		return m.ipv6
	default:
		log.Println("Invalid extraction type") // should never happen
		return nil
	}
}

func (m *MasterExtraction) Close() {}

// Score gives a score for a certain string.
func (m *MasterExtraction) Score(s string, kind Kind) int {
	log.Printf("final score for %s: %d", s, m.Extraction(kind).Score(s))
	return m.Extraction(kind).Score(s)
}

// Export writes items of the given kind contained in a MasterExtraction
// to the given path
func (m *MasterExtraction) Export(path string, kinds ...Kind) {
	file, err := os.OpenFile(path,
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		log.Printf("Error opening file: %v\n", err)
	}
	defer file.Close()
	// default to load all
	if len(kinds) == 0 {
		kinds = []Kind{Dns, Url, Ipv4, Ipv6}
	}
	for _, kind := range kinds {
		toSearch := m.Extraction(kind)
		toSearch.RLock()
		for u, w := range toSearch._Data {
			if w < 0 {
				file.WriteString(fmt.Sprintf("%s,%d\n", u, w))
			}
		}
		toSearch.RUnlock()
	}
}

// Reader processes a collection of sources using the given extractors,
// outputting the results to a *MasterExtraction
// As it is now, Reader can take in and read RemoteSources, but they are not updated.
type Reader struct {
	Sources []CSVSource
}

// Scorer extracts data of the given kinds from the given sources to a MasterExtraction
func (r Reader) Scorer(kinds ...Kind) *MasterExtraction {
	// XXX Is there a cleaner way to initialize this nested struct?
	log.Println("Creating scorer")
	newExt := func() *Extraction {
		return &Extraction{
			_Data: make(map[string]int),
		}
	}
	dest := &MasterExtraction{
		sync.RWMutex{},
		r.Sources,
		newExt(),
		newExt(),
		newExt(),
		newExt(),
	}
	// default to load all
	if len(kinds) == 0 {
		kinds = []Kind{Dns, Url, Ipv4, Ipv6}
	}
	for _, src := range r.Sources {
		go src.ReadTo(dest, kinds)
	}
	return dest
}

func NewReader(sources ...CSVSource) Reader {
	return Reader{
		Sources: sources,
	}
}

// toUrl returns the URl form of a string by first trying to parse the URL as
// is, then parsing after prepending a default schema.
func toUrl(str string) *url.URL {
	u, err := url.Parse(str)
	if err == nil && u.Host != "" {
		return u
	}
	// might have no schema
	u, err = url.Parse("http://" + str)
	if err == nil && u.Host != "-" {
		return u
	}
	// url not valid
	return nil
}
