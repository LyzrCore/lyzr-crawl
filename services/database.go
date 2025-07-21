package services

import (
	"context"
	"crawler/config"
	"crawler/models"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitMongoDB initializes the MongoDB connection
func InitMongoDB(mongoURI, dbName string) error {
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	// Test the connection
	err = client.Ping(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to ping MongoDB: %v", err)
	}

	config.MongoClient = client
	db := client.Database(dbName)
	config.JobsCollection = db.Collection("jobs")

	log.Printf("Connected to MongoDB: %s/%s", mongoURI, dbName)

	// Create TTL index for automatic job cleanup
	if err := CreateJobsTTLIndex(); err != nil {
		log.Printf("Warning: Failed to create TTL index for jobs cleanup: %v", err)
		// Don't fail initialization, just log the warning
	}

	return nil
}

// CreateJobsTTLIndex creates a TTL index on the jobs collection to automatically delete old jobs
func CreateJobsTTLIndex() error {
	if config.JobsCollection == nil {
		return fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create TTL index on created_at field with 24-hour expiration
	indexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "created_at", Value: 1},
		},
		Options: options.Index().SetExpireAfterSeconds(86400), // 24 hours = 86400 seconds
	}

	indexName, err := config.JobsCollection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		return fmt.Errorf("failed to create TTL index: %v", err)
	}

	log.Printf("Created TTL index '%s' on jobs collection - jobs will auto-expire after 24 hours", indexName)
	return nil
}

// SaveJobToMongoDB saves a job to the jobs collection
func SaveJobToMongoDB(job *models.JobStatus) error {
	if config.JobsCollection == nil {
		return fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := config.JobsCollection.InsertOne(ctx, job)
	return err
}

// UpdateJobInMongoDB updates a job in the jobs collection
func UpdateJobInMongoDB(job *models.JobStatus) error {
	if config.JobsCollection == nil {
		return fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": job.ID}
	update := bson.M{"$set": job}

	_, err := config.JobsCollection.UpdateOne(ctx, filter, update)
	return err
}

// GetJobFromMongoDB retrieves a job from the jobs collection
func GetJobFromMongoDB(jobID string) (*models.JobStatus, error) {
	if config.JobsCollection == nil {
		return nil, fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var job models.JobStatus
	err := config.JobsCollection.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job)
	if err != nil {
		return nil, err
	}

	return &job, nil
}

// LoadActiveJobsFromMongoDB loads running jobs from MongoDB on startup
func LoadActiveJobsFromMongoDB() {
	if config.JobsCollection == nil {
		log.Println("Jobs collection not initialized, skipping job recovery")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find all running jobs
	cursor, err := config.JobsCollection.Find(ctx, bson.M{"status": "running"})
	if err != nil {
		log.Printf("Failed to load active jobs from MongoDB: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var recoveredJobs []models.JobStatus
	if err := cursor.All(ctx, &recoveredJobs); err != nil {
		log.Printf("Failed to decode active jobs: %v", err)
		return
	}

	// Load recovered jobs into memory
	config.JobsMutex.Lock()
	for _, job := range recoveredJobs {
		// Mark recovered jobs as failed since the process was interrupted
		job.Status = "failed"
		job.Error = "Job interrupted by server restart"
		job.UpdatedAt = time.Now()
		
		config.ActiveJobs[job.ID] = &job
		
		// Update status in MongoDB
		go UpdateJobInMongoDB(&job)
	}
	config.JobsMutex.Unlock()

	log.Printf("Recovered %d interrupted jobs from MongoDB", len(recoveredJobs))
}