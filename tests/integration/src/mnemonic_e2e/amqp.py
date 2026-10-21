"""RabbitMQ access: publish raw bytes to the enrichment queue and inspect its depth."""

import pika

ENRICHMENT_QUEUE = "enrichment-jobs"


class Amqp:
    def __init__(self, url: str) -> None:
        self._params = pika.URLParameters(url)

    def publish_raw(self, queue: str, body: bytes) -> None:
        """Publish body to queue through the default exchange, as is (no JSON, no validation)."""
        connection = pika.BlockingConnection(self._params)
        try:
            connection.channel().basic_publish(exchange="", routing_key=queue, body=body)
        finally:
            connection.close()

    def queue_depth(self, queue: str) -> int:
        """Return the ready-message count of an existing queue."""
        connection = pika.BlockingConnection(self._params)
        try:
            return int(connection.channel().queue_declare(queue=queue, passive=True).method.message_count)
        finally:
            connection.close()
