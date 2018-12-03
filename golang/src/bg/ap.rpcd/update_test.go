/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"bg/ap_common/aptest"
	"bg/cloud_rpc"

	"google.golang.org/grpc"

	"github.com/stretchr/testify/require"
)

var fileTime = time.Date(2018, time.January, 1, 2, 3, 4, 5, time.UTC)

func mkTimeStampFile(tr *aptest.TestRoot, uType uploadType) string {
	// example: <root>/var/spool/watchd/droplog/2018-07-09T21:52:31+00:00.json
	fname := fileTime.Format(time.RFC3339) + ".json"
	fpath := filepath.Join(tr.Root, uType.dir, fname)
	fdata := []byte("{\"test\": \"data\"}")
	err := ioutil.WriteFile(fpath, fdata, 0755)
	if err != nil {
		panic(err)
	}
	fileTime = fileTime.Add(time.Minute)
	return fpath
}

// Usually we use testify's mock, but we need something a little more complex here.
//
// Note that the struct's methods can't use a pointer receiver-- so each
// invocation is a copy of mockRPC.
type mockRPC struct {
	t       *testing.T
	baseURL string
	calls   *int // * because of non-pointer receivers
}

func (mock mockRPC) GenerateURL(ctx context.Context, rq *cloud_rpc.GenerateURLRequest, g ...grpc.CallOption) (*cloud_rpc.GenerateURLResponse, error) {
	mock.t.Logf("request: %v", rq)
	urls := make([]*cloud_rpc.SignedURL, 0)
	for idx, object := range rq.Objects {
		url := fmt.Sprintf("%s/%s/%d", mock.baseURL, rq.Prefix, idx)
		urls = append(urls, &cloud_rpc.SignedURL{
			Object: object,
			Url:    url,
		})
	}
	response := &cloud_rpc.GenerateURLResponse{Urls: urls}
	mock.t.Logf("response: %v", response)
	*mock.calls++
	return response, nil
}

func prefix2uType(prefix string) uploadType {
	for _, t := range uploadTypes {
		if prefix == t.prefix {
			return t
		}
	}
	panic(fmt.Errorf("bad prefix %s", prefix))
}

func TestUpload(t *testing.T) {
	*uploadErrMax = 3
	*uploadBatchSize = 5

	statsUInfo := prefix2uType("stats")
	dropUInfo := prefix2uType("drops")

	testCases := []struct {
		name       string
		uType      uploadType
		nFiles     int // Create 'n' files to upload
		nErrors    int // Cause 'n' errors to occur
		nRemaining int // Expect 'n' files to remain
		nCalls     int // Expect 'n' RPC calls
		nPUTs      int // Expect 'n' PUTs
	}{
		{"noFiles", statsUInfo, 0, 0, 0, 0, 0},
		{"stats_1", statsUInfo, 1, 0, 0, 1, 1},
		{"stats_batchplus1", statsUInfo, *uploadBatchSize + 1, 0, 1, 1, *uploadBatchSize},
		{"drop_1", dropUInfo, 1, 0, 0, 1, 1},
		{"drop_2errors", dropUInfo, 3, 2, 2, 1, 3},
		// Give up when the error threshold is reached
		{"bailout", dropUInfo, *uploadBatchSize, *uploadErrMax, *uploadBatchSize, 1, *uploadErrMax},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var nPUTs int
			var nCalls int
			var nErrors int
			var files = make([]string, tc.nFiles)
			assert := require.New(t)
			tr := aptest.NewTestRoot(t)
			defer tr.Fini()

			for i := 0; i < tc.nFiles; i++ {
				files = append(files, mkTimeStampFile(tr, tc.uType))
			}

			// Make a local HTTP server to respond to the PUT requests
			tSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nPUTs++
				if nErrors < tc.nErrors {
					nErrors++
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				t.Logf("request: %s %s", r.Method, r.URL)
				assert.Equal("PUT", r.Method)
				assert.Regexp(regexp.MustCompile("^/"+tc.uType.prefix+"/\\d+$"), r.URL.Path)
				assert.Equal(tc.uType.ctype, r.Header.Get("Content-Type"))
				fmt.Fprintln(w, "ok")
			}))
			defer tSrv.Close()

			// Setup Mock RPC client and response
			csMock := mockRPC{
				t:       t,
				baseURL: tSrv.URL,
				calls:   &nCalls,
			}

			// Run the upload logic
			doUpload(csMock)

			nRemaining := 0
			for _, f := range files {
				if _, err := os.Stat(f); err == nil {
					nRemaining++
				}
			}
			assert.Equal(tc.nRemaining, nRemaining, "unexpected number of unsent files")
			assert.Equal(tc.nCalls, nCalls, "unexpected number of rpc calls")
			assert.Equal(tc.nPUTs, nPUTs, "unexpected number of PUTs")
		})
	}
}
