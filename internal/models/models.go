package models

// Article represents the structure of the data being scraped for articles.
type Article struct {
	Title   string
	URL     string
	Summary string
}

// Product represents the structure of the data being scraped for products.
type Product struct {
	Name     string
	Price    float64
	Category string
}