# Study Notes
## Memory
### Durability in memory-based stores
- The database keeps its working data in memory, which is volatile.
- A write operation appends its result to a sequential write-ahead log (WAL) on disk before the operation is considered complete.
  - The WAL provides durability for changes that have not yet reached the backup.
- The database also maintains a disk-resident backup (a snapshot of database state).
  - Updating this backup may happen asynchronously and in batches, decoupled from client requests.
  - The backup reduces recovery time because startup does not need to replay the entire WAL.
- A checkpoint applies a batch of WAL records to the backup.
  - After a successful checkpoint, the backup represents state at that checkpoint boundary.
  - WAL records already included in that backup can be discarded.
- Database recovery
  1. Load the latest disk backup/snapshot as the baseline state.
  2. Replay WAL records written after the checkpoint boundary to reconstruct the latest durable state.

## Column-based and row-based
- Analytics focus: column base (`SELECT SUM(cost) FROM table`)
- Row accessibility focus: row base (`SELECT * FROM table`)
- Wide column store
  - Bigtable and HBase
- Column family
## Storage
- Data files
- Index files
### Index files
- Primary Index as an indirection
  + This helps reduce workload on write
    - Write process only needs to care of the primary index linkage
  + Reading requires an additional jump
    - Query -> secondary index -> primary index -> row data
