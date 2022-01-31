package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
)

type crawlResult struct {
	err error
	msg string
}

type crawler struct {
	sync.Mutex
	visited  map[string]struct{}
	maxDepth int
}

func newCrawler(maxDepth int) *crawler {
	return &crawler{
		visited:  make(map[string]struct{}),
		maxDepth: maxDepth,
	}
}

// scan pages recursively
func (c *crawler) run(ctx context.Context, url string, results chan<- crawlResult, depth int) {
	if c.checkVisited(url) {
		return
	}
	if depth > c.maxDepth {
		return
	}

	// log.Printf("crawling %s, depth %d\n", url, depth)
	// time.Sleep(1 * time.Second)

	select {
	case <-ctx.Done():
		return

	default:
		resp, err := http.Get(url)
		if err != nil {
			results <- crawlResult{
				err: errors.Wrapf(err, "get page %s", url),
			}
			return
		}
		defer func() {
			err := resp.Body.Close()
			if err != nil {
				log.Printf("close body error: %v\n", err)
			}
		}()
		if resp.StatusCode != 200 {
			results <- crawlResult{
				err: errors.Wrapf(err, "non 200 page status: %s", url),
			}
			return
		}

		c.Lock()
		c.visited[url] = struct{}{}
		c.Unlock()

		page, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			results <- crawlResult{
				err: errors.Wrapf(err, "parse page %s", url),
			}
			return
		}

		title := pageTitle(page)
		links := pageLinks(page)

		results <- crawlResult{
			err: nil,
			msg: fmt.Sprintf("%s -> %s", url, title),
		}

		for _, link := range links {
			go c.run(ctx, link, results, depth+1)
		}
	}
}

func pageTitle(page *goquery.Document) string {
	return page.Find("title").First().Text()
}

func pageLinks(page *goquery.Document) []string {
	urls := make([]string, 0)
	page.Find("a").Each(func(_ int, s *goquery.Selection) {
		url, ok := s.Attr("href")
		if ok && strings.Contains(url, "https://") {
			urls = append(urls, url)
		}
	})

	return urls
}

func (c *crawler) checkVisited(url string) bool {
	c.Lock()
	defer c.Unlock()

	_, ok := c.visited[url]
	return ok
}

func processResults(ctx context.Context, results <-chan crawlResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-results:
			if msg.err != nil {
				log.Printf("%s\n", msg.err)
			} else {
				log.Printf("%s\n", msg.msg)
			}
		}
	}
}

func main() {
	log.Printf("Program started, pid: %d\n", os.Getpid())
	started := time.Now()

	depthLimit := 1
	url := "https://pkg.go.dev"

	ctx, cancel := context.WithCancel(context.Background())
	crawler := newCrawler(depthLimit)
	results := make(chan crawlResult)
	go crawler.run(ctx, url, results, 0)
	go processResults(ctx, results)

	osSignalChan := make(chan os.Signal, 1)
	signal.Notify(osSignalChan, syscall.SIGINT, syscall.SIGUSR1)
	for {
		select {
		case <-ctx.Done():
			log.Println("Running time", time.Since(started))
			return
		case sig := <-osSignalChan:
			if sig == syscall.SIGINT {
				cancel()
			}
			if sig == syscall.SIGUSR1 {
				crawler.maxDepth += 2
				log.Printf("depth increased to %d", crawler.maxDepth)
			}
		}
	}

}
