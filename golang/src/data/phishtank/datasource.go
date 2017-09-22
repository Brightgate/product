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

// Kind represents the type of data extracted/used
type Kind int

const (
	Dns Kind = iota
	Url
	Ipv4
	Ipv6
)

// Source is a type that can be used to produce a Scorer. It holds all the
// static information needed to initialize a Scorer from a datasource.
type Source interface {
	// Weight is used to determine how trustworthy a url/domain/etc is that
	// is contained within this datasource. Negative numbers are not
	// trustworthy, positive are.
	Weight(kind Kind) int
	Scorer() Scorer
}

// BasicSource implements the Weight method using a default weight and a map
// of kinds of extraction to weight. Scorer is not implemented
type BasicSource struct {
	name          string
	weightMap     map[Kind]int // weights for each kind of extraction
	defaultWeight int
	filepath      string
}

func (s BasicSource) Weight(kind Kind) int {
	if w, ok := s.weightMap[kind]; ok {
		return w
	}
	return s.defaultWeight
}

// Scorer is used to judge the trustworthiness of domains, urls, ips, etc.
type Scorer interface {
	Score(s string, k Kind) int
	Close()
}

// The basic scorer type would contain a pointer to its source and have the
// score method, returning the weight if it contains the string.
// See safebrowsing.

// MultiScorer combines results from multiple scorers
//
// Example use of MultiScorer:
//
// csvScorer := NewReader(
// 	Phishtank("phishtank.csv"),
// 	Whitelist("whitelist.csv"),
// 	MDL("mdl.csv")).Scorer())
// scorer := NewMultiScorer(
// 	csvScorer,
// 	SafeBrowsing("safebrowsing").Scorer())
// defer scorer.Close()
// if scorer.Score("1.2.3.4", Ipv4) < 0 {
//     fmt.Println("Unsafe IP detected")
// }
type MultiScorer struct {
	scorers []Scorer
}

func (m *MultiScorer) Score(s string, k Kind) int {
	var score int
	for _, scorer := range m.scorers {
		score += scorer.Score(s, k)
	}
	return score
}

func (m *MultiScorer) Close() {
	for _, scorer := range m.scorers {
		scorer.Close()
	}
}

// NewMultiScorer produces a MultiScorer from multiple scorers
func NewMultiScorer(scorers ...Scorer) *MultiScorer {
	return &MultiScorer{
		scorers,
	}
}
