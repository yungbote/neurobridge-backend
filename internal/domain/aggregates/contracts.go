package aggregates

// WriteTxOwnership defines who owns write transaction boundaries.
type WriteTxOwnership string

const (
	// WriteTxOwnedByAggregate means aggregate write methods start/manage atomic DB transactions internally.
	WriteTxOwnedByAggregate WriteTxOwnership = "aggregate_owned"
)

// ReadPolicy defines how aggregate contracts should expose reads.
type ReadPolicy string

const (
	// ReadPolicyInvariantScoped allows only reads needed for invariant decisions in write flows.
	ReadPolicyInvariantScoped ReadPolicy = "invariant_scoped_reads"
	// ReadPolicyTableRepoQueries keeps broad read-model/analytics queries on table repos.
	ReadPolicyTableRepoQueries ReadPolicy = "table_repo_queries"
)

// Contract describes aggregate-level policy expectations.
type Contract struct {
	Name             string
	WriteTxOwnership WriteTxOwnership
	ReadPolicy       ReadPolicy
	Notes            string
}

// Aggregate is the common marker for all aggregate contracts.
// Implementations should return a stable contract description.
type Aggregate interface {
	Contract() Contract
}

// RequiresAggregateOwnedTx returns true when write transaction ownership is aggregate-owned.
func (c Contract) RequiresAggregateOwnedTx() bool {
	return c.WriteTxOwnership == WriteTxOwnedByAggregate
}
