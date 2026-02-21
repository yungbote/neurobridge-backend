// Package aggregates contains infrastructure implementations of domain aggregate contracts.
//
// Implementations in this package compose table-level repos from internal/data/repos
// and own transaction boundaries for invariant-critical write operations.
package aggregates
