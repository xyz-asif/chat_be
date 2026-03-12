package notifications

import (
	"context"
	"log"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// FirebaseFCM implements FCMSender using Firebase Admin SDK
type FirebaseFCM struct {
	client *messaging.Client
}

// NewFirebaseFCM creates an FCM sender. Returns nil (not an error) if
// credentials are missing — the notification service handles nil gracefully
// by skipping push notifications.
func NewFirebaseFCM(credPath, projectID string) *FirebaseFCM {
	if credPath == "" && projectID == "" {
		log.Println("FCM: No credentials configured, push notifications disabled")
		return nil
	}

	ctx := context.Background()
	var opts []option.ClientOption
	if credPath != "" {
		opts = append(opts, option.WithCredentialsFile(credPath))
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, opts...)
	if err != nil {
		log.Printf("FCM: Failed to init Firebase app: %v", err)
		return nil
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		log.Printf("FCM: Failed to init messaging client: %v", err)
		return nil
	}

	log.Println("FCM: Push notifications enabled")
	return &FirebaseFCM{client: client}
}

func (f *FirebaseFCM) SendPush(ctx context.Context, tokens []string, title, body string, data map[string]string) ([]string, error) {
	if f == nil || len(tokens) == 0 {
		return nil, nil
	}

	message := &messaging.MulticastMessage{
		Tokens: tokens,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
			Notification: &messaging.AndroidNotification{
				ChannelID: "high_importance_channel", // matches your Flutter channel
				Sound:     "default",
			},
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound:            "default",
					MutableContent:   true,
					ContentAvailable: true,
				},
			},
		},
	}

	resp, err := f.client.SendEachForMulticast(ctx, message)
	if err != nil {
		return nil, err
	}

	var staleTokens []string

	// Log failures (stale tokens, etc.) but don't fail the notification
	if resp.FailureCount > 0 {
		for i, r := range resp.Responses {
			if r.Error != nil {
				// Check if token is invalid/unregistered
				if messaging.IsUnregistered(r.Error) ||
					strings.Contains(r.Error.Error(), "not found") {
					staleTokens = append(staleTokens, tokens[i])
				}
				log.Printf("FCM: Token %d failed: %v", i, r.Error)
			}
		}
	}

	return staleTokens, nil
}
