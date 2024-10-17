package builders

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/common/utils"
)

// Response is a helper struct for building responses.
type Response struct {
	content    string
	embeds     []discord.Embed
	components []discord.ContainerComponent
	files      []*discord.File
	flags      discord.MessageFlags
}

// NewResponse creates a new Response.
func NewResponse() *Response {
	return &Response{}
}

// SetContent sets the content of the response.
func (rb *Response) SetContent(content string) *Response {
	rb.content = utils.GetTimestampedSubtext(content)
	return rb
}

// SetEmbeds sets the embeds of the response.
func (rb *Response) SetEmbeds(embeds ...discord.Embed) *Response {
	rb.embeds = embeds
	return rb
}

// SetComponents sets the components of the response.
func (rb *Response) SetComponents(components ...discord.ContainerComponent) *Response {
	rb.components = components
	return rb
}

// AddFile adds a file to the response.
func (rb *Response) AddFile(file *discord.File) *Response {
	rb.files = append(rb.files, file)
	return rb
}

// SetEphemeral sets the ephemeral flag of the response.
func (rb *Response) SetEphemeral(ephemeral bool) *Response {
	if ephemeral {
		rb.flags |= discord.MessageFlagEphemeral
	} else {
		rb.flags &^= discord.MessageFlagEphemeral
	}
	return rb
}

// Build builds the discord.MessageUpdate.
func (rb *Response) Build() discord.MessageUpdate {
	return discord.NewMessageUpdateBuilder().
		SetContent(rb.content).
		SetEmbeds(rb.embeds...).
		SetContainerComponents(rb.components...).
		AddFiles(rb.files...).
		SetFlags(rb.flags).
		RetainAttachments().
		Build()
}
