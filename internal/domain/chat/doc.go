package chat

// Storage truth:
// - Postgres is canonical for threads/messages and derived memory artifacts.
// - Vector search is a rebuildable retrieval index over derived "docs".
