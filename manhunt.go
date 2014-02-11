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
)

var numCPU = runtime.NumCPU()
var debug  = flag.Bool("debug", false, "Print extra debug messages.")

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

func errorLog(message string) {
	if *debug {
		fmt.Fprint(os.Stderr, message)
	}
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
func searchManPage(searchTerm string, manFilePath string, matchChan chan<- string) error {
	file, err := os.OpenFile(manFilePath, os.O_RDONLY, 0400)
	if err != nil {
		return err
	}
	defer file.Close()

	// reader is set depending on gzippedness of file.
	var reader *bufio.Reader

	// Check if the file was a gzip file and set reader appropriately.
	if filepath.Ext(manFilePath) == GZIP_EXTENSION {
		gz, err := gzip.NewReader(file)
		if err != nil {
			// If there was an error opening the gzip reader, just return nil
			// and skip this file.
			errorLog(fmt.Sprintf("Error opening gzip reader for '%s'\n", manFilePath))
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
			errorLog(fmt.Sprintf("Unknown error while processing %s\n", manFilePath))
			errorLog(fmt.Sprintf("Error: %s\n", err))
		}

		// Check for the searchTerm on the line of the file.
		if strings.Contains(line, searchTerm) {
			// Matches go to matchChan and are handled by the printMatch
			// goroutine.
			matchChan <- manFilePath
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
		if err != nil {
			// Skip non-existant paths.
			if os.IsNotExist(err) {
				errorLog(fmt.Sprintf("Error: '%s' does not exist.\n", filePath))
				return nil
			}
			errorLog(fmt.Sprintf("Error walking '%s': %s\n", filePath, err))
			return err
		}

		// Put filepaths into pathChan if it's a regular file.
		if fileInfo.Mode().IsRegular() {
			basename := path.Base(filePath)

			// Sometimes manpages on a system are both compressed and
			// uncompressed (why?). Remove the .gz suffix in the basename so
			// that we don't display those twice.
			basename = strings.TrimSuffix(basename, GZIP_EXTENSION)

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

	runtime.GOMAXPROCS(numCPU)

	pathChan := make(chan string, numCPU+1)
	matchChan := make(chan string, numCPU+1)

	// printMatch prints things that arrive on the matchChan
	go printMatch(matchChan)

	var wg sync.WaitGroup

	// Start a few goroutines for searching the manpages.
	for i := 0; i < numCPU*2; i++ {
		// A new WaitGroup for each goroutine
		wg.Add(1)
		go func() {
			for manFilePath := range pathChan {
				// We're discarding errors from searchManPages for now.
				searchManPage(searchTerm, manFilePath, matchChan)
			}
			// WaitGroup is finished after goroutine has processed all of
			// pathChan
			wg.Done()
		}()
	}

	// The walkFunc feeds the full paths of the files found within the
	// MANPATHs to pathChan. pathChan is read in the goroutine above and
	// the paths are fed to searchManPage()
	nextPath := walkFunc(pathChan)
	for _, manFilePath := range MANPATH {
		err := filepath.Walk(manFilePath, nextPath)
		if err != nil {
			fmt.Println(err)
			continue
		}
	}

	// No more paths to feed to pathChan, close it so the WaitGroups can exit.
	close(pathChan)

	// This WaitGroup is finished when the pathChan EOF is encountered in the
	// above goroutine.
	wg.Wait()

	// WaitGroups have exited, nothing else to match in matchChan
	close(matchChan)
}
