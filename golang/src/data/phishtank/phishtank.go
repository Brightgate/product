package phishtank

/*
 * data/phishtank is a data source library.
 *
 * Metadata about this data source:
 * - http://data.phishtank.com/....
 * - Updated Hourly
 * - [Complete on one retrieval, or needs to be accumulated?]
 * - CSV
 *
 * KnownToDataSource(s string) (bool)
 * Get(s string) ([]record)
 * Update()
 *
 https://pixabay.com/en/fish-market-seafood-fish-428058/
 https://pixabay.com/p-1191938/
*/

import (
	"encoding/csv"
	"io"
	"log"
	"net/url"
	"os"
	"sort"
)

type DataSource struct {
	FilePath string
	URL      url.URL
	Update   int
	Format   int
	_Data    *[]string
	// XXX Need a lock?
}

func (ds *DataSource) Loader(path string) {
	ds._Data = new([]string)

	f, err := os.Open(path)
	if err != nil {
		log.Printf("couldn't open '%s': %s", path, err)
		return
	}

	r := csv.NewReader(f)

	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		u, err := url.Parse(record[1])
		if err == nil {
			*ds._Data = append(*ds._Data, u.Host)
		}
	}

	sort.Strings(*ds._Data)
	ds.FilePath = path
}

func (ds *DataSource) KnownToDataSource(s string) bool {
	i := sort.SearchStrings(*ds._Data, s)

	if i < len(*ds._Data) && (*ds._Data)[i] == s {
		log.Printf("found: %s", s)
		return true
	}

	return false
}
