/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package sentence

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

// A Sentence is implemented using a map so that we can easily compute
// whether or not two sentences have similar (same terms, but possibly
// different frequencies) or identical information content (terms and
// frequencies identical).
type Sentence map[string]int

// New builds a new empty sentence.
func New() Sentence {
	return make(Sentence)
}

// NewFromString builds a new sentence from the supplied string.
func NewFromString(sent string) Sentence {
	s := New()

	lt := strings.Fields(sent)

	for _, v := range lt {
		s.AddTerm(v)
	}

	return s
}

// AddTerm adds the given term to the sentence. It returns true when the added
// term is redundant (it was already present in the sentence).  If term is new,
// then false is returned.
func (s Sentence) AddTerm(term string) bool {
	s[term] = s[term] + 1

	return s[term] > 1
}

// AddTermf adds the given term to the sentence using Sprintf style formatting.
// It returns true when the added term is redundant (it was already present in
// the sentence).  If term is new, then false is returned.
func (s Sentence) AddTermf(format string, a ...interface{}) bool {
	return s.AddString(fmt.Sprintf(format, a...))
}

// AddString splits the supplied string and adds each term to the Sentence.
// It returns true when all added terms are redundant.  It returns false
// when at least one term is exclusive to sent.
func (s Sentence) AddString(sent string) bool {
	redundant := true

	for _, v := range strings.Fields(sent) {
		redundant = s.AddTerm(v) && redundant
	}

	return redundant
}

// AddSentence merges s2 with the sentence.   It returns true when all added
// terms are redundant.  It returns false when at least one term is exclusive
// to s2.
func (s Sentence) AddSentence(s2 Sentence) bool {
	redundant := true

	for k, v := range s2 {
		s[k] += v
		if s[k] == v {
			// Meaning that this word was exclusive to the added
			// sentence.
			redundant = false
		}
	}

	return redundant
}

// SubtractSentence removes the contents of s2 from the sentence s1.
func (s Sentence) SubtractSentence(s2 Sentence) bool {
	redundant := true

	for k, v := range s2 {
		if s[k] == 0 {
			// Term not in s
			continue
		}
		s[k] -= v
		if s[k] == 0 {
			// s no longer contains this term
			delete(s, k)
			redundant = false
		}
	}

	return redundant
}

// Terms returns the slice of strings that make up the sentence terms.
func (s Sentence) Terms() []string {
	t := make([]string, len(s))

	i := 0
	for k := range s {
		t[i] = k
		i++
	}
	return t
}

// TermCount returns the number of unique terms in the sentence.
// (so NewFromString("banana carrot carrot").TermCount() == 2)
func (s Sentence) TermCount() int {
	return len(s)
}

// TermHash produces a hashed value for the sentence's unique terms
// NewFromString("a b c").TermHash() == NewFromString("c c b b a a").TermHash()
func (s Sentence) TermHash() uint64 {
	h := fnv.New64()

	ws := s.Terms()

	sort.Strings(ws)

	for _, k := range ws {
		_, _ = h.Write([]byte(k))
	}

	return h.Sum64()
}

// WordCount returns the number of total accumulated terms
// NewFromString("banana carrot carrot").WordCount() == 3
func (s Sentence) WordCount() int {
	n := 0

	for _, v := range s {
		n += v
	}

	return n
}

// WordHash produces a hashed value for the sentence's total
// population of words.
// NewFromString("a b c").WordHash() != NewFromString("a a b b c c").WordHash()
func (s Sentence) WordHash() uint64 {
	h := fnv.New64()

	ws := s.Terms()

	sort.Strings(ws)

	for _, k := range ws {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte(strconv.Itoa(s[k])))
	}

	return h.Sum64()
}

// String converts the sentence to a sorted string with no redundant terms.
func (s Sentence) String() string {
	ts := s.Terms()

	sort.Strings(ts)

	return strings.Join(ts, " ")
}

// NaryString converts the sentence to a sorted string, with redundant terms
// restated once for each occurence.  (banana carrot carrot carrot)
func (s Sentence) NaryString() string {
	t := make([]string, 0)

	ts := s.Terms()
	sort.Strings(ts)

	for _, word := range ts {
		for u := 0; u < s[word]; u++ {
			t = append(t, word)
		}
	}
	return strings.Join(t, " ")
}

