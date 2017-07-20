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
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const key = "bd4ea8a80e25662e85f349c84bf300995ef013528c8201455edaeccf7426ec5e"

type DataSource struct {
	FilePath string
	URL      url.URL
	Update   int
	Format   int
	_Data    *[]string
	// XXX Need a lock?
}

// AutoLoader updates the csv file at path with a frequency determined by freq, reloading upon new file.
func (ds *DataSource) AutoLoader(path string, freq time.Duration) {
	ticker := time.NewTicker(freq)
	go func() {
		for {
			if changed, err := ds.update(path); changed && err == nil {
				ds.Loader(path)
			} else if changed {
				log.Printf("Failed to autoload file: %v", err)
			}
			<-ticker.C
		}
	}()
}

// update retrieves the most recent phish data, if data at path either doesn't
// exist or is outdated. Returns whether update needed, and any errors.
func (ds *DataSource) update(path string) (bool, error) {
	// check if needs updating/creating
	fileInfo, err := os.Stat(path)
	if err == nil {
		if time.Now().Sub(fileInfo.ModTime()) < time.Hour {
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return true, fmt.Errorf("failed to open phishtank file: %v", err)
	}

	resp, err := http.Get(fmt.Sprintf("http://data.phishtank.com/data/%s/%s",
		key, path))
	if err != nil {
		return true, fmt.Errorf("failed to update phishtank: %v", err)
	}
	defer resp.Body.Close()

	// save to temp file
	tmpPath := filepath.Dir(path) + "tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return true, fmt.Errorf("failed to create temp file %s: %v",
			tmpPath, err)
	}
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return true, fmt.Errorf("failed to download to temp file %s: %v",
			tmpPath, err)
	}

	// rename temp to real file
	if err := os.Rename(tmpPath, path); err != nil {
		return true, fmt.Errorf("failed while moving %s to %s: %v", tmpPath,
			path, err)
	}

	return true, nil
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
