// Package authoritywriter provides the finite W1 transaction ordering protocol.
// Its capabilities establish ordering and lifetime, never credential evidence.
package authoritywriter

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("authority writer unavailable")
	ErrDenied      = errors.New("authority writer denied")
	ErrUnrooted    = errors.New("authority writer unrooted")
	ErrPhase       = errors.New("authority writer phase violation")
	ErrClosed      = errors.New("authority writer closed")
)

type Realm struct{ Installation, Application, Environment uuid.UUID }
type Table uint8

const (
	Persons Table = iota + 1
	Emails
	Credentials
	Connections
	Bindings
	Challenges
	Factors
	Flows
	Sessions
	Workspaces
	Memberships
)

type Phase uint8

const (
	P Phase = iota + 1
	C
	H
	S
	W
	D
	F
)

type Access uint8

const (
	ExistingUpdate Access = iota + 1
	ExistingShare
	ReservedInsert
)

type Row struct {
	Table  Table
	ID     uuid.UUID
	Access Access
}
type Delivery struct {
	MaterialID uuid.UUID
	JobKey     string
}
type Plan struct{ data *planData }
type planData struct {
	operation *OperationPlan
	realm     Realm
	rows      []Row
	delivery  []Delivery
}

func validID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
func tablePhase(t Table) Phase {
	switch t {
	case Persons:
		return P
	case Emails, Credentials, Connections, Bindings:
		return C
	case Challenges, Factors, Flows:
		return H
	case Sessions:
		return S
	case Workspaces, Memberships:
		return W
	default:
		return 0
	}
}

// NewPlan copies the finite lock inventory. It accepts no SQL or table names.
func NewPlan(realm Realm, rows []Row, delivery []Delivery) (Plan, error) {
	if !validID(realm.Installation) || !validID(realm.Application) || !validID(realm.Environment) || len(delivery) > 1 {
		return Plan{}, ErrUnrooted
	}
	d := &planData{realm: realm, rows: append([]Row(nil), rows...), delivery: append([]Delivery(nil), delivery...)}
	for _, r := range d.rows {
		validAccess := r.Access == ExistingUpdate || r.Access == ReservedInsert || r.Access == ExistingShare && r.Table == Connections
		if tablePhase(r.Table) == 0 || !validID(r.ID) || !validAccess {
			return Plan{}, ErrUnrooted
		}
	}
	sort.Slice(d.rows, func(i, j int) bool {
		a, b := d.rows[i], d.rows[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		return bytes.Compare(a.ID[:], b.ID[:]) < 0
	})
	for i := 1; i < len(d.rows); i++ {
		if d.rows[i-1].Table == d.rows[i].Table && d.rows[i-1].ID == d.rows[i].ID {
			return Plan{}, ErrUnrooted
		}
	}
	for i, item := range d.delivery {
		if !validID(item.MaterialID) || item.JobKey == "" || len(item.JobKey) > 256 || !utf8.ValidString(item.JobKey) || strings.ContainsAny(item.JobKey, "\x00\r\n") {
			return Plan{}, ErrUnrooted
		}
		d.delivery[i].JobKey = strings.Clone(item.JobKey)
	}
	return Plan{data: d}, nil
}

func (p *planData) contains(row Row) bool {
	if p == nil {
		return false
	}
	i := sort.Search(len(p.rows), func(i int) bool {
		r := p.rows[i]
		return r.Table > row.Table || r.Table == row.Table && bytes.Compare(r.ID[:], row.ID[:]) >= 0
	})
	return i < len(p.rows) && p.rows[i] == row
}
