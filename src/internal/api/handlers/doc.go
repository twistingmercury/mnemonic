// Package handlers provides shared utilities and the parent package for
// Mnemonic's HTTP handler groups. Sub-packages implement endpoint logic
// for patterns and operational endpoints.
//
// This package contains shared types and utilities used by all handler
// sub-packages: RFC 7807 error responses, cursor-based pagination,
// query parameter parsing, and service error mapping.
//
// Documentation:
//   - API: docs/api/openapi/mnemonic-v1.yaml
//   - Design: docs/design/service-layer.md (Error Mapping, Cursor-Based Pagination)
package handlers
