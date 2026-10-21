// Package service provides the business logic layer for the Mnemonic server.
// Services sit between the MCP tool handlers and the data repositories;
// repository errors such as pattern.ErrNotFound pass through wrapped.
//
// Sub-packages: pattern, search, openai.
package service
