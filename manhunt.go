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
	"time"
)

const (
	NCPUS = 2
)

// TODO: Work this out automatically, if possible.
var MANPATH = [...]string{
	"/usr/local/share/man",
	"/usr/local/man",
	"/usr/share/man",
	"/usr/X11R6/man",
	"/opt/man",
}

var pathChan chan string

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

	// Check if the file was a gzip file.
	var gzipFile bool
	if filepath.Ext(path) == ".gz" {
		gzipFile = true
	}

	// reader is set depending on gzippedness of file.
	var reader *bufio.Reader

	// Use a gzip reader if the file was gzipped.
	if gzipFile {
		gz, err := gzip.NewReader(file)
		if err != nil {
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

// This function is passed to filepath.Walk
func walkFunc(path string, fileInfo os.FileInfo, err error) error {
	// This usually occurs when a path doesn't exist.
	// Skip it.
	if err != nil {
		return nil
	}

	// Put filepaths into pathChan if it's a regular file.
	if fileInfo.Mode().IsRegular() {
		// paths are passed to searchManPage via a goroutine in main()
		pathChan <- path
	}
	return nil
}

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Errorf("Please provide a search term.\n")
	}
	searchTerm := flag.Arg(0)

	runtime.GOMAXPROCS(NCPUS)

	// pathChan is global
	pathChan = make(chan string, NCPUS * 4)
	matchChan := make(chan string, NCPUS * 4)

	// printMatch prints things that arrive on the matchChan
	go printMatch(matchChan)

	var wg sync.WaitGroup

	for i := 0; i < NCPUS * 2; i++ {
		// A new WaitGroup for each goroutine
		wg.Add(1)
		go func() {
			for path := range pathChan {
				_ = searchManPage(searchTerm, path, matchChan)
			}
			// WaitGroup is finished after goroutine has processed all of pathChan
			wg.Done()
		}()
	}

	for _, path := range MANPATH {
		err := filepath.Walk(path, walkFunc)
		if err != nil {
			continue
		}
	}
	close(pathChan)

	// This WaitGroup is finished when the pathChan EOF is encountered in the
	// above goroutine.
	wg.Wait()

	// Probably not required, but I'm not sure.
	// Wait for matchChan to be exhausted before closing it.
	for {
		if len(matchChan) == 0 {
			close(matchChan)
			break
		}
		time.Sleep(1)
	}
}
