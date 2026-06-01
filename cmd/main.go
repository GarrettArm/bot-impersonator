package main

import (
	"fmt"
	"go-web-scraper/internal/scraper"
	"log"
	"time"
)

func runAScrape(iteration int) {
	// Create a new scraper with optional configurations
	s := scraper.NewScraper(
		scraper.WithMaxDepth(10),                            // Only crawl 10 levels deep
		scraper.WithConcurrent(true),                        // Enable concurrent crawling
		scraper.WithDetailedStats(true),                     // Enable detailed statistics display
		scraper.WithFileDownloads(true, "downloaded_files"), // Enable file downloading to the specified directory
		scraper.WithParallelDownloads(5),                    // Enable parallel downloads with 5 workers
		// scraper.WithUserAgent("Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm) Chrome/116.0.1938.76 Safari/537.36"), // Set custom user agent
		// scraper.WithUserAgent("Mozilla/5.0 (compatible; Barkrowler/0.9; +https://babbar.tech/crawler)"), // Set custom user agent
		scraper.WithUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:137.0) Gecko/20100101 Firefox/137.0"), // Set custom user agent
	)

	baseUrl := "http://localhost:80"
	fmt.Printf("Only URLs within the base URL path will be crawled: %s\n", baseUrl)

	fmt.Printf("\n--- Starting Scraping Iteration %d ---\n", iteration)

	err := s.Scrape(baseUrl)
	if err != nil {
		log.Printf("Error during scraping iteration %d: %v", iteration, err)
	} else {
		fmt.Printf("Iteration %d completed successfully\n", iteration)
		// Print intermediate results after each iteration
		s.PrintVisitedURLs()
	}

	// Clean up resources
	s.Cleanup()
}

func main() {
	// Set the total duration for scraping
	scrapeDuration := 5 * time.Minute // Run for 5 minutes

	fmt.Printf("Starting recursive web scraping for %s...\n", scrapeDuration)
	fmt.Println("WARNING: No delay between scraping iterations - server load will be high!")

	startTime := time.Now()
	endTime := startTime.Add(scrapeDuration)
	iteration := 1

	// Loop until the duration expires
	for time.Now().Before(endTime) {
		// Execute one complete scraping run
		runAScrape(iteration)

		// Increment iteration counter
		iteration++

		// Check if we still have time for another iteration
		if !time.Now().Before(endTime) {
			break
		}
	}

	elapsedTime := time.Since(startTime)
	fmt.Println("\nScraping process completed!")
	fmt.Printf("Ran for a total of %d iterations over %s\n", iteration-1, elapsedTime.Round(time.Second))
}
