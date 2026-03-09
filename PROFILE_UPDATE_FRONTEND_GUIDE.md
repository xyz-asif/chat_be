# Profile Update Broadcasting - Implementation Guide

## Backend Changes Summary

### 1. Users Service Updated (`internal/features/users/service.go`)

The service now:
- Accepts HubSender, ConnectionRepository, and ChatRepository dependencies
- Broadcasts `profile_updated` WebSocket event after successful profile update
- Sends updates to all friends (connections) and chat room participants
- Runs asynchronously (doesn't block API response)

### 2. Required main.go Changes

**Replace your current users service initialization:**

```go
// OLD CODE (remove this):
userService := users.NewService(userRepo)

// NEW CODE (use this):
userService := users.NewService(userRepo, hub, connRepo, chatRepo)
```

**Complete wiring example:**

```go
// In your main.go where you initialize services:

// 1. Initialize repositories
userRepo := users.NewRepository(db)
connRepo := connections.NewRepository(db)
chatRepo := chat.NewRepository(db)

// 2. Initialize hub
hub := chat.NewHub()
go hub.Run()

// 3. Initialize services with dependencies
userService := users.NewService(userRepo, hub, connRepo, chatRepo)
connService := connections.NewService(connRepo)
chatService := chat.NewService(chatRepo, userRepo, connRepo, hub)

// 4. Initialize handlers
userHandler := users.NewHandler(userService)
connHandler := connections.NewHandler(connService)
chatHandler := chat.NewHandler(chatService)
```

---

## Frontend Implementation Guide

### WebSocket Event Handler

Add this to your WebSocket message handler:

```javascript
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  
  switch (msg.type) {
    case 'profile_updated':
      handleProfileUpdate(msg.payload);
      break;
    // ... your other handlers
  }
};

function handleProfileUpdate(payload) {
  const { userId, displayName, photoURL, bio } = payload;
  
  // Update user in your store/cache
  updateUserInStore({
    id: userId,
    displayName,
    photoURL,
    bio
  });
  
  // Update chat list if this user appears there
  updateChatListUser(userId, { displayName, photoURL });
  
  // Update active conversation if open with this user
  updateActiveConversationUser(userId, { displayName, photoURL });
}
```

### Profile Update API Call

```javascript
// Update profile (name, bio, photoURL)
async function updateProfile(updates) {
  const response = await fetch('/api/v1/users/me', {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`
    },
    body: JSON.stringify(updates)
  });
  
  if (!response.ok) {
    throw new Error('Failed to update profile');
  }
  
  const data = await response.json();
  
  // Update local user state immediately
  updateCurrentUser(data.data);
  
  return data.data;
}

// Usage examples:

// Update display name
await updateProfile({ displayName: 'New Name' });

// Update bio
await updateProfile({ bio: 'New bio text' });

// Update profile photo URL (after uploading to storage)
await updateProfile({ photoURL: 'https://example.com/new-photo.jpg' });

// Update multiple fields at once
await updateProfile({
  displayName: 'New Name',
  bio: 'New bio',
  photoURL: 'https://example.com/photo.jpg'
});
```

### State Management Updates

**Redux/Zustand/Context example:**

```javascript
// In your user store
const useUserStore = create((set, get) => ({
  users: {},
  currentUser: null,
  
  // Update any user by ID
  updateUser: (userId, updates) => set((state) => ({
    users: {
      ...state.users,
      [userId]: { ...state.users[userId], ...updates }
    },
    currentUser: state.currentUser?.id === userId 
      ? { ...state.currentUser, ...updates }
      : state.currentUser
  })),
  
  // Set current user
  setCurrentUser: (user) => set({ currentUser: user })
}));

// Usage in components
function handleProfileUpdate(payload) {
  useUserStore.getState().updateUser(payload.userId, {
    displayName: payload.displayName,
    photoURL: payload.photoURL,
    bio: payload.bio
  });
}
```

### Chat List Updates

```javascript
// Update user info in chat list
function updateChatListUser(userId, updates) {
  const chatList = useChatStore.getState().rooms;
  
  chatList.forEach(room => {
    const participant = room.participants.find(p => p.id === userId);
    if (participant) {
      Object.assign(participant, updates);
    }
  });
  
  // Trigger re-render
  useChatStore.getState().setRooms([...chatList]);
}
```

### Message Updates

```javascript
// Update sender info in messages
function updateMessageSender(userId, updates) {
  const messages = useChatStore.getState().messages;
  
  messages.forEach(msg => {
    if (msg.senderId === userId) {
      msg.senderName = updates.displayName;
      msg.senderPhotoURL = updates.photoURL;
    }
  });
  
  useChatStore.getState().setMessages([...messages]);
}
```

---

## Edge Cases Handled

1. **No connections/friends**: Broadcast skipped gracefully
2. **No chat rooms**: Broadcast skipped gracefully
3. **Hub not initialized**: Broadcast skipped (nil check)
4. **Repository errors**: Logged but don't affect API response
5. **Empty updates**: API returns error "no valid fields to update"
6. **Invalid user ID**: Returns validation error
7. **User has no active chats**: No broadcast sent (expected)
8. **Large number of recipients**: Uses map for deduplication

---

## WebSocket Event Payload

```json
{
  "type": "profile_updated",
  "payload": {
    "userId": "65a1b2c3d4e5f6g7h8i9j0k1",
    "displayName": "New Name",
    "photoURL": "https://example.com/photo.jpg",
    "bio": "New bio text"
  }
}
```

**Notes:**
- Only changed fields are included in the payload
- `displayName` and `photoURL` are always included for consistency
- Recipients are deduplicated (user won't receive multiple notifications)

---

## Profile Photo Upload Flow

1. **Upload photo to storage** (Cloudinary/Firebase/etc)
2. **Get photo URL**
3. **Update profile**:
   ```javascript
   await updateProfile({ photoURL: uploadedPhotoUrl });
   ```
4. **Friends receive WebSocket event** with new photoURL
5. **UI updates instantly** across all friends' screens

---

## Testing Checklist

- [ ] Update profile name → see change in chat list for friend
- [ ] Update profile photo → see new photo in active chat
- [ ] Update bio → bio updates in profile view for others
- [ ] Update multiple fields → all fields update correctly
- [ ] User with no friends → API succeeds, no broadcast
- [ ] User with no chat rooms → API succeeds, no broadcast
- [ ] WebSocket disconnected during update → API succeeds, broadcast to online users only
