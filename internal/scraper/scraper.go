package scraper

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// DownloadError represents a failed download attempt
type DownloadError struct {
	URL string
	Err error
}

// Scraper represents a web scraper with visited URL tracking
type Scraper struct {
	visited                sync.Map
	baseURL                *url.URL
	maxDepth               int
	concurrent             bool
	visitedURLs            []string
	detailedStats          bool
	statsLock              sync.Mutex
	totalLinks             int
	downloadFiles          bool
	downloadsDir           string
	downloadCount          int
	maxConcurrentDownloads int
	downloadWg             sync.WaitGroup
	downloadCh             chan string
	userAgent              string
	httpClient             *http.Client
	failedDownloads        []DownloadError
}

// ScraperOption defines the functional options for configuring a Scraper
type ScraperOption func(*Scraper)

// WithMaxDepth sets the maximum depth for recursive crawling
func WithMaxDepth(depth int) ScraperOption {
	return func(s *Scraper) {
		s.maxDepth = depth
	}
}

// WithConcurrent enables concurrent scraping
func WithConcurrent(concurrent bool) ScraperOption {
	return func(s *Scraper) {
		s.concurrent = concurrent
	}
}

// WithDetailedStats enables detailed statistics printing
func WithDetailedStats(detailed bool) ScraperOption {
	return func(s *Scraper) {
		s.detailedStats = detailed
	}
}

// WithFileDownloads enables downloading of files encountered during scraping
func WithFileDownloads(enable bool, downloadDir string) ScraperOption {
	return func(s *Scraper) {
		s.downloadFiles = enable
		
		// If no directory is specified, create a "downloads" folder in the current directory
		if downloadDir == "" {
			downloadDir = "downloads"
		}
		s.downloadsDir = downloadDir
		
		// Create the downloads directory if it doesn't exist
		if enable {
			if err := os.MkdirAll(downloadDir, 0755); err != nil {
				fmt.Printf("Warning: Failed to create downloads directory: %v\n", err)
			}
		}
	}
}

// WithParallelDownloads sets the maximum number of concurrent file downloads
func WithParallelDownloads(maxConcurrent int) ScraperOption {
	return func(s *Scraper) {
		if maxConcurrent <= 0 {
			maxConcurrent = 5 // Default to 5 concurrent downloads if invalid value provided
		}
		s.maxConcurrentDownloads = maxConcurrent
	}
}

// WithUserAgent sets a custom user agent for HTTP requests
func WithUserAgent(userAgent string) ScraperOption {
	return func(s *Scraper) {
		s.userAgent = userAgent
	}
}

// NewScraper creates a new Scraper instance with options
func NewScraper(options ...ScraperOption) *Scraper {
	s := &Scraper{
		maxDepth:      10, // Default max depth
		concurrent:    false,
		detailedStats: true, // Enable detailed stats by default
		downloadFiles: false, // Disable file downloads by default
		downloadsDir:  "downloads", // Default downloads directory
		maxConcurrentDownloads: 5, // Default to 5 concurrent downloads
		userAgent:     "Go-Web-Scraper/1.0", // Default user agent
		failedDownloads: make([]DownloadError, 0),
		httpClient:    &http.Client{
			Timeout: time.Second * 30, // Default timeout of 30 seconds
		},
	}

	for _, option := range options {
		option(s)
	}

	// Initialize download channel if file downloads are enabled
	if s.downloadFiles && s.maxConcurrentDownloads > 0 {
		s.downloadCh = make(chan string, 100) // Buffer for pending downloads
	}

	return s
}

// downloadWorker processes download tasks from the download channel
func (s *Scraper) downloadWorker() {
	defer s.downloadWg.Done()
	
	for url := range s.downloadCh {
		err := s.downloadFile(url)
		if err != nil {
			s.statsLock.Lock()
			fmt.Printf("Failed to download file %s: %v\n", url, err)
			// Track failed downloads for reporting
			s.failedDownloads = append(s.failedDownloads, struct {
				URL string
				Err error
			}{URL: url, Err: err})
			s.statsLock.Unlock()
		}
	}
}

