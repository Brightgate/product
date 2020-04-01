/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package classifier

import (
	"net"
	"strings"
	"testing"

	"github.com/klauspost/oui"
	"github.com/stretchr/testify/require"
)

func TestClassifyResult(t *testing.T) {
	assert := require.New(t)

	r1 := ClassifyResult{
		ModelName:      "test-model",
		Classification: "orange",
		Probability:    0.980001,
		NextProb:       0.10,
		Region:         ClassifyCertain,
	}
	assert.True(r1.Equal(r1))
	assert.Equal("test-model:orange [0.98]", r1.String())

	r2 := r1
	r2.Probability = r1.Probability - 0.000001
	assert.True(r2.Equal(r2))

	r3 := ClassifyResult{
		ModelName:      "test-model",
		Classification: "grapefruit",
		Probability:    1.0,
		NextProb:       0.10,
		Region:         ClassifyCertain,
	}
	assert.True(r3.Equal(r3))

	r4 := r3
	r4.Probability = r3.Probability - 0.20
	assert.True(r4.Equal(r4))

	assert.True(r1.Equal(r2))
	assert.False(r2.Equal(r3))
	assert.False(r3.Equal(r4))
}

func TestPosterior(t *testing.T) {
	assert := require.New(t)

	// Taken from some real results
	p1 := map[string]float64{
		"Android":    0.6687898089171975,
		"Linux":      0.00016480799868153598,
		"OpenWrt":    6.005645306588192e-05,
		"Windows 10": 3.177865110218958e-05,
		"iOS":        0.0004888913031669297,
		"macOS":      0.00013391063696826326,
	}

	r1 := newClassifyResultFromPosterior("test-model", 0.5, 0.4, p1)
	assert.Equal("Android", r1.Classification)
	assert.Equal(ClassifyCertain, r1.Region)
	assert.Equal(p1["Android"], r1.Probability)
	assert.Equal(p1["iOS"], r1.NextProb)
	assert.Equal("test-model", r1.ModelName)

	r1 = newClassifyResultFromPosterior("test-model", 0.9, 0.5, p1)
	assert.Equal("Android", r1.Classification)
	assert.Equal(ClassifyCrossing, r1.Region)
	assert.Equal(p1["Android"], r1.Probability)
	assert.Equal(p1["iOS"], r1.NextProb)
	assert.Equal("test-model", r1.ModelName)

	r1 = newClassifyResultFromPosterior("test-model", 0.9, 0.8, p1)
	assert.Equal("Android", r1.Classification)
	assert.Equal(ClassifyUncertain, r1.Region)
	assert.Equal(p1["Android"], r1.Probability)
	assert.Equal(p1["iOS"], r1.NextProb)
	assert.Equal("test-model", r1.ModelName)

	// Edge case
	p2 := map[string]float64{
		"Android": 0.0,
		"Linux":   0.0,
		"OpenWrt": 0.0,
	}
	r1 = newClassifyResultFromPosterior("test-model", 0.9, 0.8, p2)
	// We can't really assert the classification since they are all the same
	assert.Equal(ClassifyUncertain, r1.Region)
	assert.Equal(0.0, r1.Probability)
	assert.Equal(0.0, r1.NextProb)
	assert.Equal("test-model", r1.ModelName)
}

const mockOUI = `
OUI/MA-L			Organization
company_id			Organization
				Address

58-CB-52   (hex)		Google Inc.
58CB52     (base 16)		Google Inc.
				1600 Amphitheatre Parkway
				Mountain View CA 94043
				US

`

func TestMfgLookup(t *testing.T) {
	assert := require.New(t)

	rdr := strings.NewReader(mockOUI)
	ouiDB, err := oui.OpenStatic(rdr)
	assert.NoError(err)

	tests := map[string]string{
		"58:cb:52:12:34:56": "Google Inc.",
		"00:00:00:00:00:00": "-unknown-mfg-",
		// Alpha/Beta special
		"60:90:84:a1:22:33": "Brightgate, Inc.",
	}

	cl := NewMfgLookupClassifier(ouiDB)
	for mac, exp := range tests {
		hwaddr, err := net.ParseMAC(mac)
		assert.NoError(err)
		result := cl.Classify(hwaddr)
		assert.Equal(exp, result.Classification)
	}
}
