package main

import (
	"regexp"
	"fmt"
	"path/filepath"
	"flag"
	"os"
	"log"
	"bufio"
	"io"
	"strings"
	"sync"
	"strconv"
	"runtime"
)

var (
	fileList	= flag.String("list", "", "a file contain the files you want statistic")
	help		= flag.Bool("help", false, "show usage")
	commentR	= regexp.MustCompile(`(?:^//.*)|(?:^/\*.*)|(?:.*\*/$)`)
)

const (
	workers	= 1000
)

func init() {
	flag.Usage = func() {
		fmt.Printf("usage: %s [option] <file1>...<fileN>\n",
			filepath.Base(os.Args[0]))
		fmt.Println("option:")
		flag.PrintDefaults()
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()
	if *help || (flag.NFlag() == 0 && flag.NArg() == 0) {
		flag.Usage()
		os.Exit(1)
	}
	files := flag.Args()
	if *fileList != "" {
		files = append(files, getFilesFromList(*fileList)...)
	}
	statisticFiles(files)
}

func getFilesFromList(listName string) (files []string) {
	list, err := os.Open(listName)
	if err != nil {
		log.Printf("open file(%q) failed: %s\n", listName, err)
		return nil
	}
	defer list.Close()
	reader := bufio.NewReader(list)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("parse line failed: %s\n", err)
		}
		line = strings.TrimSpace(line)
		files = append(files, line)
	}
	return files
}

type statisticInfo struct {
	filename	string	//filename
	total		int		//total line count
	space		int		//space line count
	comment		int		//comment line count
	regular		int		//regular line count
}

type statisticManage struct {
	infoC	popChannel
	done	chan int
	waiter	sync.WaitGroup
}

type statisticResult struct {
	totalFiles	int
	sumRegular	int
	sumSpace	int
	sumComment	int
	sumTotal	int
}

type popChannel chan []*statisticInfo

func newPopChannel() popChannel {
	return make(chan []*statisticInfo, 1)
}

func (p popChannel) stack(arg ...*statisticInfo) {
	toChannel := arg
	for {
		select {
		case p <- toChannel:
			return
		case old := <-p:
			toChannel = append(old, toChannel...)
		}
	}
}

func statisticFiles(filenames []string) {
	manage := &statisticManage{
		infoC:	newPopChannel(),
		done:	make(chan int),
	}
	go showResult(manage.infoC, manage.done)
	for _, filename := range filenames {
		if runtime.NumGoroutine() > workers {
			statisticFile(filename, manage.infoC, nil)
		} else {
			manage.waiter.Add(1)
			go statisticFile(filename, manage.infoC, func() { manage.waiter.Done() })
		}
	}
	manage.waiter.Wait()
	manage.done <- 0
	fmt.Printf("total files: %d<->%d\n", len(filenames), <-manage.done)
}

func showResult(infoC popChannel, done chan int) {
	const fomater = "%-80s %10s %10s %10s %10s\n"
	result := statisticResult{}

	fmt.Printf(fomater, "filename", "regular", "space", "comment", "total")
LOOP:
	for {
		select {
		case <-done:
			break LOOP
		case infos := <-infoC:
			showInfos(infos, &result, fomater)
		}
	}
	if len(infoC) > 0 {
		showInfos(<-infoC, &result, fomater)
	}

	fmt.Printf(fomater, "In total", strconv.Itoa(result.sumRegular), strconv.Itoa(result.sumSpace), strconv.Itoa(result.sumComment), strconv.Itoa(result.sumTotal))
	done <- result.totalFiles
}

func showInfos(infos []*statisticInfo, result *statisticResult, fomater string) {
	for _, info := range infos {
		fmt.Printf(fomater, info.filename, strconv.Itoa(info.regular), strconv.Itoa(info.space), strconv.Itoa(info.comment), strconv.Itoa(info.total))
		(*result).totalFiles++
		(*result).sumRegular += info.regular
		(*result).sumSpace += info.space
		(*result).sumComment += info.comment
		(*result).sumTotal += info.total
	}
}

func statisticFile(filename string, infoC popChannel, done func()) {
	if done != nil {
		defer done()
	}
	file, err := os.Open(filename)
	if err != nil {
		log.Printf("open file(%q) failed: %s\n", filename, err)
		return
	}
	defer file.Close()
	info := &statisticInfo{
		filename: filename,
	}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("parse line failed: %s\n", err)
		}
		info.total++
		line = strings.TrimSpace(line)
		switch {
		case commentR.MatchString(line):
			info.comment++
		case line == "":
			info.space++
		default:
			info.regular++
		}
	}
	infoC.stack(info)
}
