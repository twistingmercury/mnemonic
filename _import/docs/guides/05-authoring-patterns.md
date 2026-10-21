# Authoring Patterns

This guide teaches you how to write patterns that work well with semantic search, follow Mnemonic conventions, and deliver value to your team.

## What is a Pattern?

A pattern is a Markdown document with YAML frontmatter that captures a piece of team knowledge: a coding convention, architectural decision, best practice, or technique. Patterns are loaded into Mnemonic through the REST API (`POST /v1/api/patterns`), semantically enriched (embedded), and served to AI coding assistants via MCP search. The frontmatter fields become fields of the create request and the Markdown body becomes its `content`.

When Claude Code or another AI assistant searches Mnemonic with a question, it finds patterns by semantic similarity — understanding the concepts you're asking about, not just keyword matching. Well-written patterns are discoverable, precise, and actionable.

## Pattern File Structure

Every pattern file has three parts:

1. **YAML Frontmatter** (metadata)
2. **H1 Title** (the pattern name as a heading)
3. **Body** (content sections, some decorated for indexing)

Here's the overall shape:

```markdown
---
[frontmatter fields here]
---

# [Pattern Title]

## Overview

[Overview section - not decorated, so not indexed]

[//]: pattern
## [Section 1]

[Section 1 content - indexed]

[//]: pattern
## [Section 2]

[Section 2 content - indexed]
```

## Frontmatter Schema

These are the rules the API enforces on create and update (`mnemonic-api` `internal/handlers/patterns`). A violation returns `400` with a field-level error; see [Pattern File Schema](../reference/pattern-schema.md) for the full table.

### Required Fields

| Field | Type | Format | Constraints |
| --- | --- | --- | --- |
| `name` | string | kebab-case | Machine identifier. Regex: `^[a-z][a-z0-9-]*$`. Max 128 characters. Must be unique (a duplicate returns `409 Conflict`). |
| `entity_type` | string | kebab-case | Category. Regex: `^[a-z][a-z0-9-]*$`. Max 100 characters. Free value, not a fixed list. Examples: `best-practice`, `cli-pattern`, `api-specification`, `architectural-decision`, `technique`. |
| `language` | string | enum | One of the supported languages (see below). |
| `domain` | string | enum | One of the supported domains (see below). |
| `content` | string | Markdown | The body of the file. Non-empty, at most 100,000 bytes. |

`description` is optional for the API but strongly recommended (and required by the mnemonic-patterns validator): free text, max 500 characters, used for keyword search and UI display. Write a complete sentence or short paragraph.

### Supported Language Values

