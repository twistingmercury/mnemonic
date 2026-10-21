package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/api/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

func TestPrivateDiagnosticBoundsAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		hidden        []string
	}{
		{"upstream body", "sentinel: API returned status 502: raw private payload\nmore private text", []string{"raw private payload", "more private text"}},
		{"credentials", "sentinel: Authorization: Bearer supersecret password=privatepwd token=privatetoken api_key=privatekey", []string{"supersecret", "privatepwd", "privatetoken", "privatekey"}},
		{"url", "sentinel: https://username:privatepwd@example.com/path?q=privatequery", []string{"username", "privatepwd", "privatequery", "example.com"}},
		{"quoted data", "sentinel: database rejected 'private pattern' and \"private query\"", []string{"private pattern", "private query"}},
		{"oversized quote", "sentinel: '" + strings.Repeat("privatecontent", 500) + "'", []string{"privatecontent"}},
		{"multiline quote", "sentinel: \"private\ncontent\"", []string{"private", "content"}},
		{"escaped double quotes", `sentinel: "value \"private-double-marker\" end"`, []string{"private-double-marker"}},
		{"escaped single quotes", `sentinel: 'value \'private-single-marker\' end'`, []string{"private-single-marker"}},
		{"escaped backslashes", `sentinel: "value \\ private-slash-marker"`, []string{"private-slash-marker"}},
		{"unclosed escaped value", `sentinel: "value \"private-unclosed-marker`, []string{"private-unclosed-marker"}},
		{"trailing escape", `sentinel: "private-trailing-marker\`, []string{"private-trailing-marker"}},
		{"truncated escaped value", `sentinel: "value \"private-truncated-marker` + strings.Repeat("x", 5000), []string{"private-truncated-marker"}},
		{"bounded", "sentinel: " + strings.Repeat("é", 1000), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, attachRaw := range []bool{false, true} {
				var logs bytes.Buffer
				router := gin.New()
				router.Use(middleware.CompletionLogging(zerolog.New(&logs), nil))
				router.GET("/test/:id", func(c *gin.Context) {
					if attachRaw {
						_ = c.Error(errors.New(tc.message)).SetMeta("private-meta")
					} else {
						middleware.RecordFailure(c, errors.New(tc.message))
					}
					c.Status(500)
				})
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest("GET", "/test/private-path", nil))
				var log map[string]any
				require.NoError(t, json.Unmarshal(logs.Bytes(), &log))
				cause, ok := log["error"].(string)
				require.True(t, ok)
				assert.Contains(t, cause, "sentinel")
				assert.LessOrEqual(t, len(cause), 512)
				assert.True(t, utf8.ValidString(cause))
				assert.NotContains(t, logs.String(), "private-meta")
				assert.NotContains(t, logs.String(), "private-path")
				for _, hidden := range tc.hidden {
					assert.NotContains(t, logs.String(), hidden)
				}
			}
		})
	}
}
