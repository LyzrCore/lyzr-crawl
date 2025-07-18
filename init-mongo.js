// MongoDB initialization script
// This script runs when the MongoDB container starts for the first time

// Switch to the crawler database
db = db.getSiblingDB('crawler');

// Create a user for the crawler application
db.createUser({
  user: 'crawler_app',
  pwd: 'crawler_app_pass',
  roles: [
    {
      role: 'readWrite',
      db: 'crawler'
    }
  ]
});

// Create collections with indexes for better performance
db.createCollection('crawls');
db.createCollection('jobs');

// Create indexes on the crawls collection
db.crawls.createIndex({ "target_url": 1 });
db.crawls.createIndex({ "crawled_at": -1 });
db.crawls.createIndex({ "settings.depth": 1 });
db.crawls.createIndex({ "total_urls": -1 });

// Create indexes on the jobs collection (if we decide to store job status in MongoDB)
db.jobs.createIndex({ "job_id": 1 }, { unique: true });
db.jobs.createIndex({ "status": 1 });
db.jobs.createIndex({ "created_at": -1 });

print('MongoDB initialization complete!');
print('Database: crawler');
print('Collections: crawls, jobs');
print('User: crawler_app');
print('Indexes created for performance optimization');