// Scrape starts the scraping process from a given URL
func (s *Scraper) Scrape(urlStr string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	
	s.baseURL = parsedURL
	fmt.Printf("Base URL set to: %s (domain: %s, path: %s)\n", 
		urlStr, s.baseURL.Hostname(), s.baseURL.Path)
	
	// Start download workers if file downloads are enabled
	if s.downloadFiles && s.maxConcurrentDownloads > 0 {
		// Create a new download channel if it's nil or closed
		if s.downloadCh == nil {
			s.downloadCh = make(chan string, 100) // Buffer for pending downloads
			
			fmt.Printf("Starting %d download workers for parallel file downloads\n", s.maxConcurrentDownloads)
			s.downloadWg.Add(s.maxConcurrentDownloads)
			for i := 0; i < s.maxConcurrentDownloads; i++ {
				go s.downloadWorker()
			}
		}
	}
	
	// Start recursive scraping
	err = s.scrapeURL(urlStr, 0)
	if err != nil {
		return err
	}
	
	// Don't close the download channel here - we'll be calling Scrape repeatedly
	// We'll handle channel closure in a new Cleanup method
	
	return nil
}

// Cleanup performs cleanup operations like closing channels and waiting for workers
// Should be called after all scraping is complete
func (s *Scraper) Cleanup() {
	// Close the download channel and wait for workers to finish
	if s.downloadFiles && s.maxConcurrentDownloads > 0 && s.downloadCh != nil {
		close(s.downloadCh)
		s.downloadCh = nil
		s.downloadWg.Wait()
		fmt.Println("All file downloads completed")
	}
}

// scrapeURL scrapes a URL and recursively scrapes all links found
func (s *Scraper) scrapeURL(urlStr string, depth int) error {
	// Check max depth
	if depth > s.maxDepth {
		return nil
	}
	
	// Check if URL was already visited
	if _, exists := s.visited.Load(urlStr); exists {
		return nil
	}
	
	// Mark as visited
	s.visited.Store(urlStr, true)
	s.visitedURLs = append(s.visitedURLs, urlStr)
	
	fmt.Printf("Scraping URL (depth %d): %s\n", depth, urlStr)
	
	// Check if this URL is a downloadable file
	if s.downloadFiles && s.isDownloadableFile(urlStr) {
		if s.maxConcurrentDownloads > 0 {
			// Queue the file for parallel downloading by workers
			s.downloadCh <- urlStr
		} else {
			// Download synchronously if parallel downloads not enabled
			if err := s.downloadFile(urlStr); err != nil {
				fmt.Printf("Failed to download file %s: %v\n", urlStr, err)
			}
		}
		return nil // Don't try to extract links from files
	}
	
	// Fetch the page
	links, err := s.extractLinks(urlStr)
	if err != nil {
		return fmt.Errorf("failed to extract links from %s: %w", urlStr, err)
	}
	
	// Print stats about found links
	if s.detailedStats {
		s.statsLock.Lock()
		s.totalLinks += len(links)
		fmt.Printf("  Found %d links on this page (total links found so far: %d)\n", len(links), s.totalLinks)
		
		// Print a sample of links found (up to 5)
		if len(links) > 0 {
			maxSample := 5
			if len(links) < maxSample {
				maxSample = len(links)
			}
			fmt.Printf("  Sample links:\n")
			for i := 0; i < maxSample; i++ {
				fmt.Printf("    - %s\n", links[i])
			}
			if len(links) > maxSample {
				fmt.Printf("    - ... and %d more\n", len(links)-maxSample)
			}
		}
		s.statsLock.Unlock()
	}
	
	// Process found links
	var wg sync.WaitGroup
	for _, link := range links {
		normalizedLink := s.normalizeURL(link)
		if normalizedLink == "" {
			continue
		}
		
		// Skip if not part of the same site
		if !s.isSameDomain(normalizedLink) {
			continue
		}
		
		if s.concurrent {
			wg.Add(1)
			go func(link string) {
				defer wg.Done()
				s.scrapeURL(link, depth+1)
			}(normalizedLink)
		} else {
			s.scrapeURL(normalizedLink, depth+1)
		}
	}
	
	if s.concurrent {
		wg.Wait()
	}
	
	return nil
}

