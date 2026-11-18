// Package notification models outbound messages and the port used to deliver
// them.
package notification

import "context"

// Recipient identifies where a message is delivered.
type Recipient struct {
	// ChatID is the numeric Telegram chat identifier. It is zero when the
	// message must be delivered by username.
	ChatID int64
	// Username is the Telegram handle used when no chat identifier is known.
	Username string
}

// Message is a notification to be delivered.
type Message struct {
	// Text is the human readable body of the notification.
	Text string
}

// Notifier delivers notifications. Implementations live in the
// infrastructure layer.
type Notifier interface {
	// Send delivers a message to a recipient.
	Send(ctx context.Context, recipient Recipient, message Message) error
}
