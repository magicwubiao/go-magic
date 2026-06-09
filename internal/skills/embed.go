package skills

import (
	"embed"
)

// BuiltinSkillsFS embeds the built-in skills directory into the binary
// This ensures skills are available regardless of the working directory
//
//go:embed builtin
var BuiltinSkillsFS embed.FS

// BuiltinDirName is the root directory name for embedded builtin skills
const BuiltinDirName = "builtin"
