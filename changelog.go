// Package townsquare holds files from the repository root that the program embeds.
package townsquare

import _ "embed"

// Changelog is CHANGELOG.md, so the app can show what changed after an update.
//
//go:embed CHANGELOG.md
var Changelog string

// Skill is the agent guide shipped with the CLI (`townsquare skills show`).
//
//go:embed skills/townsquare/SKILL.md
var Skill string
