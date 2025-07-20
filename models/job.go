package models

import (
	"time"
)

// JobStatus represents the status of a crawl job
type JobStatus struct {
	ID        string               `json:"id" bson:"_id" example:"60f7b3b3b3b3b3b3b3b3b3b3"`
	Status    string               `json:"status" bson:"status" example:"completed" enum:"running,completed,failed"`
	Progress  string               `json:"progress,omitempty" bson:"progress,omitempty" example:"Starting crawl..."`
	Result    *CrawlResult         `json:"result,omitempty" bson:"result,omitempty"`
	Error     string               `json:"error,omitempty" bson:"error,omitempty" example:"Error message if failed"`
	CreatedAt time.Time            `json:"created_at" bson:"created_at" example:"2023-07-18T10:30:45Z"`
	UpdatedAt time.Time            `json:"updated_at" bson:"updated_at" example:"2023-07-18T10:32:15Z"`
	Request   *CrawlRequest        `json:"request,omitempty" bson:"request,omitempty"`
}