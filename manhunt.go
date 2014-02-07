package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	GZIP_EXTENSION = ".gz"
	NCPUS          = 2
)

// TODO: Work this out automatically, if possible.
// Paths taken from /etc/{manpath.config,man_db.conf}
var MANPATH = [...]string{
	// MANDATORY_MANPATH
	"/usr/local/share/man",
	"/usr/share/man",
	"/usr/man",
	// Regular manpaths.
	"/usr/local/man",
	"/usr/X11R6/man",
	"/opt/man",
}

// Prints items arriving on matchChan
func printMatch(matchChan <-chan string) {
	for match := range matchChan {
		basename := path.Base(match)
		manInfo := strings.Split(basename, ".")
		command := manInfo[0]
		section := manInfo[1]
		fmt.Printf("%s (%s)\n", command, section)
	}
}

// Handle opening and searching the file.
// TODO: Split this into two functions. 1) Opening. 2) Searching.
func searchManPage(searchTerm string, path string, matchChan chan<- string) error {
	file, err := os.OpenFile(path, os.O_RDONLY, 0400)
	if err != nil {
		return err
	}
	defer file.Close()

	// reader is set depending on gzippedness of file.
	var reader *bufio.Reader

	// Check if the file was a gzip file and set reader appropriately.
	if filepath.Ext(path) == GZIP_EXTENSION {
		gz, err := gzip.NewReader(file)
		if err != nil {
			// IF there was an error opening the gzip reader, just return nil
			// and skip this file.
			fmt.Errorf("Error opening gzip reader for '%s'", path)
			return nil
		}
		reader = bufio.NewReader(gz)
		defer gz.Close()
	} else {
		reader = bufio.NewReader(file)
	}

	// Start reading through the file, line by line.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// Break if we get to the EOF
			if err == io.EOF {
				break
			}
			// Report the error if it's anything other than EOF.
			fmt.Errorf("Unknown error while processing %s\n", path)
			fmt.Errorf("Error: %s", err)
		}

		// Check for the searchTerm on the line of the file.
		if strings.Contains(line, searchTerm) {
			// Matches go to matchChan and are handled by the printMatch goroutine.
			matchChan <- path
			return nil
		}
	}

	return nil
}

// Closure around seenPages.
func walkFunc(pathChan chan<- string) func(filePath string, fileInfo os.FileInfo, err error) error {
	var seenPages = make(map[string]bool)

	// This function is passed to filepath.Walk
	return func(filePath string, fileInfo os.FileInfo, err error) error {
		// This usually occurs when a path doesn't exist.
		// Skip it.
		if err != nil {
			return nil
		}

		// Put filepaths into pathChan if it's a regular file.
		if fileInfo.Mode().IsRegular() {
			basename := path.Base(filePath)

			// If we haven't seen the manpage, pass it through the pathChan
			if _, ok := seenPages[basename]; !ok {
				// paths are passed to searchManPage via a goroutine in main()
				pathChan <- filePath

				// Flag manpage as seen so we don't bother searching it again.
				seenPages[basename] = true
			}
		}
		return nil
	}
}

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Println("Please provide a search term.")
		return
	}
	searchTerm := flag.Arg(0)

	runtime.GOMAXPROCS(NCPUS)

	pathChan := make(chan string, NCPUS*4)
	matchChan := make(chan string, NCPUS*4)

	// printMatch prints things that arrive on the matchChan
	go printMatch(matchChan)

	var wg sync.WaitGroup

	for i := 0; i < NCPUS*2; i++ {
		// A new WaitGroup for each goroutine
		wg.Add(1)
		go func() {
			for path := range pathChan {
				// We're discarding errors from searchManPages for now.
				_ = searchManPage(searchTerm, path, matchChan)
			}
			// WaitGroup is finished after goroutine has processed all of pathChan
			wg.Done()
		}()
	}

	nextPath := walkFunc(pathChan)
	for _, path := range MANPATH {
		err := filepath.Walk(path, nextPath)
		if err != nil {
			continue
		}
	}
	close(pathChan)
	close(matchChan)

	// This WaitGroup is finished when the pathChan EOF is encountered in the
	// above goroutine.
	wg.Wait()
}
