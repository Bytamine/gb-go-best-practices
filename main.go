package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
	defer func() {
		if r := recover(); r != nil {
			results <- crawlResult{
				err: errors.Errorf("panic: %v on url: %s", r, url),
			}
		}
	}()

	if c.checkVisited(url) {
		return
	}
	if depth > c.maxDepth {
		return
	}

	if rand.Intn(12) == 0 {
		panic("random panic")
	}

	log.Trace().Msgf("crawling %s, depth %d", url, depth)
	time.Sleep(1 * time.Second)

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
				log.Error().Msgf("close body error: %v", err)
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
				log.Error().Msgf("%s", msg.err)
			} else {
				log.Info().Msgf("%s", msg.msg)
			}
		}
	}
}

func setup() {

	zerolog.TimeFieldFormat = ""
	zerolog.TimestampFunc = func() time.Time {
		return time.Date(2008, 1, 8, 17, 5, 05, 0, time.UTC)
	}
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
}

func main() {
	setup()

	log.Debug().Msgf("Program started, pid: %d", os.Getpid())
	started := time.Now()

	rand.Seed(time.Now().UnixNano())

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
			log.Debug().Msgf("Running time %v", time.Since(started))
			return
		case sig := <-osSignalChan:
			if sig == syscall.SIGINT {
				cancel()
			}
			if sig == syscall.SIGUSR1 {
				crawler.maxDepth += 2
				log.Trace().Msgf("depth increased to %d", crawler.maxDepth)
			}
		}
	}

}
