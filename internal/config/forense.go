// Failover forensics: what was lost in the replication window.
//
// WHY THIS EXISTS. The high-availability model chosen on 2026-08-18
// (Litestream + etcd-based election) leaves a window: whatever the leader
// wrote and hasn't replicated yet dies with the node. The argument that
// made that model acceptable was NOT "the window is small" — it was that
// the loss can be AUDITED: the old database is kept aside and compared
// against the restored one.
//
// Without this function, that audit would be someone opening two SQLite
// files by hand, in the middle of an incident, at 3am. An argument that
// only exists on paper isn't an argument — it's a promise.
//
// 🔴 WHAT THE COMPARISON NEEDS TO SEPARATE, and merging the two would be the bug:
//
//   - a row with `wa_message_id` FILLED IN, missing in the restored one
//     => the message REACHED Meta and the gateway forgot. A consumer retry
//     with the same key sends it again: DUPLICATE on the customer's phone.
//     This is the actionable set.
//
//   - a row with `wa_message_id` EMPTY, missing in the restored one
//     => it reserved and never confirmed. It may never have gone out, or
//     gone out with the confirm failing to replicate. THERE IS NO WAY TO
//     TELL FROM HERE.
//
// Merging the two into a single number would be treating "don't know" as
// "know", which is the same disease as the blind monitor. They come out in
// separate lists, always.
package config

import (
	"database/sql"
	"fmt"
	"sort"
)

// SendAtRisk is an idempotency reservation that existed in the old
// database and doesn't exist in the restored one.
type SendAtRisk struct {
	Consumer string
	Key      string
	// Wamid empty means "reserved and never confirmed" — see the header.
	Wamid string
	// CreatedAt is the unix time of the reservation's INSERT. It's what
	// lets the loss be matched to the time of the outage.
	CreatedAt int64
}

// FailoverComparison is the audit's result.
type FailoverComparison struct {
	// Confirmed were sent to Meta and the restored database doesn't know
	// it. Retrying duplicates.
	Confirmed []SendAtRisk
	// Open reserved and never confirmed. Unknown outcome.
	Open []SendAtRisk
	// ReadInOld and ReadInCurrent exist so the report can say the SIZE
	// of what was compared. A comparison that returns "0 lost" over an old
	// database with 0 rows isn't good news — it's an empty comparison, and
	// whoever reads it has to be able to tell the two apart.
	ReadInOld     int
	ReadInCurrent int
}

// Lost tells whether there is anything actionable or ambiguous.
func (c FailoverComparison) Lost() bool {
	return len(c.Confirmed) > 0 || len(c.Open) > 0
}

// openReadOnly opens a database WITHOUT migrating and WITHOUT writing.
//
// 🔴 THIS FUNCTION EXISTS SO AS NOT TO USE OpenStore, and the reason is
// serious: OpenStore RUNS MIGRATIONS. Pointed at the old database, it
// would ALTER it — that is, the forensics tool would destroy the evidence
// it came to examine, and do it silently, on the only surviving copy of
// that data.
//
// `mode=ro` in the DSN, and no write pragma. It also does NOT ask for the
// encryption key: the `idempotencia` columns are plain text, so the
// forensics run without any secret in hand — which is desirable on its own.
func openReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("config: abrir %s somente leitura: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("config: %s nao respondeu: %w", path, err)
	}
	return db, nil
}

func readIdempotency(db *sql.DB) (map[string]SendAtRisk, error) {
	rows, err := db.Query(`SELECT consumidor, chave, wa_message_id, criado_em FROM idempotencia`)
	if err != nil {
		return nil, fmt.Errorf("config: ler idempotencia: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byKey := map[string]SendAtRisk{}
	for rows.Next() {
		var e SendAtRisk
		if err := rows.Scan(&e.Consumer, &e.Key, &e.Wamid, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("config: ler linha de idempotencia: %w", err)
		}
		byKey[e.Consumer+"\x00"+e.Key] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: percorrer idempotencia: %w", err)
	}
	return byKey, nil
}

// CompareFailover lists what existed in the OLD database and disappeared
// from the CURRENT one.
//
// Both are opened READ ONLY. The direction of the comparison is
// deliberately asymmetric: a key that exists only in the CURRENT one is
// work the successor did after taking over — normal, and not of interest
// here.
func CompareFailover(oldPath, currentPath string) (FailoverComparison, error) {
	oldDB, err := openReadOnly(oldPath)
	if err != nil {
		return FailoverComparison{}, err
	}
	defer func() { _ = oldDB.Close() }()

	currentDB, err := openReadOnly(currentPath)
	if err != nil {
		return FailoverComparison{}, err
	}
	defer func() { _ = currentDB.Close() }()

	old, err := readIdempotency(oldDB)
	if err != nil {
		return FailoverComparison{}, fmt.Errorf("banco antigo (%s): %w", oldPath, err)
	}
	current, err := readIdempotency(currentDB)
	if err != nil {
		return FailoverComparison{}, fmt.Errorf("banco atual (%s): %w", currentPath, err)
	}

	c := FailoverComparison{ReadInOld: len(old), ReadInCurrent: len(current)}
	for id, e := range old {
		if _, exists := current[id]; exists {
			continue
		}
		if e.Wamid != "" {
			c.Confirmed = append(c.Confirmed, e)
		} else {
			c.Open = append(c.Open, e)
		}
	}
	// Stable order, OLDEST to newest: whoever reads it is reconstructing a
	// timeline, and Go maps iterate in random order — two runs would give
	// different reports for the same pair of databases.
	sortRows := func(s []SendAtRisk) {
		sort.Slice(s, func(i, j int) bool {
			if s[i].CreatedAt != s[j].CreatedAt {
				return s[i].CreatedAt < s[j].CreatedAt
			}
			return s[i].Consumer+s[i].Key < s[j].Consumer+s[j].Key
		})
	}
	sortRows(c.Confirmed)
	sortRows(c.Open)
	return c, nil
}