These are the API defaults (`vocabulary.languages` in mnemonic-api's config); an operator can replace the list in the API's config file.

`agnostic`, `bash`, `c`, `cpp`, `csharp`, `cql`, `cypher`, `dart`, `delphi`, `docker`, `elixir`, `erlang`, `go`, `json`, `java`, `javascript`, `kotlin`, `lua`, `markdown`, `matlab`, `mql`, `objective-c`, `perl`, `php`, `plsql`, `powershell`, `python`, `r`, `react`, `ruby`, `rust`, `scala`, `shell`, `sql`, `swift`, `terraform`, `toml`, `tsql`, `typescript`, `visual-basic`, `yaml`, `zig`.

Use `agnostic` for patterns that apply across languages (e.g., code review practices, documentation standards).

### Supported Domain Values

Defaults from `vocabulary.domains`, also replaceable in the API's config file.

`api-design`, `backend`, `frontend`, `testing`, `devops`, `cli`, `data-design`, `documentation`, `data-access`, `security`, `shell-scripting`, `configuration`, `observability`, `source-management`.

Choose the primary domain where the pattern applies most directly. If your pattern spans multiple domains, pick the most specific one.

### Optional Fields

| Field | Type | Notes |
| --- | --- | --- |
| `description` | string | Max 500 characters. See above. |
| `tags` | array of string | At most 20. Used as an AND filter in search and included in each chunk's embedding text. Keep these specific and actionable (e.g., `error-handling`, `cobra-cli`, `integration-testing`). |
| `version` | string | Free-text label for the language, framework, or spec version. Examples: `Go 1.21+`, `React 18+`, `Postgres 14+`. Not a pattern revision number. |
| `related_patterns` | array of string | Names of related patterns. Stored and returned as given; Mnemonic does not use it to build graph relationships, which come from extracted concepts. |

### Agents Are Not Supported Yet

Earlier versions of this guide described an `agents` frontmatter field mapped to weighted "agent associations." No Mnemonic service implements this: the create request has no agent field, nothing is stored, and search has no agent filter or weighting. If your pattern files carry `agents:`, it is ignored by the API. Agent association is a possible future feature.

### Unknown Fields

You can add custom metadata fields (e.g., `author`, `created`, `status`) for your own tooling. The API ignores JSON fields it does not know, so they are not stored in Mnemonic.

## The `[//]: pattern` Decorator

The pattern decorator is the key mechanism that controls what gets indexed for semantic search.

### How It Works

Place `[//]: pattern` on its own line before a heading (any level: `#`, `##`, `###`, ...). Blank lines between the decorator and the heading are fine. The line must be exactly `[//]: pattern`, with no leading or trailing spaces and Unix (LF) line endings; anything else is not recognized.

```markdown
[//]: pattern
## How We Handle Errors

Everything from this heading until the next decorator or end of file
is indexed as a searchable chunk.
```

The heading text becomes the chunk's section title (max 255 characters; a longer title makes the create fail with a server error).

### What Gets Indexed

| Content | Indexed? | Why |
| --- | --- | --- |
| Decorated section (heading + body) | Yes | Marked explicitly for search |
| Undecorated section *after* a decorated one | Yes, inside the preceding chunk | A chunk only ends at the next decorator or end of file |
| `## Overview` before the first decorator | No | Not decorated; provides context, not search results |
| Intro prose before first decorator | No | Supports reading but not searching |
| Empty decorated body | No | Silently dropped |

Everything in the file is still stored and returned by `get_pattern`; the table only covers what is indexed. A pattern with no decorated sections (or only empty ones) is indexed as a single chunk holding the whole content, titled by its first heading or `Content`, so search can only return it as a whole.

### Why Decorators Matter

Decorators let you control search relevance. Content that's important for humans reading the file (introductions, disclaimers) can be stored without polluting search rankings, as long as it comes before the first decorator. This keeps search results focused on actionable guidance.

### Special Case: Overview Section

The `## Overview` section should never be decorated with `[//]: pattern`. By convention, Overview provides context for human readers — what the pattern is, when to use it, and why it matters — without competing in search rankings. Mnemonic has no special rule for the name "Overview": it stays out of the index only because it is undecorated and placed before the first decorator.

### Multiple Decorated Sections

If your pattern covers multiple independent techniques, use multiple decorated sections:

```markdown
[//]: pattern
## Setup and Initialization

Content about setup...

[//]: pattern
## Error Handling

Content about error handling...

[//]: pattern
## Performance Tuning

Content about performance...
```

Each decorated section becomes an independently searchable chunk. Someone searching "Go performance tuning" will find the third section directly, without noise from the setup section.

## Required Sections

The mnemonic-patterns validator requires these (the API itself only requires non-empty content, but without a decorated section the whole file is indexed as one chunk):

1. **`## Overview`** — What the pattern is, when to use it, and why. Keep this concise (a few sentences to a short paragraph). It's your elevator pitch.

2. **At least one `[//]: pattern` decorated section** — The actual knowledge. Without decorated sections, the whole file, Overview included, is searched as one undifferentiated chunk.

## Writing Patterns for Semantic Search

Semantic search works by understanding concepts, not matching keywords. Here are practices that make patterns discoverable and useful.

### 1. Make Each Decorated Section Independently Discoverable

Someone searching "how to wrap errors in Go" should find your error-handling section without needing surrounding context. Each section should make sense on its own.

Bad:

```markdown
[//]: pattern
## Implementation Details

All error handling logic uses the fmt.Errorf wrapper.
```

Good:

```markdown
[//]: pattern
## Wrapping Errors with fmt.Errorf

When propagating errors up the call stack, wrap them with fmt.Errorf and %w
to preserve the error chain. This allows callers to use errors.Is and errors.As
for inspection.

Example:
```

The good version includes what the pattern is, why you'd use it, and an example. A search for "error wrapping" will find it.

### 2. Use Descriptive Headings

The section heading becomes part of the searchable content: each chunk is embedded as `pattern name | tags | section title` followed by the body. Make it descriptive, not generic.

Bad: `## Implementation`, `## Example`, `## Details`

Good: `## Wrapping Errors`, `## Mocking Database Calls in Unit Tests`, `## Performance Tuning for High-Throughput Topics`

### 3. Include Code Examples

Embeddings capture the semantics of code. A pattern with concrete examples is more discoverable than one with only prose. Show, don't just tell.

````markdown
[//]: pattern
## Error Wrapping with fmt.Errorf

When propagating errors up the call stack, use fmt.Errorf with %w:

```go
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
```

This preserves the error chain. Callers can inspect the underlying error:

```go
err := doWork()
if errors.Is(err, os.ErrNotExist) {
    // handle missing file
}
```
````

### 4. Write for AI Context

The pattern content is what Claude Code reads. Make it actionable and explicit.

- "Do this, not that" with examples
- Explain the trade-offs
- Include error cases or exceptions
- Provide concrete syntax and patterns

Avoid vague guidance ("be careful," "consider," "might want to"). AI assistants work better with specificity.

### 5. Keep Chunks Focused

A decorated section covering one technique is better than one covering five. Focused sections rank better in semantic search and are easier for an AI to apply.

Bad:

```markdown
[//]: pattern
## Configuration Management

Our CLI uses Viper for configuration loading, with defaults from config.yaml,
overrides from environment variables, and CLI flags taking precedence. We also
use Cobra for command structure and custom middleware for validation...
```

Good:

```markdown
[//]: pattern
## Configuration Loading with Viper

Define a Config struct and load it with Viper. Use explicit function parameters
instead of package-level globals.

[code example]

[//]: pattern
## CLI Flag Binding

Use Cobra's BindFlag to connect CLI flags to Viper keys.

[code example]
```

### 6. Use Tags for Precision

Tags enable conjunctive (AND) filtering in search. Choose tags that are specific and actionable.

Good tags: `error-handling`, `cobra-cli`, `integration-test`, `postgres-migration`, `jwt-auth`

Avoid vague tags: `code`, `pattern`, `tech`, `important`

## Complete Example

Here's a full, realistic pattern:

`````markdown
---
name: cobra-configuration-pattern
entity_type: cli-pattern
language: go
domain: cli
description: Configuration management with explicit config injection, environment variable overrides, and clear precedence rules for Cobra CLIs
tags:
  - cobra
  - cli
  - configuration
  - viper
  - environment-variables
version: Go 1.21+
related_patterns:
  - cobra-root-command-pattern
  - error-handling-in-go
---

# Cobra Configuration Pattern

## Overview

This pattern demonstrates configuration management for Cobra CLIs with explicit
config passing, environment variable overrides, and clear precedence rules.

The pattern separates configuration concerns: defaults come from code, files
provide overrides, environment variables can be used in deployment scenarios,
and CLI flags take final precedence.

[//]: pattern
## Define Configuration Structure

Create a Config struct to hold all configuration values. Keep the struct minimal
and focused — only include settings your CLI actually needs.

```go
type Config struct {
    Host       string
    Port       int
    LogLevel   string
    Timeout    time.Duration
    Workers    int
}
```

Load configuration from multiple sources using Viper, with explicit precedence:
file → environment variables → CLI flags.

```go
func LoadConfig(cfgFile string) (*Config, error) {
    v := viper.New()

    // 1. Set defaults
    v.SetDefault("host", "localhost")
    v.SetDefault("port", 8080)
    v.SetDefault("log_level", "info")
    v.SetDefault("timeout", 30*time.Second)
    v.SetDefault("workers", 4)

    // 2. Load from file if provided
    if cfgFile != "" {
        v.SetConfigFile(cfgFile)
        if err := v.ReadInConfig(); err != nil {
            return nil, fmt.Errorf("failed to load config file: %w", err)
        }
    }

    // 3. Override with environment variables
    v.SetEnvPrefix("MYAPP")
    v.AutomaticEnv()

    // 4. Unmarshal into struct
    var cfg Config
    if err := v.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    return &cfg, nil
}
```

[//]: pattern
## Bind CLI Flags

Use Cobra's built-in flag binding to allow CLI flags to override configuration.
Bind flags after loading the config file but before unmarshaling.

```go
func setupFlags(cmd *cobra.Command, cfg *Config) {
    cmd.Flags().String("host", cfg.Host, "Server hostname")
    cmd.Flags().Int("port", cfg.Port, "Server port")
    cmd.Flags().String("log-level", cfg.LogLevel, "Log level")

    // Bind flags to Viper keys
    viper.BindPFlag("host", cmd.Flags().Lookup("host"))
    viper.BindPFlag("port", cmd.Flags().Lookup("port"))
    viper.BindPFlag("log_level", cmd.Flags().Lookup("log-level"))
}
```

Bind flags in your command's PreRunE or PersistentPreRunE to ensure they're
applied before command execution.

[//]: pattern
## Passing Config Explicitly

Never use package-level globals for configuration. Pass config as a method
receiver or function parameter:

```go
type Server struct {
    cfg *Config
}

func NewServer(cfg *Config) *Server {
    return &Server{cfg: cfg}
}

func (s *Server) Run(ctx context.Context) error {
    // Use s.cfg.Host, s.cfg.Port, etc.
}
```

This makes testing easier and config flow explicit. A reader of your code can
immediately see where config comes from.

[//]: pattern
## Example: Complete CLI with Configuration

Here's a minimal but complete example:

```go
package main

import (
    "context"
    "fmt"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

type Config struct {
    Host    string
    Port    int
    Workers int
}

func loadConfig(cfgFile string) (*Config, error) {
    v := viper.New()
    v.SetDefault("host", "localhost")
    v.SetDefault("port", 8080)
    v.SetDefault("workers", 4)

    if cfgFile != "" {
        v.SetConfigFile(cfgFile)
        if err := v.ReadInConfig(); err != nil {
            return nil, err
        }
    }

    v.SetEnvPrefix("MYAPP")
    v.AutomaticEnv()

    var cfg Config
    if err := v.Unmarshal(&cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}

type App struct {
    cfg *Config
}

func (a *App) Run(ctx context.Context) error {
    fmt.Printf("Starting server on %s:%d with %d workers\n",
        a.cfg.Host, a.cfg.Port, a.cfg.Workers)
    return nil
}

func main() {
    var cfgFile string

    rootCmd := &cobra.Command{
        Use:   "myapp",
        Short: "My application",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg, err := loadConfig(cfgFile)
            if err != nil {
                return err
            }

            app := &App{cfg: cfg}
            return app.Run(cmd.Context())
        },
    }

    rootCmd.Flags().StringVar(&cfgFile, "config", "", "Config file path")
    rootCmd.Flags().String("host", "localhost", "Server host")
    rootCmd.Flags().Int("port", 8080, "Server port")

    viper.BindPFlag("host", rootCmd.Flags().Lookup("host"))
    viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))

    rootCmd.Execute()
}
```

Usage:

```bash
# Use defaults
./myapp

# Override via environment variable
MYAPP_HOST=0.0.0.0 ./myapp

# Override via CLI flag
./myapp --host 0.0.0.0 --port 9000

# Load from config file
./myapp --config /etc/myapp/config.yaml
```
`````

## Validation

The mnemonic-patterns repository includes a validation script that checks all patterns for correctness:

```bash
./install/validate.sh --dir ./patterns
```

### What Validation Checks

These checks belong to the mnemonic-patterns script, not the API (the API's own rules are in [Frontmatter Schema](#frontmatter-schema)):

1. YAML frontmatter is present and parseable
2. All required fields are present and non-empty
3. `name` matches the regex `^[a-z][a-z0-9-]*$`
4. `language` is one of the supported enum values
5. `domain` is one of the supported enum values
6. `entity_type` uses kebab-case
7. `description` is non-empty
8. At least one `[//]: pattern` decorator is present
9. `## Overview` section is present

### Dependencies

The validation script requires:

- `yq` (mikefarah/yq) — YAML parsing
- `jq` — JSON parsing

Install these in your environment before running validation.

### Validation Failures

If validation fails, the script outputs clear error messages with line numbers and field names. Common issues:

- Missing required fields: Add them to frontmatter
- Invalid enum values: Check your language or domain against the supported values list
- Missing Overview: Add a `## Overview` section
- No decorated sections: Add at least one `[//]: pattern` section
- Invalid name format: Use kebab-case only (no underscores, spaces, or caps)

## Best Practices

### Do

- Write patterns about specific, reusable techniques
- Include concrete code examples
- Keep sections focused (one technique per section)
- Use the Overview to set context
- Tag patterns to enable precise filtering
- Keep material you don't want indexed above the first decorator
- Write for both humans and AI assistants

### Don't

- Create patterns for one-off solutions or project-specific code
- Write vague guidance ("be careful," "as needed")
- Mix multiple unrelated techniques in one section
- Put important guidance outside decorated sections
- Use generic or vague tags
- Index the Overview section
- Leave patterns without examples

## What's Next

**[Managing Patterns](06-managing-patterns.md)** — Learn how to load patterns into Mnemonic, bulk-load from files, keep your knowledge base organized, and handle versioning and deprecation.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
