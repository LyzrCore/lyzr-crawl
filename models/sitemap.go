package models

import (
	"encoding/xml"
	"time"
)

// Sitemap XML structures for parsing
type SitemapURL struct {
	Loc        string    `xml:"loc"`
	LastMod    time.Time `xml:"lastmod"`
	ChangeFreq string    `xml:"changefreq"`
	Priority   float64   `xml:"priority"`
}

type SitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []SitemapURL `xml:"url"`
}

type SitemapReference struct {
	Loc     string    `xml:"loc"`
	LastMod time.Time `xml:"lastmod"`
}

type SitemapIndex struct {
	XMLName  xml.Name           `xml:"sitemapindex"`
	Sitemaps []SitemapReference `xml:"sitemap"`
}

// URLFallback contains information about URL fallback attempts
type URLFallback struct {
	OriginalURL string
	FallbackURL string
	Success     bool
	Error       string
}