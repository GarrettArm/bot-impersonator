package scraper

import (
	"net/http"
)

// createRequest creates an HTTP request with the scraper's user agent
func (s *Scraper) createRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent)
	return req, nil
}

// fetchURL performs an HTTP GET request with the scraper's user agent
func (s *Scraper) fetchURL(url string) (*http.Response, error) {
	req, err := s.createRequest("GET", url)
	if err != nil {
		return nil, err
	}
	return s.httpClient.Do(req)
}
