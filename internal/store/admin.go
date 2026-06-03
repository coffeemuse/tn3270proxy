package store

import "context"

// Reserved group namespace. Group names with the (case-insensitive) prefix
// ReservedGroupPrefix are dictated by the app: the admin UI can manage their
// membership but can never create or delete them. AdminGroup is the first such
// group; membership in it gates the 3270 admin screens. It is auto-created by
// migrate() so it always exists.
const (
	AdminGroup          = "ZZADMIN"
	ReservedGroupPrefix = "ZZ"
)

// Group is a named collection of users granted access to services.
type Group struct {
	ID   int64
	Name string
}

// ListUsers returns all users ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, username, password_hash FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListGroups returns all groups ordered by name.
func (s *Store) ListGroups(ctx context.Context) ([]Group, error) {
	return s.queryGroups(ctx, "SELECT id, name FROM groups ORDER BY name")
}

// ListGroupsForService returns the groups granted access to the service.
func (s *Store) ListGroupsForService(ctx context.Context, serviceID int64) ([]Group, error) {
	return s.queryGroups(ctx,
		`SELECT g.id, g.name FROM groups g
		 JOIN group_services gs ON gs.group_id = g.id
		 WHERE gs.service_id = ? ORDER BY g.name`, serviceID)
}

func (s *Store) queryGroups(ctx context.Context, query string, args ...any) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListAllServices returns all services ordered by name.
func (s *Store) ListAllServices(ctx context.Context) ([]Service, error) {
	return s.queryServices(ctx,
		"SELECT id, name, host, port, tls, tls_verify FROM services ORDER BY name")
}

// GetService returns the service by id, or ErrNotFound.
func (s *Store) GetService(ctx context.Context, id int64) (Service, error) {
	svcs, err := s.queryServices(ctx,
		"SELECT id, name, host, port, tls, tls_verify FROM services WHERE id = ?", id)
	if err != nil {
		return Service{}, err
	}
	if len(svcs) == 0 {
		return Service{}, ErrNotFound
	}
	return svcs[0], nil
}

func (s *Store) queryServices(ctx context.Context, query string, args ...any) ([]Service, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var svc Service
		var tlsInt, verifyInt int
		if err := rows.Scan(&svc.ID, &svc.Name, &svc.Host, &svc.Port, &tlsInt, &verifyInt); err != nil {
			return nil, err
		}
		svc.TLS = tlsInt != 0
		svc.TLSVerify = verifyInt != 0
		out = append(out, svc)
	}
	return out, rows.Err()
}

// SetPassword replaces the user's password hash. It exists because CreateUser
// is INSERT OR IGNORE and never updates an existing row. Returns ErrNotFound
// for an unknown user id.
func (s *Store) SetPassword(ctx context.Context, userID int64, passwordHash string) error {
	return s.execExpectingRow(ctx,
		"UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, userID)
}

// UpdateService replaces every editable field of the service. Returns
// ErrNotFound for an unknown service id.
func (s *Store) UpdateService(ctx context.Context, id int64, name, host string, port int, tls, verify bool) error {
	tlsInt, verifyInt := 0, 0
	if tls {
		tlsInt = 1
	}
	if verify {
		verifyInt = 1
	}
	return s.execExpectingRow(ctx,
		"UPDATE services SET name = ?, host = ?, port = ?, tls = ?, tls_verify = ? WHERE id = ?",
		name, host, port, tlsInt, verifyInt, id)
}

// execExpectingRow runs a statement that must affect exactly one row, mapping
// zero affected rows to ErrNotFound.
func (s *Store) execExpectingRow(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}
