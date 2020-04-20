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

type result struct {
	url, title, err string
	responseCode    int
}

func getTitle(body io.ReadCloser) string {
	tokenizer := html.NewTokenizer(body)
	title := "<title> tag missing"
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {
				break
			} else {
				title = err.Error()
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

func getWebContent(client *http.Client, wg *sync.WaitGroup, urls <-chan string, results chan<- result, id int) {
	// 4 - when finished, decrement the counter
	defer wg.Done()

	// 1 - read urls from 'urls' channel
	for url := range urls {

		// 2 - fetch web data
		req, err := http.NewRequest(http.MethodGet, url, nil)

		if err != nil {
			results <- result{url: url, err: err.Error()}
			return
		}

		resp, err := client.Do(req)

		if err != nil {
			results <- result{url: url, err: err.Error()}
			return
		}

		if resp != nil {
			defer resp.Body.Close()
			// 3 - write the result in 'results' channel
			results <- result{url: url, responseCode: resp.StatusCode, title: getTitle(resp.Body)}

		}

	}
}

func printOutput(wg *sync.WaitGroup, results <-chan result, colored bool) {
	defer wg.Done()
	var template string
	if colored {
		template = "\033[1;34m%-60s\033[0m\033[1;33m[%-3s]\033[0m \033[1;32m%s\033[0m\033[1;31m%s\033[0m\n"
	} else {
		template = "%-60s[%-3s] %s%s\n"
	}
	for r := range results {
		fmt.Printf(template, r.url, fmt.Sprint(r.responseCode), r.title, r.err)
	}
}

func main() {

	var nWorkers int
	flag.IntVar(&nWorkers, "n", 20, "Number of concurrent workers")

	var followRedirect bool
	flag.BoolVar(&followRedirect, "f", false, "Follow redirects")

	var timeout int
	flag.IntVar(&timeout, "t", 20, "Request timeout (in seconds)")

	var colored bool
	flag.BoolVar(&colored, "c", false, "Colored output")

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
	results := make(chan result)

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
	go printOutput(&resultWG, results, colored)

	scanner := bufio.NewScanner(os.Stdin)

	// read the urls from standard input and send them into channel 'urls'
	for scanner.Scan() {
		url := strings.Trim(scanner.Text(), " ")
		urls <- url
	}

	if scanner.Err() != nil {
		fmt.Print(scanner.Err())
	}

	// close the channel (we don't need to send messages anymore)
	close(urls)

	// wait until the output is printed
	resultWG.Wait()
}
