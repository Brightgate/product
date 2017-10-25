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
	"log"
	"os"

	"github.com/google/safebrowsing"
)

/*
Example of use:

sb := SafeBrowser("my-database").Scorer()
defer sb.Close()
if sb.Score("www.badsite.com") < 0 {
    fmt.Println("Potential phishing site")
}

*/

// SBSource contains information about a SafeBrowser. Implements Weight and Load
type SBSource struct {
	BasicSource
}

// SBScorer is a scorer using Google's Safe Browser
type SBScorer struct {
	SBSource
	sb *safebrowsing.SafeBrowser
}

// SafeBrowser returns a source for loading the safe browsing database stored
// at the given path.
func SafeBrowser(path string) SBSource {
	return SBSource{
		BasicSource{
			name:          "safebrowsing",
			filepath:      path,
			weightMap:     map[Kind]int{},
			defaultWeight: -20,
		},
	}
}

// Scorer returns a new google-provided SafeBrowser. Is automaticaly
// updated every half hour (see source code for details).
//
// the safe browser also has its own close method that should be called to clean up.
//
func (s SBSource) Scorer() *SBScorer {
	config := safebrowsing.Config{
		APIKey: apiKey,
		DBPath: s.filepath,
		Logger: os.Stdout,
	}
	sb, err := safebrowsing.NewSafeBrowser(config)
	if err != nil {
		log.Println("Problem making new Safe Browser")
		// error out?
	}
	return &SBScorer{
		s,
		sb,
	}
}

func (s *SBScorer) Close() {
	if err := s.sb.Close(); err != nil {
		log.Println("Error closing safe browser: %v", err)
	}
}

var apiKey = "AIzaSyCg0CmPXzTWpQcPfYkX15jU8sSb__pLwCEAIzaSyCg0CmPXzTWpQcPfYkX15jU8sSb__pLwCE"

func (sb *SBScorer) Contains(s string) bool {
	threats, err := sb.sb.LookupURLs([]string{s})
	if err != nil {
		log.Printf("Problem looking up %s", s)
		return true
	}
	if len(threats[0]) > 0 {
		log.Printf("%s might be a threat!", s)
	}
	return len(threats[0]) > 0 // more specific information is also provided
}

func (sb *SBScorer) Score(s string, kind Kind) int {
	if sb.Contains(s) {
		log.Printf("%s might be a threat!", s)
		return sb.Weight(kind)
	}
	return 0
}
