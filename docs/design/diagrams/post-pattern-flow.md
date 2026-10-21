# POST /v1/api/patterns — Activity Diagram

> Back to [Project README](../../README.md)

Posting a pattern has two halves. `mnemonic-api` validates the request, writes the pattern, its chunks and one enrichment job per chunk in a single PostgreSQL transaction, publishes each job ID to the RabbitMQ `enrichment-jobs` queue, and returns `202 Accepted`. `mnemonic-enricher` then embeds each chunk and, after the pattern's last chunk is embedded, extracts concepts and syncs the Neo4j graph. For the end-to-end sequence across all components, see [System Architecture - Data Loading via Admin API](../architecture/system/02-system-architecture.md#data-loading-via-admin-api).

## API: request to 202 Accepted

Source: `mnemonic-api` `src/internal/handlers/patterns/patterns.go` (`Create`), `src/internal/service/pattern/service.go` (`Create`, `insertChunksAndJobs`, `publishJob`), `src/internal/queue/rabbitmq/publisher.go` (`Publish`).

```mermaid
flowchart TD
    A([HTTP POST /v1/api/patterns]) --> B[Handler: Create]

    B --> C[ShouldBindJSON into patternRequest]
    C --> D{JSON parse<br/>error?}
    D -- yes --> E[RespondValidationError<br/>400 Bad Request]
    D -- no --> F[validatePatternFields<br/>format, length, vocabulary]
    F --> G{any field<br/>errors?}
    G -- yes --> G1[RespondValidationError<br/>400 Bad Request]
    G -- no --> J[Normalise nil tags and<br/>related_patterns to empty slices]
    J --> AA[patternSvc.Create]

    subgraph SVC [Service: pattern.Create]
        AA --> AB[Begin PostgreSQL transaction]
        AB --> AC["patternRepo.Create<br/>INSERT INTO patterns<br/>enrichment_status = 'pending'"]
        AC --> AD{error?}
        AD -- unique violation --> AD1[return service.ErrConflict]
        AD -- other error --> AD2[return wrapped error]
        AD -- nil --> AE["splitIntoChunks(content)<br/>one chunk per '[//]: pattern' section,<br/>else one whole-content chunk"]
        AE --> AF{any chunks?}
        AF -- no --> AK
        AF -- yes --> AG[chunkRepo.CreateBatch<br/>one INSERT for all chunks]
        AG --> AH[enrichmentRepo.CreatePendingForChunks<br/>one INSERT, one pending job per chunk]
        AH --> AI{error?}
        AI -- yes --> AI1[return wrapped error<br/>transaction rolls back]
        AI -- no --> AK[Commit transaction]
        AK --> AL{commit error?}
        AL -- yes --> AL1[return wrapped error]
        AL -- no --> AM[For each job ID: publishJob]
        AM --> AN["Publisher.Publish<br/>body {job_id}, persistent,<br/>W3C traceparent in AMQP headers"]
        AN --> AO{publish failed after<br/>one reconnect and retry?}
        AO -- yes --> AO1[log warning;<br/>job row stays pending]
        AO -- no --> AP[return pattern]
        AO1 --> AP
    end

    AD1 --> BC
    AD2 --> BC
    AI1 --> BC
    AL1 --> BC
    AP --> BC{svc.Create error?}
    BC -- service.ErrConflict --> BC1[409 Conflict]
    BC -- other error --> BC3[500 Internal Server Error]
    BC -- nil --> BI[toPatternResponse]

    BI --> BJ[Set Location header<br/>/v1/api/patterns/:id]
    BJ --> BK([202 Accepted + patternResponse body])
```

## Enricher: job to enriched pattern

Source: `mnemonic-enricher` `src/internal/enricher/enrichment.go` (`Run`, `handleDelivery`, `handleRetry`), `src/internal/service/enrichment/service.go` (`processChunkJob`, `runGraphPipeline`), `src/internal/repository/enrichmentjob/repository.go` (`Start`, `ClaimDue`, `MarkFailed`).

