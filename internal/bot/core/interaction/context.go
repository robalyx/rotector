//nolint:containedctx // -
package interaction

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"go.uber.org/zap"
)

// While the Go team generally recommends against storing context in structs,
// this implementation is intentionally designed as a request-scoped wrapper that:
//  1. Has clear context ownership and lifetime (tied to a single Discord interaction)
//  2. Bundles related request data (event, session, manager) without embedding
//  3. Provides a clean API for interaction handling without exposing raw context methods
//  4. Follows context propagation patterns through explicit WithContext/WithValue methods
//
// We still properly respect context immutability by creating new instances when the context
// changes which aligns with Go's context design philosophy.

// Context wraps the standard context.Context and adds bot functionality.
type Context struct {
	ctx       context.Context
	event     CommonEvent
	session   *session.Session
	manager   *Manager
	responded bool
}

// New creates a new Context with the given parent context and bot data.
func New(ctx context.Context, event CommonEvent, session *session.Session, manager *Manager) *Context {
	return &Context{
		ctx:     ctx,
		event:   event,
		session: session,
		manager: manager,
	}
}

// Context returns the standard context.Context associated with this context.
func (c *Context) Context() context.Context {
	return c.ctx
}

// Event returns the Discord event associated with this context.
func (c *Context) Event() CommonEvent {
	return c.event
}

// WithContext returns a copy of the Context with the given context.Context as parent.
func (c *Context) WithContext(ctx context.Context) *Context {
	return &Context{
		ctx:       ctx,
		event:     c.event,
		session:   c.session,
		manager:   c.manager,
		responded: c.responded,
	}
}

// WithValue returns a copy of the Context with the given key-value pair added.
func (c *Context) WithValue(key, val any) *Context {
	return &Context{
		ctx:       context.WithValue(c.ctx, key, val),
		event:     c.event,
		session:   c.session,
		manager:   c.manager,
		responded: c.responded,
	}
}

// Error updates the interaction response with an error message.
func (c *Context) Error(content string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		ClearFiles().
		ClearComponents().
		RetainAttachments().
		AddComponents(utils.CreateTimestampedTextDisplay("Fatal error: " + content)).
		AddFlags(discord.MessageFlagIsComponentsV2).
		Build()

	_, _ = c.event.Client().Rest.UpdateInteractionResponse(c.event.ApplicationID(), c.event.Token(), messageUpdate)
	c.manager.sessionManager.CloseSession(c.ctx, c.session, uint64(c.event.User().ID), uint64(c.event.Message().ID))
	c.responded = true

	c.manager.logger.Debug("Fatal error",
		zap.Uint64("user_id", uint64(c.event.User().ID)),
		zap.String("content", content),
	)
}

// Clear updates the interaction response with a message and clears the files and components.
func (c *Context) Clear(content string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		ClearFiles().
		ClearComponents().
		RetainAttachments().
		AddComponents(utils.CreateTimestampedTextDisplay(content)).
		AddFlags(discord.MessageFlagIsComponentsV2).
		Build()

	_, _ = c.event.Client().Rest.UpdateInteractionResponse(
		c.event.ApplicationID(),
		c.event.Token(),
		messageUpdate,
	)
	c.responded = true
}

// ClearComponents updates the interaction response with a message and clears the components.
func (c *Context) ClearComponents(content string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		ClearComponents().
		AddComponents(utils.CreateTimestampedTextDisplay(content)).
		AddFlags(discord.MessageFlagIsComponentsV2).
		Build()

	_, _ = c.event.Client().Rest.UpdateInteractionResponse(
		c.event.ApplicationID(),
		c.event.Token(),
		messageUpdate,
	)
	c.responded = true
}

// RespondWithFiles updates the interaction response with a message and file attachments.
func (c *Context) RespondWithFiles(content string, files ...*discord.File) {
	page := c.manager.GetPage(session.CurrentPage.Get(c.session))
	c.manager.Display(c.event, c.session, page, content, files...)
	c.responded = true
}

// NavigateBack navigates back to the previous page in the history.
func (c *Context) NavigateBack(content string) {
	previousPages := session.PreviousPages.Get(c.session)

	if len(previousPages) > 0 {
		// Get the last page from history
		lastIdx := len(previousPages) - 1
		previousPage := previousPages[lastIdx]

		// Call the cleanup handler for the current page
		currentPage := c.manager.GetPage(session.CurrentPage.Get(c.session))
		if currentPage.CleanupHandlerFunc != nil {
			currentPage.CleanupHandlerFunc(c.session)
		}

		// Navigate to the previous page
		c.manager.Show(c.event, c.session, previousPage, content)
		c.session.Touch(context.Background())
	} else {
		c.Cancel(content)
	}

	c.responded = true
}

// Cancel stops the loading of the new page in the session and refreshes with a new message.
func (c *Context) Cancel(content string) {
	page := c.manager.GetPage(session.CurrentPage.Get(c.session))
	c.manager.Display(c.event, c.session, page, content)
	c.responded = true
}

// Reload reloads the page being processed with a new message.
func (c *Context) Reload(content string) {
	pageName := session.CurrentPage.Get(c.session)
	c.manager.Show(c.event, c.session, pageName, content)
	c.session.Touch(context.Background())
	c.responded = true
}

// Show updates the Discord message with new content and components for the target page.
func (c *Context) Show(pageName, content string) {
	c.manager.Show(c.event, c.session, pageName, content)
	c.session.Touch(context.Background())
	c.responded = true
}

// UpdatePage updates the session with a new page.
// This does not refresh or reload the page, it just updates the page in the session.
func (c *Context) UpdatePage(pageName string) {
	page := c.manager.GetPage(pageName)
	c.manager.UpdatePage(c.session, page)
	c.responded = true
}

// Modal updates the interaction response with a modal.
func (c *Context) Modal(modal *discord.ModalCreateBuilder) {
	if err := c.event.Modal(modal.Build()); err != nil {
		c.manager.logger.Error("Failed to create modal",
			zap.Error(err),
			zap.String("custom_id", modal.CustomID),
			zap.String("title", modal.Title),
		)

		return
	}

	c.session.Touch(context.Background())
	c.responded = true

	// WORKAROUND:
	// This fixes a problem with Discord's select menu behavior. When a modal is opened,
	// the selected option in the dropdown remains selected even if the user exits the modal.
	// This prevents the user from selecting the same option again since Discord doesn't
	// automatically reset the selection. By updating the message after opening a modal,
	// we force the select menu to reset to its default state.
	page := c.manager.GetPage(session.CurrentPage.Get(c.session))
	if !page.DisableSelectMenuReset {
		_, err := c.event.Client().Rest.UpdateInteractionResponse(
			c.event.ApplicationID(),
			c.event.Token(),
			page.Message(c.session).Build(),
		)
		if err != nil {
			c.Error("Failed to implement workaround for Discord's select menu behavior. Please report this issue.")
		}
	}
}
