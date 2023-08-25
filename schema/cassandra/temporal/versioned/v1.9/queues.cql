CREATE TABLE queue_messages
(
    queue_id         varchar,
    queue_partition  int,
    message_id       bigint,
    message_payload  blob,
    message_encoding text,
    PRIMARY KEY ((queue_id, queue_partition), message_id)
) WITH COMPACTION = {
    'class': 'org.apache.cassandra.db.compaction.LeveledCompactionStrategy'
    };
