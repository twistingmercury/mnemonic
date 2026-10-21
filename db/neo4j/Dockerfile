FROM neo4j:5

ENV NEO4J_AUTH=neo4j/mnemonic_dev
ENV NEO4J_PLUGINS='["apoc"]'
ENV NEO4J_dbms_security_procedures_unrestricted=apoc.*
ENV NEO4J_dbms_security_procedures_allowlist=apoc.*

# /data is a VOLUME in the base image, so schema cannot be baked into it.
# The wrapper entrypoint applies these idempotent files on every start.
COPY --chmod=0644 001_create_constraints.cypher 002_create_existence_constraints.cypher 003_create_indexes.cypher /startup/schema/
COPY --chmod=0755 schema-entrypoint.sh /startup/schema-entrypoint.sh
# --chmod on COPY also applies to the directory it creates, which would leave
# /startup/schema without the execute bit and unreadable to the neo4j user.
RUN chmod 0755 /startup/schema

# Healthy only once the wrapper has applied the schema and written the marker.
HEALTHCHECK --interval=10s --timeout=5s --start-period=60s --retries=5 \
    CMD test -f /tmp/schema_ready

# Same tini wrapper as the base image, with the schema entrypoint in front of the base one.
ENTRYPOINT ["tini", "-g", "--", "/startup/schema-entrypoint.sh"]
CMD ["neo4j"]