```mermaid
flowchart TD
    Q[["RabbitMQ<br/>enrichment-jobs"]] --> D1[handleDelivery<br/>continue trace from traceparent header]
    SW([Retry sweep every 15s]) --> S1["ClaimDue: FOR UPDATE SKIP LOCKED<br/>failed jobs whose retry is due, and<br/>never-attempted jobs pending over 2 min"]
    S1 --> RUN

    D1 --> D2{body parses?}
    D2 -- no --> NACK[Nack, no requeue]
    D2 -- yes --> D3["StartJob: mark processing<br/>only if pending and due"]
    D3 --> D4{result}
    D4 -- unavailable --> ACK1[Ack; sweep owns the job]
    D4 -- error --> NACK
    D4 -- started --> RUN

    RUN[processChunkJob] --> P1[Load chunk with pattern name and tags]
    P1 --> P2["OpenAI embeddings<br/>name, tags, section title + content"]
    P2 --> P3[Store embedding,<br/>mark chunk enriched]
    P3 --> P4{every chunk of the<br/>pattern enriched?}
    P4 -- no --> DONE
    P4 -- yes --> P5[OpenAI chat completion<br/>extract concepts]
    P5 --> P6["Neo4j SyncPattern: Pattern node,<br/>MENTIONED_IN concepts, RELATED_TO edges"]
    P6 --> P7["Pattern enrichment_status = 'enriched'"]
    P7 --> DONE[Mark job completed]
    DONE --> ACK2[Ack delivery]

    P1 -. step fails .-> F1["Mark pattern (and chunk) failed;<br/>MarkFailed: attempts + 1, pending again<br/>after retry_delay, or failed at max_attempts"]
    P2 -. step fails .-> F1
    P5 -. step fails .-> F1
    P6 -. step fails .-> F1
    F1 --> ACK2
```

## Notes

- `validatePatternFields` limits: `name` required, max 128 runes, kebab-case; `content` required, max 100000 bytes; `description` max 500 runes; `tags` max 20 items; `entity_type` required, max 100 runes, kebab-case; `language` and `domain` required, max 64 runes, kebab-case, and must be in the configured vocabulary (`vocabulary.languages`, `vocabulary.domains`), otherwise `INVALID_VALUE` (see [ADR-007](../architecture/system/00-architectural-decisions.md#adr-007)).
- Error responses are RFC 7807 problem details. The service maps a duplicate name to `409 Conflict`; any other failure returns `500` and nothing is persisted, because the pattern, chunks and jobs share one transaction.
- `enrichment_status` is always `pending` on insert; enrichment happens asynchronously, so a `202` does not mean the pattern is searchable yet.
- Sections preceded by a `[//]: pattern` decorator line and a heading become chunks; text outside them is not indexed. Content with no decorated section (or only empty ones) becomes a single chunk holding the whole trimmed content, titled by its first heading or `Content`, so it still gets one enrichment job. Only whitespace-only content yields no chunks.
- Jobs are published only after the transaction commits, so a rolled-back write never enqueues work. Publishing is best-effort ([ADR-009](../architecture/system/00-architectural-decisions.md#adr-009)): on failure the publisher waits `queue.rabbitmq.reconnect_delay` (default 5s), reconnects once and retries; if that also fails it increments `mnemonic.queue.publish_failures_total` and the job row stays `pending`. The enricher's retry sweep picks it up after the 2-minute grace period.
- The API does not write to Neo4j when a pattern is created; the enricher builds the graph. The API's only graph write is the best-effort cleanup on `DELETE`.
- A delivery whose job the sweep already took, or that is waiting out its retry delay, is acked without processing (`Start` is conditional). Maintenance every 5 minutes returns jobs stuck in `processing` longer than `enrichment.job_timeout` to `pending` and deletes old completed and failed jobs.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
