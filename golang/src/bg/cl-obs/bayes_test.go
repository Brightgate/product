//
// COPYRIGHT 2019 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or  alteration will be a violation of federal law.
//

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentenceBasics(t *testing.T) {
	r := newSentence()

	assert.Equal(t, 0, r.termCount(), "empty sentence should have zero termCount")

	r.addTerm("potato")
	r.addTerm("carrot")
	r.addTerm("avocado")
	r.addTerm("turnip")

	assert.Equal(t, 4, r.termCount(), "sentence should have termCount matching distinct terms added")

	s := newSentenceFromString("potato carrot avocado turnip")
	assert.Equal(t, 4, s.termCount(), "sentence should have termCount matching distinct terms added")

	assert.Equal(t, r.toString(), s.toString(), "strings should be equal")

	assert.Equal(t, s.termHash(), r.termHash(), "term hashes should be equal")

	assert.Equal(t, s.wordHash(), r.wordHash(), "word hashes should be equal")

	added := r.addTermf("%s %s", "mango", "radish")

	assert.Equal(t, false, added, "sentence should have accepted new terms")
	assert.Equal(t, 6, r.termCount(), "sentence should have termCount matching distinct new terms added")
}

func TestAddSentence(t *testing.T) {
	q := newSentenceFromString("potato carrot")
	r := newSentenceFromString("avocado turnip")

	added := r.addSentence(q)

	assert.Equal(t, 4, r.termCount(), "sentence should have termCount matching sum of distinct terms in sentences")
	assert.Equal(t, false, added, "sentence addition has new content")

	s := newSentenceFromString("carrot turnip banana")

	added = r.addSentence(s)

	assert.Equal(t, 5, r.termCount(), "sentence should have termCount matching sum of distinct terms in sentences")
	assert.Equal(t, false, added, "sentence addition has new content")
}

func TestAddRedundantSentence(t *testing.T) {
	q := newSentenceFromString("potato carrot")
	r := newSentenceFromString("avocado turnip")

	r.addSentence(q)

	s := newSentenceFromString("carrot turnip")

	added := r.addSentence(s)

	assert.Equal(t, 4, r.termCount(), "sentence should have termCount matching sum of distinct terms in sentences")
	assert.Equal(t, true, added, "sentence addition has only redundant content")
}
