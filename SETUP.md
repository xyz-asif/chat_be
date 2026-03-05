# Anchor Platform - Setup Guide

## Prerequisites

### 1. **Go (Golang)**
- **Version**: Go 1.21 or higher
- **Installation**: 
  ```bash
  # macOS
  brew install go
  
  # Or download from https://golang.org/dl/
  ```
- **Verify**:
  ```bash
  go version
  ```

### 2. **MongoDB**
- **Version**: MongoDB 5.0 or higher (must support replica sets for transactions)
- **Installation**:
  ```bash
  # macOS
  brew tap mongodb/brew
  brew install mongodb-community@7.0
  ```
- **Start MongoDB**:
  ```bash
  brew services start mongodb-community@7.0
  ```

### 3. **Firebase Project**
- Create a Firebase project at [https://console.firebase.google.com](https://console.firebase.google.com)
- Enable **Authentication** → **Sign-in method** → Enable your preferred providers (Email/Password, Google, etc.)
- Generate a **Service Account Key**:
  1. Go to Project Settings → Service Accounts
  2. Click "Generate New Private Key"
  3. Save the JSON file securely

---

## Setup Steps

### Step 1: Clone and Install Dependencies

```bash
cd /Users/asif/development/Go\ lang/Anchor

# Download Go dependencies
go mod download
go mod tidy
```

### Step 2: Configure MongoDB Replica Set

> **CRITICAL**: MongoDB transactions require a replica set. Standalone MongoDB will fail.

```bash
# Stop MongoDB if running
brew services stop mongodb-community@7.0

# Start MongoDB with replica set
mongod --replSet rs0 --dbpath /usr/local/var/mongodb

# In a new terminal, initialize replica set
mongosh
> rs.initiate()
> rs.status()  # Verify it's active
> exit
```

**Alternative (Docker)**:
```bash
docker run -d --name mongodb-anchor \
  -p 27017:27017 \
  mongo:7.0 --replSet rs0

docker exec -it mongodb-anchor mongosh --eval "rs.initiate()"
```

### Step 3: Create Environment Configuration

Create a `.env` file in the project root:

```bash
cd /Users/asif/development/Go\ lang/Anchor
touch .env
```

Add the following configuration:

```env
# Server Configuration
PORT=8080

# MongoDB Configuration
MONGO_URI=mongodb://localhost:27017/?replicaSet=rs0
MONGO_DB_NAME=anchor_db

# Firebase Configuration
FIREBASE_CREDENTIALS_PATH=/path/to/your/serviceAccountKey.json
FIREBASE_PROJECT_ID=your-firebase-project-id

# JWT Configuration
JWT_SECRET=your-secret-key-change-in-production
JWT_EXPIRE=24
```

**Replace**:
- `/path/to/your/serviceAccountKey.json` → Path to your Firebase service account key
- `your-firebase-project-id` → Your actual Firebase project ID

### Step 4: Create MongoDB Indexes (Optional but Recommended)

```bash
mongosh anchor_db
```

```javascript
// Text indexes for search
db.anchors.createIndex({ title: "text", description: "text", tags: "text" })
db.anchor_content.createIndex({ content: "text", title: "text" })

// Performance indexes
db.anchors.createIndex({ userId: 1, createdAt: -1 })
db.anchors.createIndex({ category: 1, lastContentAddedAt: -1 })
db.anchors.createIndex({ trendingScore: -1 })
db.anchors.createIndex({ isPublic: 1, isDeleted: 1 })

db.anchor_content.createIndex({ anchorId: 1, section: 1, order: 1 })
db.anchor_content.createIndex({ anchorId: 1, isDeleted: 1 })

db.comments.createIndex({ anchorId: 1, createdAt: -1 })
db.comments.createIndex({ parentCommentId: 1 })

db.likes.createIndex({ userId: 1, anchorId: 1 }, { unique: true })
db.comment_likes.createIndex({ userId: 1, commentId: 1 }, { unique: true })

db.anchor_follows.createIndex({ userId: 1, anchorId: 1 }, { unique: true })
db.follows.createIndex({ followerId: 1, followedUserId: 1 }, { unique: true })

db.notifications.createIndex({ userId: 1, createdAt: -1 })
db.notifications.createIndex({ userId: 1, isRead: 1 })

db.collections.createIndex({ userId: 1, createdAt: -1 })

db.users.createIndex({ firebaseUid: 1 }, { unique: true })
db.users.createIndex({ email: 1 }, { unique: true, sparse: true })
```

### Step 5: Build the Application

```bash
go build -o api ./cmd/api
```

### Step 6: Run the Application

```bash
./api
```

You should see:
```
Starting server on port 8080
```

---

## Verification

### Test Health Endpoint

```bash
curl http://localhost:8080/api/v1/health
```

Expected response:
```json
{"status":"ok"}
```

### Test with Firebase Token

1. **Get a Firebase ID token** from your frontend or using Firebase Admin SDK
2. **Test authenticated endpoint**:

```bash
export TOKEN="your-firebase-id-token"

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/users/me
```

---

## Common Issues & Solutions

### Issue 1: "Transaction failed" errors

**Cause**: MongoDB not running in replica set mode

**Solution**:
```bash
mongosh
> rs.status()  # Should show replica set info, not an error
```

If not initialized:
```bash
> rs.initiate()
```

### Issue 2: "Firebase Auth not setup" warning

**Cause**: Invalid Firebase credentials path or project ID

**Solution**:
- Verify `.env` file has correct paths
- Ensure service account JSON file exists and is readable
- Check Firebase project ID matches your Firebase console

### Issue 3: Port 8080 already in use

**Solution**:
```bash
# Change PORT in .env file
PORT=3000
```

### Issue 4: MongoDB connection refused

**Solution**:
```bash
# Check if MongoDB is running
brew services list | grep mongodb

# Start if not running
brew services start mongodb-community@7.0
```

---

## Development Workflow

### Running in Development Mode

```bash
# Install air for hot reload (optional)
go install github.com/cosmtrek/air@latest

# Run with hot reload
air
```

### Running Tests (when implemented)

```bash
go test ./...
```

### Building for Production

```bash
# Build optimized binary
CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o api ./cmd/api
```

---

## Environment Variables Reference

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `PORT` | Yes | Server port | `8080` |
| `MONGO_URI` | Yes | MongoDB connection string (must include replica set) | `mongodb://localhost:27017/?replicaSet=rs0` |
| `MONGO_DB_NAME` | Yes | Database name | `anchor_db` |
| `FIREBASE_CREDENTIALS_PATH` | Yes | Path to Firebase service account JSON | `/path/to/key.json` |
| `FIREBASE_PROJECT_ID` | Yes | Firebase project ID | `my-project-123` |
| `JWT_SECRET` | Yes | Secret key for signing tokens | `some-secret-key` |
| `JWT_EXPIRE` | Yes | Token expiration in hours | `24` |

---

## API Endpoints Overview

Once running, the following endpoints are available:

### Public Endpoints
- `GET /api/v1/health` - Health check
- `GET /api/v1/anchors/:id` - Get anchor details
- `GET /api/v1/anchors/search?q=query` - Search anchors
- `GET /api/v1/explore/trending` - Trending anchors
- `GET /api/v1/explore/category/:category` - Category discovery

### Authenticated Endpoints (require `Authorization: Bearer <token>`)
- **Users**: `/api/v1/users/me`, `/api/v1/users/:id/follow`
- **Anchors**: `/api/v1/anchors` (POST, PATCH, DELETE)
- **Content**: `/api/v1/anchors/:id/content`
- **Comments**: `/api/v1/anchors/:id/comments`
- **Social**: `/api/v1/anchors/:id/follow`, `/api/v1/anchors/:id/clone`
- **Feed**: `/api/v1/feed`
- **Collections**: `/api/v1/collections`
- **Notifications**: `/api/v1/notifications`

Full API documentation available in [walkthrough.md](file:///Users/asif/.gemini/antigravity/brain/568ec0ec-1938-43fb-8eec-6250488d7450/walkthrough.md)

---

## Next Steps

1. ✅ Complete setup above
2. 🧪 Test endpoints with Postman or curl
3. 🔧 Configure MongoDB indexes for production
4. 📱 Connect your frontend application
5. 🚀 Deploy to production (see deployment guide)

---

## Support

For issues or questions:
- Check MongoDB replica set status: `mongosh` → `rs.status()`
- Verify Firebase credentials are valid
- Review application logs for detailed error messages
- Ensure all environment variables are set correctly
