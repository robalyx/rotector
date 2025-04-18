package ai

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// ApplicationJSON is the MIME type for JSON content.
	ApplicationJSON = "application/json"
	// TextPlain is the MIME type for plain text content.
	TextPlain = "text/plain"

	// SharedUserContentGuidelines contains the guidelines for analyzing user content for predatory behavior.
	SharedUserContentGuidelines = `Instruction:
Pay close attention to the following indicators of predatory behavior in descriptions:

1. Grooming Tactics:
- ANY attempt at building trust through friendly/caring language
- ANY attempt to establish private communication
- ANY attempt to move communication off-platform
- ANY use of manipulation or guilt tactics
- ANY promises of friendship/relationship
- ANY use of excessive compliments or flattery
- ANY creation of secrecy or exclusivity
- ANY attempt to isolate targets from others

2. Exploitation Indicators:
- ANY seeking of private interactions
- ANY offering or requesting of inappropriate content
- ANY inappropriate use of authority positions
- ANY targeting of specific age groups/genders
- ANY creation of power imbalances
- ANY attempt to normalize inappropriate behavior
- ANY use of coded language for inappropriate acts

3. Suspicious Communication Patterns:
- ANY coded language implying inappropriate activities
- ANY leading phrases implying secrecy
- ANY studio mentions or invites (ZERO EXCEPTIONS)
- ANY game or chat references that could enable private interactions
- ANY condo/con references
- ANY "exclusive" group invitations
- ANY private server invitations
- ANY age-restricted invitations
- ANY suspicious direct messaging demands
- ANY use of slang with inappropriate context ("down", "dtf", etc.)
- ANY claims of following TOS/rules to avoid detection
- ANY roleplay requests or themes (ZERO EXCEPTIONS)
- ANY mentions of "trading" or variations which commonly refer to CSAM
- ANY mentions of "cheese" or "pizza" which are known code words for CSAM
- ANY use of "yk" or "you know" in suspicious contexts

4. Inappropriate Content:
- ANY sexual content or innuendo
- ANY sexual solicitation
- ANY erotic roleplay (ERP)
- ANY age-inappropriate dating content
- ANY non-consensual references
- ANY ownership/dominance references
- ANY adult community references
- ANY suggestive size references
- ANY inappropriate trading
- ANY degradation terms
- ANY breeding/heat themes
- ANY references to bulls or cuckolding content
- ANY raceplay or racial sexual stereotypes
- ANY fart/gas/smell references
- ANY fetish references

5. Technical Evasion:
- ANY bypassed inappropriate terms
- ANY Caesar cipher (ROT13 and other rotations)
- ANY deliberately misspelled inappropriate terms
- ANY references to "futa" or bypasses like "fmta", "fmt", etc.
- ANY references to "les" or similar LGBT+ terms used inappropriately

7. Social Engineering:
- ANY friend requests with inappropriate context
- ANY terms of endearment used predatorily (mommy, daddy, kitten, etc.)
- ANY "special" or "exclusive" game pass offers
- ANY promises of rewards for buying passes
- ANY promises or offers of "fun"
- ANY references to "blue user" or "blue app"
- ANY directing to other profiles/accounts
- ANY use of innocent-sounding terms as code words
- ANY mentions of literacy or writing ability

Instruction: Pay close attention to usernames and display names that suggest predatory intentions, such as:
- ANY names exploiting authority or mentor roles
- ANY names suggesting sexual availability or soliciting inappropriate interactions 
- ANY names using pet names or diminutives suggestively (kitty, kitten, etc.)
- ANY names targeting minors or specific genders inappropriately
- ANY names using coded language or suggestive terms related to inappropriate acts
- ANY names hinting at exploitation or predatory activities
- ANY references to adult content platforms or services
- ANY deliberately misspelled inappropriate terms
- ANY mentions of bull, fart, gas, smell, etc.

Instruction: You MUST flag ANY roleplay requests and themes because:
1. ANY roleplay can be used to groom or exploit children
2. ANY roleplay creates opportunities for predators to build trust
3. Even seemingly innocent roleplay can escalate to inappropriate content
4. There is no way to ensure roleplay remains appropriate

Instruction: You MUST consider ANY sexual content or references on Roblox as predatory behavior due to:
1. Roblox is primarily a children's platform
2. ANY sexual content in spaces meant for children is inherently predatory
3. ANY sexual usernames/content expose minors to inappropriate material
4. There is no legitimate reason for sexual content on a children's platform`
)

var (
	// ErrModelResponse indicates the model returned no usable response.
	ErrModelResponse = errors.New("model response error")
	// ErrJSONProcessing indicates a JSON processing error.
	ErrJSONProcessing = errors.New("JSON processing error")
)

// ContextType represents the type of message context.
type ContextType string

const (
	ContextTypeUser  ContextType = "user"
	ContextTypeGroup ContextType = "group"
	ContextTypeHuman ContextType = "human"
	ContextTypeAI    ContextType = "ai"
)

// ContextMap is a map of context types to their corresponding contexts.
type ContextMap map[ContextType][]Context

// ChatContext is a slice of ordered contexts.
type ChatContext []Context

// Context represents a single context entry in the chat history.
type Context struct {
	Type    ContextType
	Content string
	Model   string
}

// FormatForAI formats the context for inclusion in AI messages.
func (cc ChatContext) FormatForAI() string {
	if len(cc) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Previous conversation:\n")

	for _, ctx := range cc {
		content := ctx.Content

		// For AI responses, remove thinking blocks
		if ctx.Type == ContextTypeAI {
			for {
				startIdx := strings.Index(content, "<think>")
				if startIdx == -1 {
					break
				}
				endIdx := strings.Index(content[startIdx:], "</think>")
				if endIdx == -1 {
					break
				}
				endIdx += startIdx + 8 // Add length of "</think>"
				content = content[:startIdx] + content[endIdx:]
			}
			content = strings.TrimSpace(content)
		}

		switch ctx.Type {
		case ContextTypeHuman:
			b.WriteString(fmt.Sprintf("<previous user>%s</previous>\n", content))
		case ContextTypeAI:
			if content != "" {
				b.WriteString(fmt.Sprintf("<previous assistant>%s</previous>\n", content))
			}
		case ContextTypeUser, ContextTypeGroup:
			b.WriteString(fmt.Sprintf("<context %s>\n%s\n</context>\n", strings.ToLower(string(ctx.Type)), content))
		}
	}

	return strings.TrimSpace(b.String())
}

// GroupByType converts a ChatContext slice into a ContextMap.
func (cc ChatContext) GroupByType() ContextMap {
	grouped := make(ContextMap)
	for _, ctx := range cc {
		grouped[ctx.Type] = append(grouped[ctx.Type], ctx)
	}
	return grouped
}
