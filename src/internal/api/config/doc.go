// Package config provides configuration loading, validation, and access for
// Mnemonic. It uses Viper for multi-source configuration with environment
// variable and flag overrides, applying a strict precedence order: compiled
// defaults, configuration file, then environment variables.
//
// Documentation:
//   - Design: docs/design/configuration.md (Configuration Loading Order, Configuration Validation, Configuration Model)
//   - Architecture: docs/architecture/08-data-architecture.md (Connection Pool Configuration)
//   - Design: docs/design/data-storage.md (Connection Configuration)
package config
