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
var matchChan chan string

func printMatch() {
	for match := range matchChan {
		basename := path.Base(match)
		manInfo := strings.Split(basename, ".")
		command := manInfo[0]
		section := manInfo[1]
		fmt.Printf("%s (%s)\n", command, section)
	}
}

func decompressAndSearch(searchTerm string, path string) error {
	file, err := os.OpenFile(path, os.O_RDONLY, 0400)
	if err != nil {
		return err
	}
	defer file.Close()

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
			fmt.Printf("Unknown error while processing %s\n", path)
		}

		if strings.Contains(line, searchTerm) {
			matchChan <- path
			return nil
		}
	}

	return nil
}

func walkFunc(path string, fileInfo os.FileInfo, err error) error {
	// This usually occurs when a path doesn't exist.
	// Skip it.
	if err != nil {
		return nil
	}

	// Put filepaths into pathChan if it's a regular file.
	if fileInfo.Mode().IsRegular() {
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

	pathChan = make(chan string, NCPUS * 4)
	matchChan = make(chan string, NCPUS * 4)

	go printMatch()

	var wg sync.WaitGroup

	for i := 0; i < NCPUS * 2; i++ {
		wg.Add(1)
		go func() {
			for path := range pathChan {
				_ = decompressAndSearch(searchTerm, path)
			}
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

	wg.Wait()
	// Probably not required, but I'm not sure.
	for {
		if len(matchChan) == 0 {
			close(matchChan)
			break
		}
		time.Sleep(1)
	}
}