package mcpserver

import (
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	patternrepo "github.com/twistingmercury/librarian/internal/mcp/repository/pattern"
	patternsvc "github.com/twistingmercury/librarian/internal/mcp/service/pattern"
	searchsvc "github.com/twistingmercury/librarian/internal/mcp/service/search"
)

// formatSearchResults formats search results as markdown for LLM consumption.
// Similarity scores are displayed as percentages (e.g., "92% match") because
// the single vector similarity score is intuitive as a percentage.
func formatSearchResults(result *searchsvc.SearchResult) string {
	if len(result.Matches) == 0 {
		return fmt.Sprintf("No patterns found matching '%s'.", result.Query)
	}

	var sb strings.Builder

	// Count distinct pattern IDs; one pattern may have multiple matching chunks.
	seen := make(map[uuid.UUID]struct{}, len(result.Matches))
	for _, m := range result.Matches {
		seen[m.PatternID] = struct{}{}
	}
	distinctPatterns := len(seen)
	sections := len(result.Matches)

	header := fmt.Sprintf("Found %d sections across %d patterns matching '%s'", sections, distinctPatterns, result.Query)
	sb.WriteString(header)
	sb.WriteString(":\n")

	for _, m := range result.Matches {
		sb.WriteString("\n---\n\n")
		writeMatchEntry(&sb, m)
	}

	if len(result.GraphMatches) > 0 {
		sb.WriteString("\n---\n\n")
		sb.WriteString("### Related Patterns (via graph)\n\n")
		for _, gm := range result.GraphMatches {
			writeGraphMatchEntry(&sb, gm)
		}
	}

	return sb.String()
}

// writeMatchEntry writes a single search match entry to the builder.
func writeMatchEntry(sb *strings.Builder, m *searchsvc.ChunkMatch) {
	pct := int(math.Round(m.Similarity * 100))
	fmt.Fprintf(sb, "## %s (%d%% match)\n\n", m.PatternName, pct)

	if m.SectionTitle != "" {
		fmt.Fprintf(sb, "**Section:** %s\n\n", m.SectionTitle)
	}

	if len(m.Tags) > 0 {
		fmt.Fprintf(sb, "**Tags:** %s\n\n", strings.Join(m.Tags, ", "))
	}

	sb.WriteString(m.Content)
	sb.WriteByte('\n')
}

// writeGraphMatchEntry writes a single graph-expanded match entry to the builder.
func writeGraphMatchEntry(sb *strings.Builder, gm *searchsvc.GraphMatch) {
	fmt.Fprintf(sb, "## %s (similarity: %.2f)\n\n", gm.PatternName, gm.Similarity)
	fmt.Fprintf(sb, "**Found via:** %s\n", gm.SeedPatternName)

	if len(gm.ConceptNames) > 0 {
		fmt.Fprintf(sb, "**Shared concepts:** %s\n", strings.Join(gm.ConceptNames, ", "))
	}
	sb.WriteByte('\n')
}

// formatRelatedPatterns formats related patterns as markdown for LLM consumption.
// Similarity is displayed as a decimal (e.g., "similarity: 0.85") because it
// represents computed concept-overlap strength where the raw value is more
// meaningful.
func formatRelatedPatterns(sourceName string, results []patternsvc.RelatedPatternResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No related patterns found for '%s'.", sourceName)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d patterns related to '%s':\n", len(results), sourceName)

	for _, r := range results {
		sb.WriteString("\n---\n\n")
		writeRelatedEntry(&sb, &r)
	}

	return sb.String()
}

// writeRelatedEntry writes a single related pattern entry to the builder.
func writeRelatedEntry(sb *strings.Builder, r *patternsvc.RelatedPatternResult) {
	fmt.Fprintf(sb, "## %s (similarity: %.2f)\n\n", r.Name, r.Similarity)
	fmt.Fprintf(sb, "**Relationship:** %s\n", r.Relationship)

	if len(r.SharedConcepts) > 0 {
		fmt.Fprintf(sb, "**Shared concepts:** %s\n", strings.Join(r.SharedConcepts, ", "))
	}
}

// formatPattern formats a full pattern with optional graph context as markdown.
func formatPattern(p *patternrepo.Pattern, gc *patternsvc.GraphContext) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## %s\n\n", p.Name)
	fmt.Fprintf(&sb, "**ID:** %s\n", p.ID)

	if len(p.Tags) > 0 {
		fmt.Fprintf(&sb, "**Tags:** %s\n", strings.Join(p.Tags, ", "))
	}

	writeEnrichmentStatus(&sb, p)

	sb.WriteString("\n## Content\n\n")
	sb.WriteString(p.Content)
	sb.WriteByte('\n')

	// Graph context sections are only included for enriched patterns with
	// available graph data.
	if gc != nil {
		writeRelatedPatternsSection(&sb, gc.RelatedPatterns)
		writeConceptsSection(&sb, gc.Concepts)
	}

	return sb.String()
}

// writeEnrichmentStatus writes the enrichment status line.
func writeEnrichmentStatus(sb *strings.Builder, p *patternrepo.Pattern) {
	switch p.EnrichmentStatus {
	case "enriched":
		enrichedAt := "unknown"
		if p.EnrichedAt != nil {
			enrichedAt = p.EnrichedAt.Format("2006-01-02T15:04:05Z")
		}
		fmt.Fprintf(sb, "**Enrichment:** enriched (%s)\n", enrichedAt)
	case "failed":
		msg := "unknown error"
		if p.EnrichmentError != nil {
			msg = *p.EnrichmentError
		}
		fmt.Fprintf(sb, "**Enrichment:** failed -- %s\n", msg)
	default:
		sb.WriteString("**Enrichment:** pending\n")
	}
}

// writeRelatedPatternsSection writes the related patterns list if any exist.
func writeRelatedPatternsSection(sb *strings.Builder, related []patternsvc.RelatedPatternResult) {
	if len(related) == 0 {
		return
	}

	sb.WriteString("\n### Related Patterns\n\n")
	for _, r := range related {
		fmt.Fprintf(sb, "- **%s** (%s, similarity: %.2f)\n", r.Name, r.Relationship, r.Similarity)
	}
}

// writeConceptsSection writes the extracted concepts list if any exist.
func writeConceptsSection(sb *strings.Builder, concepts []patternsvc.ConceptResult) {
	if len(concepts) == 0 {
		return
	}

	sb.WriteString("\n### Extracted Concepts\n\n")
	for _, c := range concepts {
		fmt.Fprintf(sb, "- **%s**\n", c.Name)
	}
}
