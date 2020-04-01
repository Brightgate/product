/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package sentence

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSentenceBasics(t *testing.T) {
	assert := require.New(t)

	r := New()
	assert.Equal(0, r.TermCount(), "empty sentence should have zero TermCount")
	assert.Equal(0, r.WordCount(), "empty sentence should have zero WordCount")
	assert.Equal("", r.String())
	assert.Equal("", r.NaryString())

	s := NewFromString("avocado potato turnip carrot")
	assert.Equal(4, s.TermCount())
	assert.Equal(4, s.WordCount())
	assert.Equal("avocado carrot potato turnip", s.String())
	assert.Equal("avocado carrot potato turnip", s.NaryString())
}

func TestAddTerm(t *testing.T) {
	assert := require.New(t)

	r := New()
	assert.False(r.AddTerm("potato"))
	assert.False(r.AddTerm("carrot"))
	assert.False(r.AddTermf("%s %s", "avocado", "turnip"))

	assert.Equal("avocado carrot potato turnip", r.String())
	assert.Equal("avocado carrot potato turnip", r.NaryString())
	assert.Equal(4, r.TermCount())
	assert.Equal(4, r.WordCount())

	// Same contents
	s := NewFromString("avocado potato turnip carrot")
	assert.Equal(r.String(), s.String(), "strings should be equal")
	assert.Equal(r.TermHash(), s.TermHash(), "TermHash should be equal")
	assert.Equal(r.WordHash(), s.WordHash(), "WordHash should be equal")

	assert.True(r.AddTerm("potato"))
	assert.Equal(4, r.TermCount())
	assert.Equal(5, r.WordCount())

	assert.False(r.AddTermf("%s %s", "mango", "radish"))
	assert.Equal(6, r.TermCount(), "sentence should have termCount matching distinct new terms added")
}

func TestAddString(t *testing.T) {
	assert := require.New(t)
	r := NewFromString("avocado potato turnip carrot")

	assert.False(r.AddString("tomato potato"))
	assert.True(r.AddString("tomato potato"))
	assert.Equal(5, r.TermCount())
	assert.Equal(8, r.WordCount())
}

func TestAddSentence(t *testing.T) {
	assert := require.New(t)

	q := NewFromString("potato carrot")
	r := NewFromString("avocado turnip")

	added := r.AddSentence(q)

	assert.Equal(4, r.TermCount(), "sentence should have termCount matching sum of distinct terms in sentences")
	assert.False(added, "sentence addition has new content")

	s := NewFromString("carrot turnip banana")

	added = r.AddSentence(s)
	assert.False(added, "sentence addition has new content")
	assert.Equal(5, r.TermCount(), "sentence should have TermCount matching sum of distinct terms in sentences")

	assert.Equal(7, r.WordCount(), "sentence should have WordCount matching sum of all terms in sentences")
	assert.Equal("avocado banana carrot carrot potato turnip turnip", r.NaryString())
}

func TestAddRedundantSentence(t *testing.T) {
	assert := require.New(t)
	q := NewFromString("potato carrot")
	r := NewFromString("avocado turnip")

	r.AddSentence(q)

	added := r.AddSentence(NewFromString("carrot turnip"))

	assert.Equal(4, r.TermCount(), "sentence should have termCount matching sum of distinct terms in sentences")
	assert.True(added, "sentence addition has only redundant content")
}

func TestSubtractSentence(t *testing.T) {
	assert := require.New(t)

	q := NewFromString("potato carrot")
	r := NewFromString("avocado turnip")

	redundant := q.AddSentence(r)
	assert.False(redundant)
	assert.Equal(4, q.TermCount())
	assert.Equal(4, q.WordCount())

	redundant = q.AddSentence(r)
	assert.True(redundant)
	assert.Equal(4, q.TermCount())
	assert.Equal(6, q.WordCount())

	redundant = q.SubtractSentence(r)
	assert.True(redundant)
	assert.Equal(4, q.TermCount())
	assert.Equal(4, q.WordCount())

	redundant = q.SubtractSentence(r)
	assert.False(redundant)
	assert.Equal(2, q.TermCount())
	assert.Equal(2, q.WordCount())

	// Now r is not "in" q, so this is redundant
	redundant = q.SubtractSentence(r)
	assert.True(redundant)
	assert.Equal(2, q.TermCount())
	assert.Equal(2, q.WordCount())

	// Test subbing from yourself
	redundant = q.SubtractSentence(q)
	assert.False(redundant)
	assert.Equal(0, q.TermCount())

	// Test subbing from empty sentence
	q = New()
	redundant = q.SubtractSentence(r)
	assert.True(redundant)
	assert.Equal(0, q.TermCount())
}
