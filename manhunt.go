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

// TODO: Don't assume that manpages are all compressed.
func decompressAndSearch(searchTerm string, path string, matchChan chan<- string) error {
	file, err := os.OpenFile(path, os.O_RDONLY, 0400)
	if err != nil {
		return err
	}
	defer file.Close()

	// TODO: We should check the filemagic to see if it's a gzip file before
	// doing this
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil
	}
	gzRead := bufio.NewReader(gz)
	defer gz.Close()

	for {
		line, err := gzRead.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Errorf("Unknown error while processing %s\n", path)
		}

		if strings.Contains(line, searchTerm) {
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
		// paths are passed to decompressAndSearch via a goroutine in main()
		pathChan <- path
	}
	return nil
}

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Println("Please provide a search term.")
	}
	searchTerm := flag.Arg(0)

	runtime.GOMAXPROCS(NCPUS)

	// pathchan is global
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
				_ = decompressAndSearch(searchTerm, path, matchChan)
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
