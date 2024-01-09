package yessir

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type Fetcher interface {
	Fetch(url string) (body string, urls []string, err error)
}

var visited sync.Map

type realFetcher struct{}

func (r realFetcher) Fetch(url string) (body string, urls []string, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	// Read the body
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	body = string(b)

	// Extract URLs from the body
	tokenizer := html.NewTokenizer(strings.NewReader(body))
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return body, urls, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data == "a" || token.Data == "link" {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						if strings.Contains(attr.Val, "http") {
							urls = append(urls, attr.Val)
						} else {
							urls = append(urls, url+attr.Val)
						}
					}
				}
			}
		}
	}
}

// Crawl recursively crawls pages starting with the given URL
func Crawl(url string, depth int, fetcher Fetcher, ch chan Res, errs chan error, wg *sync.WaitGroup, mu *sync.Mutex, followExternalLinks bool) {
	defer wg.Done()

	mu.Lock()
	visited.Store(url, true)
	mu.Unlock()

	body, urls, err := fetcher.Fetch(url)
	if err != nil {
		errs <- err
		return
	}

	// Download images for the current URL
	imageURLs := ExtractImages(body, url)
	for _, imgURL := range imageURLs {
		DownloadImage(imgURL, "D:\\awesomeProject\\images")
	}

	newUrls := 0
	if depth > 1 {
		for _, u := range urls {
			if followExternalLinks || isInternalLink(url, u) {
				_, alreadyVisited := visited.Load(u)
				if !alreadyVisited {
					newUrls++
					wg.Add(1)
					go Crawl(u, depth-1, fetcher, ch, errs, wg, mu, followExternalLinks)
				}
			}
		}
	}

	// Send the result along with the number of URLs to be fetched
	ch <- Res{url, body, newUrls}
}

// Res represents the result of crawling a URL
type Res struct {
	url   string
	body  string
	found int // Number of new URLs found
}

func main() {
	followExternalLinks := flag.Bool("follow-external", true, "Whether to follow external links")
	flag.Parse()

	ch := make(chan Res)
	errs := make(chan error)
	var wg sync.WaitGroup
	var mu sync.Mutex

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Use realFetcher for actual web crawling
	wg.Add(1)
	go Crawl("https://rarehistoricalphotos.com", 3, realFetcher{}, ch, errs, &wg, &mu, *followExternalLinks)

	toCollect := 1
	for n := 0; n < toCollect; n++ {
		select {
		case s := <-ch:
			fmt.Printf("found: %s\n", s.url)
			toCollect += s.found
		case e := <-errs:
			fmt.Println(e)
		case <-ctx.Done():
			fmt.Println("Crawling timed out")
		}
	}

	// Wait for all goroutines to finish before exiting
	wg.Wait()
	close(ch)
	close(errs)
}

// ExtractImages extracts image URLs from the HTML body
func ExtractImages(body string, url string) []string {
	var imageURLs []string

	tokenizer := html.NewTokenizer(strings.NewReader(body))
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return imageURLs
		case html.SelfClosingTagToken, html.StartTagToken:
			token := tokenizer.Token()
			if token.Data == "img" {
				for _, attr := range token.Attr {
					if attr.Key == "src" {
						if strings.Contains(attr.Val, "http") {
							imageURLs = append(imageURLs, attr.Val)
						} else {
							imageURLs = append(imageURLs, url+attr.Val)
						}
					}
				}
			}
		}
	}
}

// DownloadImage downloads and saves an image to the specified directory
func DownloadImage(imgURL string, directory string) {
	resp, err := http.Get(imgURL)
	if err != nil {
		fmt.Printf("Error downloading image from %s: %v\n", imgURL, err)
		return
	}
	defer resp.Body.Close()

	// Extract the file name from the URL
	fileName := filepath.Base(imgURL)
	fileName = sanitizeFileName(fileName)

	// Create the file in the specified directory
	filePath := filepath.Join(directory, fileName)
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("Error creating file for %s: %v\n", imgURL, err)
		return
	}
	defer file.Close()

	// Write the image content to the file
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading image content from %s: %v\n", imgURL, err)
		return
	}
	_, err = file.Write(body)
	if err != nil {
		fmt.Printf("Error writing image content to file for %s: %v\n", imgURL, err)
		return
	}

	fmt.Printf("Downloaded and saved image: %s\n", filePath)
}

func sanitizeFileName(name string) string {
	parts := strings.Split(name, "?")
	return parts[0]
}

func isInternalLink(baseURL, link string) bool {
	base, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	rel, err := url.Parse(link)
	if err != nil {
		return false
	}

	target := base.ResolveReference(rel)
	return base.Host == target.Host
}
