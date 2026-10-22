FROM pgvector/pgvector:pg16

ENV POSTGRES_DB=mnemonic
ENV POSTGRES_USER=mnemonic

COPY *.up.sql /docker-entrypoint-initdb.d/
