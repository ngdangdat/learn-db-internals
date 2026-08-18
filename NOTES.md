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

## Column- versus row-oriented storage
- This classification describes the physical on-disk layout of a table.
- Row-oriented layout (horizontal partitioning)
  - Values belonging to the same logical row are stored together.
  - A good fit when most or all fields of a record are read or written together, such as point queries and range scans over records.
  - Reading one field across many rows can load unneeded fields from the same disk blocks.
- Column-oriented layout (vertical partitioning)
  - Values belonging to the same column are stored contiguously, often in separate files or file segments.
  - A good fit for scans and aggregates over a subset of columns, such as `SELECT SUM(cost) FROM table`.
  - Contiguous same-type values improve cache use, support vectorized processing, and often compress well.
  - Reconstructing a row requires metadata that associates values across columns, such as explicit or implicit row identifiers.
- Choose the layout from access patterns: record-oriented reads favor rows; large scans or aggregates over selected columns favor columns.

### Wide-column stores
- Wide-column stores are not conventional column-oriented databases.
- Examples: Bigtable and HBase.
- They model data as a multidimensional sorted map indexed by row keys.
- Related columns are grouped into column families; families are stored separately.
- Within a column family, values for the same row key are stored together, making this layout suitable for key or key-sequence access.
- A column is identified by its family and qualifier, and column families may retain versions identified by timestamps.
## Storage
### Data and index files
- Database files use storage-engine-specific formats rather than filesystem paths to find records efficiently.
- These formats aim to improve storage efficiency, access efficiency, and update efficiency.
- Data files store data records; index files store metadata that helps locate records without scanning an entire data file.
  - Index entries can contain a record location (such as a file offset or hash bucket) or, for an index-organized table, the record itself.
  - Index files are typically smaller than data files.
  - Both data and index files are partitioned into pages, commonly sized as one or more disk blocks.
- Common data-file organizations
  - Heap file: records have no required order and are commonly appended in write order; an additional index is needed for efficient lookup.
  - Hashed file: a key hash selects a bucket containing the record.
  - Index-organized table: records are stored in key order in the index itself, allowing sequential range scans and avoiding a separate data-file lookup.
- Updates and deletes are often represented by newer records or tombstones; garbage collection later reclaims space occupied by shadowed records.

### B-Trees
- A B-Tree keeps keys sorted in every node and uses wide, shallow nodes to reduce disk-page I/O in a disk-backed design.
- This project's `btree` experiment is in memory only. Its configurable minimum degree `t` gives each non-root node between `t-1` and `2t-1` keys; splitting a full node promotes its median key.
- Inserts split full nodes before descending. Deletion keeps a child at least `t` keys wide before descending by borrowing from a sibling or merging siblings, then contracts an empty internal root.
- Keys are strings and values are copied on input and output. `Range(start, end)` returns lexicographically ordered keys in the half-open interval `[start, end)`; an empty bound is unbounded.

### Index files
- An index maps search keys to record locations in a data file, or to primary keys/records in an index-organized table.
- A primary index is built on the primary data file and commonly on its primary key.
- A secondary index is built on other search keys.
  - A primary index has one entry per search key; a secondary index can have multiple entries for one search key.
  - A secondary index can point directly to a record location or store the record's primary key.
- A clustered index preserves search-key order in the data records; a nonclustered index does not.

### Primary index as indirection
- A secondary index can store a direct data-file offset, or it can store the primary key and use the primary index to find the record.
- Direct offsets reduce the read path by avoiding a second lookup, but every secondary index that stores an offset may need an update when a record moves or is relocated during maintenance.
- Primary-key indirection reduces those pointer-update costs, particularly with many secondary indexes and write-heavy workloads.
- Its read path has an extra primary-index lookup:
  - Query -> secondary index -> primary index -> data record
- A hybrid design can store both an offset and a primary key: use the offset while valid, then fall back to the primary index when it is stale.
