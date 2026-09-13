package lifecycle

// ProcessLeaseSchemaDDL defines the authoritative SQLite lease schema matching
// the V4 production specification.
const ProcessLeaseSchemaDDL = `
CREATE TABLE IF NOT EXISTS process_lease (
    host_id       TEXT    NOT NULL,
    boot_id       TEXT    NOT NULL,
    pid           INTEGER NOT NULL,
    start_ticks   INTEGER NOT NULL,
    epoch         INTEGER NOT NULL,
    fence         INTEGER NOT NULL,
    state         TEXT    NOT NULL,
    lease_seq     INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (host_id, boot_id, pid, start_ticks),
    UNIQUE (host_id, epoch, fence)
);

CREATE INDEX IF NOT EXISTS idx_process_lease_epoch_fence
    ON process_lease (host_id, epoch, fence);

CREATE INDEX IF NOT EXISTS idx_process_lease_state
    ON process_lease (state, updated_at_ns);
`

// LeaseRecord represents an entry in the process_lease table.
type LeaseRecord struct {
	HostID      string `json:"host_id"`
	BootID      string `json:"boot_id"`
	PID         int    `json:"pid"`
	StartTicks  uint64 `json:"start_ticks"`
	Epoch       int64  `json:"epoch"`
	Fence       int64  `json:"fence"`
	State       string `json:"state"`
	LeaseSeq    int64  `json:"lease_seq"`
	UpdatedAtNS int64  `json:"updated_at_ns"`
}

// CanAdvance returns true if candidate (epoch, fence) is strictly greater
// than current (epoch, fence), enforcing monotonic authority.
func (r LeaseRecord) CanAdvance(candidateEpoch, candidateFence int64) bool {
	if candidateEpoch > r.Epoch {
		return true
	}
	if candidateEpoch == r.Epoch && candidateFence > r.Fence {
		return true
	}
	return false
}

// BuildAcquireLeaseSQL returns the UPSERT query for acquiring or updating a lease.
func BuildAcquireLeaseSQL() string {
	return `
INSERT INTO process_lease (
    host_id, boot_id, pid, start_ticks, epoch, fence, state, lease_seq, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (host_id, boot_id, pid, start_ticks) DO UPDATE SET
    epoch = excluded.epoch,
    fence = excluded.fence,
    state = excluded.state,
    lease_seq = process_lease.lease_seq + 1,
    updated_at_ns = excluded.updated_at_ns
WHERE excluded.epoch > process_lease.epoch 
   OR (excluded.epoch = process_lease.epoch AND excluded.fence > process_lease.fence);
`
}
