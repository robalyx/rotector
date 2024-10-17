package reviewer

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/common/utils"
)

// ResponseBuilder is a helper struct for building responses.
type ResponseBuilder struct {
	content    string
	embeds     []discord.Embed
	components []discord.ContainerComponent
	files      []*discord.File
	flags      discord.MessageFlags
}

// NewResponseBuilder creates a new ResponseBuilder.
func NewResponseBuilder() *ResponseBuilder {
	return &ResponseBuilder{}
}

// SetContent sets the content of the response.
func (rb *ResponseBuilder) SetContent(content string) *ResponseBuilder {
	rb.content = utils.GetTimestampedSubtext(content)
	return rb
}

// SetEmbeds sets the embeds of the response.
func (rb *ResponseBuilder) SetEmbeds(embeds ...discord.Embed) *ResponseBuilder {
	rb.embeds = embeds
	return rb
}

// SetComponents sets the components of the response.
func (rb *ResponseBuilder) SetComponents(components ...discord.ContainerComponent) *ResponseBuilder {
	rb.components = components
	return rb
}

// AddFile adds a file to the response.
func (rb *ResponseBuilder) AddFile(file *discord.File) *ResponseBuilder {
	rb.files = append(rb.files, file)
	return rb
}

// SetEphemeral sets the ephemeral flag of the response.
func (rb *ResponseBuilder) SetEphemeral(ephemeral bool) *ResponseBuilder {
	if ephemeral {
		rb.flags |= discord.MessageFlagEphemeral
	} else {
		rb.flags &^= discord.MessageFlagEphemeral
	}
	return rb
}

// Build builds the discord.MessageUpdate.
func (rb *ResponseBuilder) Build() discord.MessageUpdate {
	return discord.NewMessageUpdateBuilder().
		SetContent(rb.content).
		SetEmbeds(rb.embeds...).
		SetContainerComponents(rb.components...).
		AddFiles(rb.files...).
		SetFlags(rb.flags).
		RetainAttachments().
		Build()
}
