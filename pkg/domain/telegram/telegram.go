// Package telegram models the commands arriving from Telegram and the port
// the infrastructure layer uses to listen for them.
package telegram

import "context"

// Command is an instruction sent by a Telegram account to the bot.
type Command struct {
	// ChatID is the conversation the command arrived in.
	ChatID int64
	// UserID is the numeric Telegram account identifier.
	UserID int64
	// Username is the handle of the sending account, without the @.
	Username string
	// FirstName is the display name of the sending account.
	FirstName string
	// Command is the verb, without the leading slash, lower cased.
	Command string
	// Payload is the argument that followed the verb.
	Payload string
}

// Gateway listens for commands sent to the bot.
type Gateway interface {
	// Listen starts consuming updates and returns the stream of commands.
	// The stream is closed when the context is cancelled.
	Listen(ctx context.Context) (<-chan Command, error)
}
