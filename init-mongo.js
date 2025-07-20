// MongoDB initialization script for Web Crawler
// This script runs when the MongoDB container starts for the first time
// It sets up the database schema and performance indexes

print('🚀 Starting MongoDB initialization for Web Crawler...');

// Switch to the crawler database
db = db.getSiblingDB('crawler');

// Create collections for the web crawler
print('📋 Creating collections...');
db.createCollection('crawls');
db.createCollection('jobs');

// Create indexes on the crawls collection for better performance
print('⚡ Creating indexes for crawls collection...');
db.crawls.createIndex({ "target_url": 1 });
db.crawls.createIndex({ "crawled_at": -1 });
db.crawls.createIndex({ "settings.depth": 1 });
db.crawls.createIndex({ "settings.workers": 1 });
db.crawls.createIndex({ "total_urls": -1 });
db.crawls.createIndex({ "duration": 1 });

// Create indexes on the jobs collection for real-time job tracking
print('⚡ Creating indexes for jobs collection...');
db.jobs.createIndex({ "_id": 1 }, { unique: true });
db.jobs.createIndex({ "status": 1 });
db.jobs.createIndex({ "created_at": -1 });
db.jobs.createIndex({ "updated_at": -1 });
db.jobs.createIndex({ "request.url": 1 });

// Optional: Create a dedicated application user (commented out by default)
// Uncomment the following lines if you want to use a dedicated app user instead of root
/*
print('👤 Creating application user...');
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
print('User created: crawler_app');
*/

// Create some helpful compound indexes for common queries
print('🔍 Creating compound indexes for common queries...');
db.crawls.createIndex({ "target_url": 1, "crawled_at": -1 });
db.crawls.createIndex({ "settings.depth": 1, "total_urls": -1 });
db.jobs.createIndex({ "status": 1, "created_at": -1 });

print('✅ MongoDB initialization complete!');
print('📊 Summary:');
print('  Database: crawler');
print('  Collections: crawls, jobs');
print('  Authentication: Using root user (crawler/crawler123)');
print('  Indexes: Performance optimized for common queries');
print('  Connection: mongodb://crawler:crawler123@mongodb:27017/crawler?authSource=admin');