// extractLinks fetches a URL and extracts all links from it
func (s *Scraper) extractLinks(urlStr string) ([]string, error) {
    resp, err := s.fetchURL(urlStr)
    if err != nil {
        return nil, fmt.Errorf("failed to fetch URL: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("error: received status code %d", resp.StatusCode)
    }

    doc, err := html.Parse(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to parse HTML: %w", err)
    }

    var links []string
    var f func(*html.Node)
    f = func(n *html.Node) {
        if n.Type == html.ElementNode && n.Data == "a" {
            for _, a := range n.Attr {
                if a.Key == "href" {
                    links = append(links, a.Val)
                }
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            f(c)
        }
    }
    f(doc)

    return links, nil
}

// normalizeURL converts relative URLs to absolute URLs
func (s *Scraper) normalizeURL(href string) string {
	relativeURL, err := url.Parse(href)
	if err != nil {
		return ""
	}
	
	// Skip javascript: URLs, mailto: links, and fragments only links
	if strings.HasPrefix(href, "javascript:") || 
	   strings.HasPrefix(href, "mailto:") || 
	   strings.HasPrefix(href, "#") {
		return ""
	}
	
	absoluteURL := s.baseURL.ResolveReference(relativeURL)
	return absoluteURL.String()
}

// isSameDomain checks if a URL belongs to the same domain as the base URL
// and ensures it's within the baseURL path
func (s *Scraper) isSameDomain(urlStr string) bool {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	
	// First check if it's on the same hostname
	if parsedURL.Hostname() != s.baseURL.Hostname() {
		return false
	}
	
	// Now check if the URL starts with the base URL path
	// This ensures we only crawl within the specified base URL
	baseURLPath := s.baseURL.Path
	if baseURLPath == "" {
		baseURLPath = "/"
	}
	
	// If the base URL has a path, ensure the URL we're checking starts with that path
	if baseURLPath != "/" {
		return strings.HasPrefix(parsedURL.Path, baseURLPath)
	}
	
	return true
}

// GetVisitedURLs returns all visited URLs
func (s *Scraper) GetVisitedURLs() []string {
	return s.visitedURLs
}

// PrintVisitedURLs prints all visited URLs with statistics
func (s *Scraper) PrintVisitedURLs() {
	fmt.Printf("\n===== Web Scraping Summary =====\n")
	fmt.Printf("User-Agent: %s\n", s.userAgent)
	fmt.Printf("Total pages visited: %d\n", len(s.visitedURLs))
	fmt.Printf("Total links found: %d\n", s.totalLinks)
	
	if s.downloadFiles {
		fmt.Printf("Files downloaded: %d\n", s.downloadCount)
		fmt.Printf("Download directory: %s\n", s.downloadsDir)
		
		// Print information about failed downloads if any
		if len(s.failedDownloads) > 0 {
			fmt.Printf("Failed downloads: %d\n", len(s.failedDownloads))
			if len(s.failedDownloads) <= 5 {
				for _, failed := range s.failedDownloads {
					fmt.Printf(" - %s: %v\n", failed.URL, failed.Err)
				}
			} else {
				for i := 0; i < 5; i++ {
					fmt.Printf(" - %s: %v\n", s.failedDownloads[i].URL, s.failedDownloads[i].Err)
				}
				fmt.Printf(" - and %d more...\n", len(s.failedDownloads)-5)
			}
		}
	}
	
	// Print visited URLs with numbered list
	fmt.Printf("\nVisited URLs:\n")
	for i, url := range s.visitedURLs {
		fmt.Printf("%d. %s\n", i+1, url)
	}
	fmt.Printf("=============================\n")
}

// isDownloadableFile determines if a URL points to a downloadable file based on its extension
func (s *Scraper) isDownloadableFile(urlStr string) bool {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	
	// Common file extensions that should be downloaded
	downloadableExtensions := []string{
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".zip", ".tar", ".gz", ".rar", ".7z",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg",
		".mp3", ".mp4", ".avi", ".mov", ".wmv",
		".txt", ".csv", ".json", ".xml",
	}
	
	// Get the filename from the path
	filename := path.Base(parsedURL.Path)
	
	// Check if the URL has a query string which might contain a filename
	if parsedURL.RawQuery != "" {
		// Some websites use query parameters to serve files
		// Example: /download?file=document.pdf
		queryParams, _ := url.ParseQuery(parsedURL.RawQuery)
		
		// Common parameter names that might contain filenames
		filenameParams := []string{"file", "filename", "name", "download", "id"}
		
		for _, param := range filenameParams {
			if val := queryParams.Get(param); val != "" && strings.Contains(val, ".") {
				// If the parameter value looks like a filename with extension
				filename = val
				break
			}
		}
	}
	
	// Check if the filename has any of the downloadable extensions
	lowercaseFilename := strings.ToLower(filename)
	for _, ext := range downloadableExtensions {
		if strings.HasSuffix(lowercaseFilename, ext) {
			return true
		}
	}
	
	return false
}

// downloadFile downloads a file from a URL and saves it to the downloads directory
func (s *Scraper) downloadFile(urlStr string) error {
	// Parse the URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL for download: %w", err)
	}
	
	// Get the filename from the URL
	filename := path.Base(parsedURL.Path)
	
	// If the filename is unclear, extract it from query parameters or create a unique name
	if filename == "/" || filename == "." || filename == "" {
		// Try to extract filename from query parameters
		if parsedURL.RawQuery != "" {
			queryParams, _ := url.ParseQuery(parsedURL.RawQuery)
			for _, param := range []string{"file", "filename", "name", "download", "id"} {
				if val := queryParams.Get(param); val != "" {
					filename = val
					break
				}
			}
		}
		
		// If still no valid filename, generate one based on URL and timestamp
		if filename == "/" || filename == "." || filename == "" {
			hostname := parsedURL.Hostname()
			pathStr := strings.Replace(parsedURL.Path, "/", "_", -1)
			filename = fmt.Sprintf("%s%s_%d", hostname, pathStr, s.downloadCount)
		}
	}
	
	// Create a sanitized, filesystem-safe filename
	filename = strings.Map(func(r rune) rune {
		// Replace characters that are invalid in filenames with underscores
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, filename)
	
	// Ensure we don't overwrite files with the same name by adding a counter if needed
	filePath := filepath.Join(s.downloadsDir, filename)
	counter := 1
	for {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			break
		}
		
		// If file exists, add a counter to the filename
		ext := path.Ext(filename)
		nameWithoutExt := strings.TrimSuffix(filename, ext)
		filePath = filepath.Join(s.downloadsDir, fmt.Sprintf("%s_%d%s", nameWithoutExt, counter, ext))
		counter++
	}
	
	// Create the file
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()
	
	// Download the file
	fmt.Printf("Downloading file: %s\n", urlStr)
	resp, err := s.fetchURL(urlStr)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()
	
	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	
	// Write the body to file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	
	// Increment download count
	s.statsLock.Lock()
	s.downloadCount++
	s.statsLock.Unlock()
	
	fmt.Printf("Downloaded: %s -> %s\n", urlStr, filePath)
	return nil
}