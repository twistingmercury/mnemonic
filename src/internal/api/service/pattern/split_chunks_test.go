package pattern

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_splitIntoChunks(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLen    int
		wantTitles []string
	}{
		{
			name:       "single decorated section",
			input:      "[//]: pattern\n## When to Use AsyncAPI\n\n- Event-driven architectures",
			wantLen:    1,
			wantTitles: []string{"When to Use AsyncAPI"},
		},
		{
			name:       "multiple decorated sections",
			input:      "[//]: pattern\n## Section A\ncontent A\n[//]: pattern\n## Section B\ncontent B",
			wantLen:    2,
			wantTitles: []string{"Section A", "Section B"},
		},
		{
			name:       "non-decorated heading ignored",
			input:      "## Overview\nsome text\n[//]: pattern\n## Foo\ncontent",
			wantLen:    1,
			wantTitles: []string{"Foo"},
		},
		{
			name:       "preamble before first decorator ignored",
			input:      "# Title\n\nsome intro\n\n[//]: pattern\n## Foo\ncontent",
			wantLen:    1,
			wantTitles: []string{"Foo"},
		},
		{
			name:       "no decorators becomes one chunk titled by first heading",
			input:      "## Overview\ntext",
			wantLen:    1,
			wantTitles: []string{"Overview"},
		},
		{
			name:       "no decorators and no heading",
			input:      "plain text only",
			wantLen:    1,
			wantTitles: []string{"Content"},
		},
		{
			name:       "only empty decorated sections falls back to whole content",
			input:      "# Title\n[//]: pattern\n## Empty\n",
			wantLen:    1,
			wantTitles: []string{"Title"},
		},
		{
			name:    "empty string",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "whitespace only",
			input:   " \n\t\n",
			wantLen: 0,
		},
		{
			name:       "empty decorated body dropped",
			input:      "[//]: pattern\n## Empty\n\n[//]: pattern\n## Real\ncontent",
			wantLen:    1,
			wantTitles: []string{"Real"},
		},
		{
			name:       "title extracted correctly from h2",
			input:      "[//]: pattern\n## Configuration Precedence Order\nsome content",
			wantLen:    1,
			wantTitles: []string{"Configuration Precedence Order"},
		},
		{
			name:       "h3 heading also works",
			input:      "[//]: pattern\n### Deep Section\nsome content",
			wantLen:    1,
			wantTitles: []string{"Deep Section"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitIntoChunks(tt.input)
			assert.Len(t, got, tt.wantLen)
			for i, title := range tt.wantTitles {
				if i < len(got) {
					assert.Equal(t, title, got[i].Title)
				}
			}
		})
	}
}
