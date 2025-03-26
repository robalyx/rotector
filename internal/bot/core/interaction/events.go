package interaction

import (
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// ModalData provides access to modal functionality.
type ModalData interface {
	// Text returns the value of a text input component with the given custom ID.
	Text(customID string) string
	// OptText returns the value of a text input component and whether it exists.
	OptText(customID string) (string, bool)
}

// CommonEvent extracts shared functionality from different Discord event types.
// This allows pagination to work with any interaction event without type checking.
type CommonEvent interface {
	// Client returns the Discord client instance handling this event.
	Client() bot.Client

	// ApplicationID returns the bot's application ID for API requests.
	ApplicationID() snowflake.ID

	// AppPermissions returns the permissions of the bot in the guild.
	AppPermissions() *discord.Permissions

	// Token returns the interaction token for responding to the event.
	Token() string

	// User returns the Discord user who triggered this event.
	User() discord.User

	// GuildID returns the ID of the guild where the event occurred.
	// Returns nil for direct message events.
	GuildID() *snowflake.ID

	// Member returns the member who triggered this event.
	Member() *discord.ResolvedMember

	// Modal shows a modal dialog to the user.
	// For non-ComponentInteractionCreate events, this will panic.
	Modal(discord.ModalCreate) error

	// CustomID returns the custom ID of the interaction.
	// Returns empty string for interactions without custom IDs.
	CustomID() string

	// ModalData returns modal data if this is a modal submit event.
	// Returns nil for other event types.
	ModalData() ModalData

	// SetMessage sets the message that triggered this event.
	SetMessage(message *discord.Message)

	// Message returns the message that triggered this event.
	// Returns nil for events that are not associated with a message.
	Message() *discord.Message
}

// panicModal is a helper function that implements Modal() for events that don't support it.
func panicModal(_ *discord.ModalCreate) error {
	panic("Modal() is only supported for ComponentInteractionCreate events")
}

// ApplicationCommandEvent wraps ApplicationCommandInteractionCreate.
type ApplicationCommandEvent struct {
	*events.ApplicationCommandInteractionCreate
	message *discord.Message
}

func (e *ApplicationCommandEvent) Modal(discord.ModalCreate) error {
	return panicModal(nil)
}

func (e *ApplicationCommandEvent) CustomID() string {
	return "" // Application commands don't have custom IDs
}

func (e *ApplicationCommandEvent) ModalData() ModalData {
	return nil
}

func (e *ApplicationCommandEvent) SetMessage(message *discord.Message) {
	e.message = message
}

func (e *ApplicationCommandEvent) Message() *discord.Message {
	if e.message != nil {
		return e.message
	}
	return nil
}

// ComponentEvent wraps ComponentInteractionCreate.
type ComponentEvent struct {
	*events.ComponentInteractionCreate
	message *discord.Message
}

func (e *ComponentEvent) Modal(modal discord.ModalCreate) error {
	return e.ComponentInteractionCreate.Modal(modal)
}

func (e *ComponentEvent) CustomID() string {
	return e.Data.CustomID()
}

func (e *ComponentEvent) ModalData() ModalData {
	return nil
}

func (e *ComponentEvent) SetMessage(message *discord.Message) {
	e.message = message
}

func (e *ComponentEvent) Message() *discord.Message {
	if e.message != nil {
		return e.message
	}
	return nil
}

// ModalSubmitEvent wraps ModalSubmitInteractionCreate.
type ModalSubmitEvent struct {
	*events.ModalSubmitInteractionCreate
	message *discord.Message
}

func (e *ModalSubmitEvent) Modal(discord.ModalCreate) error {
	return panicModal(nil)
}

func (e *ModalSubmitEvent) CustomID() string {
	return e.Data.CustomID
}

func (e *ModalSubmitEvent) ModalData() ModalData {
	return e.Data
}

func (e *ModalSubmitEvent) SetMessage(message *discord.Message) {
	e.message = message
}

func (e *ModalSubmitEvent) Message() *discord.Message {
	if e.message != nil {
		return e.message
	}
	return nil
}

// WrapEvent wraps Discord events in our local event types.
func WrapEvent(event any, message *discord.Message) CommonEvent {
	switch e := event.(type) {
	case *events.ApplicationCommandInteractionCreate:
		return &ApplicationCommandEvent{e, message}
	case *events.ComponentInteractionCreate:
		return &ComponentEvent{e, message}
	case *events.ModalSubmitInteractionCreate:
		return &ModalSubmitEvent{e, message}
	default:
		panic("unknown event type")
	}
}

// These type assertions ensure that all event types properly implement
// the CommonEvent interface at compile time.
var (
	_ CommonEvent = (*ApplicationCommandEvent)(nil)
	_ CommonEvent = (*ComponentEvent)(nil)
	_ CommonEvent = (*ModalSubmitEvent)(nil)
)
