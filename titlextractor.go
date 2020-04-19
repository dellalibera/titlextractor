package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const (
	blue  = "\033[1;34m%s\033[0m"
	red   = "\033[1;31m%s\033[0m"
	green = "\033[1;32m%s\033[0m"
)

func getTitle(body io.ReadCloser) string {
	tokenizer := html.NewTokenizer(body)
	title := fmt.Sprintf(red, "<title> tag missing")
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {
				break
			} else {
				title = fmt.Sprintf(red, err.Error())
			}
		}
		if tokenType == html.StartTagToken {
			token := tokenizer.Token()
			if "title" == token.Data {
				_ = tokenizer.Next()
				title = tokenizer.Token().Data
				break
			}
		}
	}
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	return title
}

func getWebContent(client *http.Client, wg *sync.WaitGroup, urls <-chan string, results chan<- []string, id int) {
	// 4 - when finished, decrement the counter
	defer wg.Done()

	// 1 - read urls from 'urls' channel
	for url := range urls {

		// 2 - fetch web data
		req, err := http.NewRequest(http.MethodGet, url, nil)

		if err != nil {
			results <- []string{url, fmt.Sprintf(red, err.Error())}
			return
		}

		resp, err := client.Do(req)

		if err != nil {
			results <- []string{url, fmt.Sprintf(red, err.Error())}
			return
		}

		if resp != nil {
			defer resp.Body.Close()
			// 3 - write the result in 'results' channel
			results <- []string{url, getTitle(resp.Body)}
		}

	}
}

func printOutput(wg *sync.WaitGroup, results <-chan []string) {
	defer wg.Done()

	for result := range results {
		fmt.Printf(blue, result[0])
		fmt.Printf(green, " --> "+result[1]+"\n")
	}
}

func main() {

	var nWorkers int
	flag.IntVar(&nWorkers, "n", 20, "Number of concurrent workers")

	var followRedirect bool
	flag.BoolVar(&followRedirect, "f", false, "Follow redirects")

	var timeout int
	flag.IntVar(&timeout, "t", 20, "Timeout (in seconds)")

	flag.Parse()

	redirectPolicyFunc := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	if followRedirect {
		redirectPolicyFunc = nil
	}

	// https://golang.org/pkg/net/http/#Transport
	var transport = &http.Transport{
		MaxIdleConns:      20,
		DisableKeepAlives: true,
		IdleConnTimeout:   time.Second,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(timeout) * time.Second,
			KeepAlive: time.Second,
		}).DialContext,
	}

	client := &http.Client{
		Timeout:       time.Duration(timeout) * time.Second,
		CheckRedirect: redirectPolicyFunc,
		Transport:     transport,
	}

	// channels
	urls := make(chan string)
	results := make(chan []string)

	var wg sync.WaitGroup
	var resultWG sync.WaitGroup

	// run n number of concurrent workers
	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go getWebContent(client, &wg, urls, results, i)
	}

	// wait until all the workers have done the job and then close the 'results' channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// we use only one worker for printing the output
	resultWG.Add(1)
	go printOutput(&resultWG, results)

	scanner := bufio.NewScanner(os.Stdin)

	// read the urls from standard input and send them into channel 'urls'
	for scanner.Scan() {
		url := strings.Trim(scanner.Text(), " ")
		urls <- url
	}

	if scanner.Err() != nil {
		fmt.Printf(red, scanner.Err())
	}

	// close the channel (we don't need to send messages anymore)
	close(urls)

	// wait until the output is printed
	resultWG.Wait()
}
