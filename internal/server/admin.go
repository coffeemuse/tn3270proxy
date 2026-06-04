package server

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// AdminStore is the slice of *store.Store the admin flow needs.
type AdminStore interface {
	ListUsers(ctx context.Context) ([]store.User, error)
	GetUserByUsername(ctx context.Context, username string) (store.User, error)
	GetUserGroups(ctx context.Context, userID int64) ([]string, error)
	CreateUser(ctx context.Context, username, passwordHash string) (int64, error)
	SetPassword(ctx context.Context, userID int64, passwordHash string) error
	DeleteUser(ctx context.Context, userID int64) error
	AddUserToGroup(ctx context.Context, userID, groupID int64) error
	RemoveUserFromGroup(ctx context.Context, userID, groupID int64) error

	ListGroups(ctx context.Context) ([]store.Group, error)
	CreateGroup(ctx context.Context, name string) (int64, error)
	DeleteGroup(ctx context.Context, groupID int64) error
	CountGroupMembers(ctx context.Context, groupID int64) (int, error)
	CountGroupServices(ctx context.Context, groupID int64) (int, error)
	ListUsersInGroup(ctx context.Context, groupID int64) ([]store.User, error)

	ListAllServices(ctx context.Context) ([]store.Service, error)
	GetService(ctx context.Context, id int64) (store.Service, error)
	CreateService(ctx context.Context, name, host string, port int, tls, verify bool) (int64, error)
	UpdateService(ctx context.Context, id int64, name, host string, port int, tls, verify bool) error
	DeleteService(ctx context.Context, serviceID int64) error
	ListGroupsForService(ctx context.Context, serviceID int64) ([]store.Group, error)
	LinkGroupService(ctx context.Context, groupID, serviceID int64) error
	UnlinkGroupService(ctx context.Context, groupID, serviceID int64) error
}

var _ AdminStore = (*store.Store)(nil)

// msgTempError is shown for any store failure; details go to the log, never
// to the screen.
const msgTempError = "TEMPORARY ERROR; TRY AGAIN"

// adminFlow drives the admin screen set for one authenticated admin. Policy
// (guardrails, duplicate pre-checks, validation, hashing) lives here; the
// store stays mechanical.
type adminFlow struct {
	store     AdminStore
	presenter AdminPresenter
	identity  auth.Identity
	term      Term // negotiated client terminal; drives page size + screen rendering
	// audit records admin CRUD events; nil (direct tests) disables auditing.
	audit func(ctx context.Context, ev store.AuditEvent)
}

// Run loops on the admin menu until the user leaves via PF3 (back to the
// service menu). A non-nil error means the client connection is unusable and
// the session should end.
func (f *adminFlow) Run(ctx context.Context, conn net.Conn) error {
	errMsg := ""
	for {
		choice, back, err := f.presenter.AdminMenu(conn, f.term, errMsg)
		if err != nil {
			return err
		}
		if back {
			return nil
		}
		errMsg = ""
		switch choice {
		case 1:
			err = f.users(ctx, conn)
		case 2:
			err = f.groups(ctx, conn)
		case 3:
			err = f.services(ctx, conn)
		}
		if err != nil {
			return err
		}
	}
}

// pageBounds clamps page to the data and returns the slice bounds plus the
// row indicator. The page size follows the client terminal's row count.
// Clamping matters after deletions shrink the list.
func (f *adminFlow) pageBounds(page, total int) (clamped, start, end int, info string) {
	if total == 0 {
		return 0, 0, 0, "ROW 0 OF 0"
	}
	size := f.term.Geometry().ListPageSize()
	maxPage := (total - 1) / size
	if page > maxPage {
		page = maxPage
	}
	if page < 0 {
		page = 0
	}
	start = page * size
	end = min(start+size, total)
	return page, start, end, fmt.Sprintf("ROW %d TO %d OF %d", start+1, end, total)
}

// groupIDByName resolves a group name via ListGroups (small N; no extra store
// method needed).
func (f *adminFlow) groupIDByName(ctx context.Context, name string) (int64, bool, error) {
	groups, err := f.store.ListGroups(ctx)
	if err != nil {
		return 0, false, err
	}
	for _, g := range groups {
		if g.Name == name {
			return g.ID, true, nil
		}
	}
	return 0, false, nil
}

// logStoreErr logs a store failure (never credentials) and returns the generic
// screen message.
func logStoreErr(op string, err error) string {
	log.Printf("admin: %s failed: %v", op, err)
	return msgTempError
}

// recordAdmin emits one admin audit event ("who changed what"). Call it only
// after the store mutation has succeeded, so the trail reflects reality.
func (f *adminFlow) recordAdmin(ctx context.Context, detail string) {
	if f.audit == nil {
		return
	}
	f.audit(ctx, store.AuditEvent{
		Kind: store.AuditAdmin, Username: f.identity.Username, Detail: detail})
}

// users is implemented in admin_users.go (Task 11).
// groups is implemented in admin_groups.go (Task 13).
// services is implemented in admin_services.go (Task 14).
