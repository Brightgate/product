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

/*
 * This tool is used to merge related DNS names in the identifier csv.  For
 * example, we would like node-1.push.apple.com, node-2.push.apple.com, etc, all
 * to be processed as 'push.apple.com'.  This allows us to react to DNS names
 * that we expect to see, without needing individual characteristics for each of
 * 100+ different possibilities.
 *
 * for this specific case, the invocation would be:
 *     ./model-merge push.apple.com. ap_indentities.csv new_ap_identities.csv
 */

package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Printf("Usage: %s <attribute> <in_file> <out_file>\n", os.Args[0])
		os.Exit(1)
	}
	substr := os.Args[1]
	infile := os.Args[2]
	outfile := os.Args[3]

	source, err := ioutil.ReadFile(infile)
	if err != nil {
		fmt.Printf("failed to load %s: %v\n", infile, err)
		os.Exit(1)
	}

	r := csv.NewReader(strings.NewReader(string(source)))
	original, err := r.ReadAll()
	if err != nil {
		fmt.Printf("failed to parse ID file %s: %v\n", infile, err)
		os.Exit(1)
	}
	attributes := len(original[0]) - 1

	out := make([][]string, len(original))
	merged := make([]string, len(original))
	identity := make([]string, len(original))
	for r := range original {
		merged[r] = "0"
		identity[r] = original[r][attributes]
	}
	merged[0] = substr

	mergedColumns := 0
	userInput := bufio.NewReader(os.Stdin)
	for c, f := range original[0][:attributes] {
		merge := false
		if strings.HasSuffix(f, substr) {
			for {
				fmt.Printf("Merge %s into %s (y/n)? ", f, substr)
				resp, _ := userInput.ReadString('\n')
				if resp == "n\n" || resp == "N\n" {
					break
				}
				if resp == "y\n" || resp == "Y\n" {
					merge = true
					mergedColumns++
					break
				}
			}
		}

		for r := range original {
			if merge {
				if original[r][c] == "1" {
					merged[r] = "1"
				}
			} else {
				out[r] = append(out[r], original[r][c])
			}
		}
	}
	if mergedColumns == 0 {
		fmt.Printf("No merged columns.  Dataset unchanged.\n")
		os.Exit(0)
	}

	// The new merged attribute goes at the end, followed by the identity
	for r, m := range merged {
		out[r] = append(out[r], m)
		out[r] = append(out[r], identity[r])
	}

	dest, err := os.Create(outfile)
	if err != nil {
		fmt.Printf("Unable to create %s: %v\n", outfile, err)
		os.Exit(1)
	}
	defer dest.Close()

	w := csv.NewWriter(dest)
	w.WriteAll(out)
	w.Flush()
}
