FROM pgvector/pgvector:pg16

ENV POSTGRES_DB=mnemonic
ENV POSTGRES_USER=mnemonic
ENV POSTGRES_PASSWORD=mnemonic_dev

COPY *.up.sql /docker-entrypoint-initdb.d/
