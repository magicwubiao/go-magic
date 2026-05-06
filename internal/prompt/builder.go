package prompt

import (
	"fmt"
	"strings"
)

type Builder struct {
	systemPrompt string
	skillsCtx    string
	memoryCtx    string
	toolsSchema  string
}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) SetPersona(persona string) *Builder {
	b.systemPrompt = persona
	return b
}

func (b *Builder) SetSkillsContext(ctx string) *Builder {
	b.skillsCtx = ctx
	return b
}

func (b *Builder) SetMemoryContext(ctx string) *Builder {
	b.memoryCtx = ctx
	return b
}

func (b *Builder) SetToolsSchema(schema string) *Builder {
	b.toolsSchema = schema
	return b
}

func (b *Builder) Build() string {
	var sb strings.Builder

	// Base system prompt
	sb.WriteString("You are magic Agent, a helpful AI assistant.\n")
	sb.WriteString("You can use tools to help users accomplish tasks.\n\n")

	// Persona
	if b.systemPrompt != "" {
		sb.WriteString("## Persona\n")
		sb.WriteString(b.systemPrompt + "\n\n")
	}

	// Memory context
	if b.memoryCtx != "" {
		sb.WriteString("## Memory\n")
		sb.WriteString(b.memoryCtx + "\n\n")
	}

	// Skills context
	if b.skillsCtx != "" {
		sb.WriteString("## Skills\n")
		sb.WriteString(b.skillsCtx + "\n\n")
	}

	// Tools
	if b.toolsSchema != "" {
		sb.WriteString("## Available Tools\n")
		sb.WriteString("You have access to the following tools:\n")
		sb.WriteString(b.toolsSchema + "\n")
		sb.WriteString("\nTo use a tool, respond with a tool call.\n")
	}

	sb.WriteString("\nRemember to be helpful, harmless, and honest.")

	return sb.String()
}

func DefaultPersona() string {
	return `You are magic, a self-improving AI agent built by Nous Research.
Your goal is to help users accomplish tasks efficiently and learn from interactions.
You can read/write files, execute commands, search the web, and more.
Always think step-by-step and use tools when needed.`
}

func FormatToolsSchema(tools []map[string]interface{}) string {
	var sb strings.Builder
	for _, tool := range tools {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", tool["name"], tool["description"]))
	}
	return sb.String()
}
