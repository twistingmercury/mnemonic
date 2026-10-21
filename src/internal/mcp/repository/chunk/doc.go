// Package chunk provides the read-only PostgreSQL repository for pattern
// chunks. Each chunk is one H2-bounded section of a parent pattern with its
// own vector embedding, searched by cosine similarity via pgvector.
package chunk
