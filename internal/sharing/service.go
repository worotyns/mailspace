// SPDX-License-Identifier: AGPL-3.0-only
// Package sharing is the bounded team-source grant foundation for MS-009/MS-010.
// A grant authorizes one organization member to read one account's cached mail.
// It never grants sending, OpenPGP keys, labels or Space ownership, and every
// check fails closed. This package does not itself serve mail; the team read
// path enforces Authorized in a later slice.
package sharing

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrNotFound = errors.New("source grant not found")
var ErrInput = errors.New("invalid source grant")
var ErrForbidden = errors.New("source grant not permitted")

// Grant is one explicit, revocable authorization to read a bounded account
// source. An empty Mailbox means every folder of that account.
type Grant struct {
	ID           string
	WorkspaceID  string
	AccountID    string
	Mailbox      string
	GrantorID    string
	GrantorEmail string
	GranteeID    string
	GranteeEmail string
	CanDownload  bool
	CreatedAt    time.Time
}

type Service struct {
	store    *identity.Store
	accounts *accounts.Service
}

func New(store *identity.Store, accounts *accounts.Service) *Service {
	return &Service{store: store, accounts: accounts}
}

func grantID() string {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func validMailbox(mailbox string) bool {
	return len(mailbox) <= 1024 && utf8.ValidString(mailbox) && !strings.ContainsAny(mailbox, "\x00\r\n")
}

const grantColumns = `g.id,g.workspace_id,g.account_id,g.mailbox,g.grantor_id,g.grantee_id,gu.email,ge.email,g.can_download,g.created_at`

func scanGrant(scan func(...any) error) (Grant, error) {
	var grant Grant
	var at int64
	if err := scan(&grant.ID, &grant.WorkspaceID, &grant.AccountID, &grant.Mailbox, &grant.GrantorID, &grant.GranteeID, &grant.GrantorEmail, &grant.GranteeEmail, &grant.CanDownload, &at); err != nil {
		return grant, err
	}
	grant.CreatedAt = time.Unix(at, 0)
	return grant, nil
}

// Grant creates or replaces one explicit source grant. Only the current account
// owner may grant, the recipient must be a provisioned user and both must still
// be members of the named organization. The caller never supplies a grant id.
func (s *Service) Grant(ctx context.Context, owner, workspaceID, accountID, mailbox, granteeEmail string, canDownload bool) (Grant, error) {
	if !validMailbox(mailbox) {
		return Grant{}, ErrInput
	}
	if _, err := s.store.Workspace(ctx, owner, workspaceID); err != nil {
		return Grant{}, ErrForbidden
	}
	if _, err := s.accounts.Get(ctx, owner, accountID); err != nil {
		return Grant{}, ErrForbidden
	}
	grantee, err := s.store.UserByEmail(ctx, granteeEmail)
	if err != nil {
		return Grant{}, ErrNotFound
	}
	if grantee.ID == owner {
		return Grant{}, ErrInput
	}
	if _, err = s.store.Workspace(ctx, grantee.ID, workspaceID); err != nil {
		return Grant{}, ErrForbidden
	}
	_, err = s.store.Database().ExecContext(ctx, `INSERT INTO source_grants(id,workspace_id,account_id,mailbox,grantor_id,grantee_id,can_download,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(account_id,mailbox,grantee_id) DO UPDATE SET workspace_id=excluded.workspace_id,grantor_id=excluded.grantor_id,can_download=excluded.can_download`, grantID(), workspaceID, accountID, mailbox, owner, grantee.ID, canDownload, time.Now().Unix())
	if err != nil {
		return Grant{}, err
	}
	return s.lookup(ctx, owner, accountID, mailbox, grantee.ID)
}

// Revoke deletes one grant by id, but only when the caller is its grantor. The
// next authorization check fails immediately.
func (s *Service) Revoke(ctx context.Context, owner, grantID string) error {
	if len(grantID) < 16 || len(grantID) > 128 {
		return ErrInput
	}
	result, err := s.store.Database().ExecContext(ctx, `DELETE FROM source_grants WHERE id=? AND grantor_id=?`, grantID, owner)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrNotFound
	}
	return nil
}

// List returns the grants this owner created, newest first. It never includes
// another user's grants.
func (s *Service) List(ctx context.Context, owner string) ([]Grant, error) {
	rows, err := s.store.Database().QueryContext(ctx, `SELECT `+grantColumns+` FROM source_grants g JOIN users gu ON gu.id=g.grantor_id JOIN users ge ON ge.id=g.grantee_id WHERE g.grantor_id=? ORDER BY g.created_at DESC,g.id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Grant{}
	for rows.Next() {
		grant, err := scanGrant(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, grant)
	}
	return result, rows.Err()
}

// Authorized reports whether the actor may read the given cached mailbox through
// a currently valid grant. It fails closed when the grantor no longer owns the
// account or either user has left the organization.
func (s *Service) Authorized(ctx context.Context, actor, accountID, mailbox string) (bool, error) {
	if actor == "" || accountID == "" || !validMailbox(mailbox) {
		return false, nil
	}
	var found int
	err := s.store.Database().QueryRowContext(ctx, `SELECT count(*) FROM source_grants g JOIN accounts a ON a.id=g.account_id JOIN workspace_members gm ON gm.workspace_id=g.workspace_id AND gm.user_id=g.grantee_id JOIN workspace_members go ON go.workspace_id=g.workspace_id AND go.user_id=g.grantor_id WHERE g.account_id=? AND g.grantee_id=? AND a.owner_id=g.grantor_id AND (g.mailbox='' OR g.mailbox=?)`, accountID, actor, mailbox).Scan(&found)
	if err != nil {
		return false, err
	}
	return found > 0, nil
}

// GrantedAccounts lists the actor's currently valid grants, revalidating account
// ownership and organization membership on every call.
func (s *Service) GrantedAccounts(ctx context.Context, actor string) ([]Grant, error) {
	rows, err := s.store.Database().QueryContext(ctx, `SELECT `+grantColumns+` FROM source_grants g JOIN accounts a ON a.id=g.account_id JOIN users gu ON gu.id=g.grantor_id JOIN users ge ON ge.id=g.grantee_id JOIN workspace_members gm ON gm.workspace_id=g.workspace_id AND gm.user_id=g.grantee_id JOIN workspace_members go ON go.workspace_id=g.workspace_id AND go.user_id=g.grantor_id WHERE g.grantee_id=? AND a.owner_id=g.grantor_id ORDER BY g.created_at DESC,g.id`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Grant{}
	for rows.Next() {
		grant, err := scanGrant(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, grant)
	}
	return result, rows.Err()
}

func (s *Service) lookup(ctx context.Context, owner, accountID, mailbox, granteeID string) (Grant, error) {
	var grant Grant
	row := s.store.Database().QueryRowContext(ctx, `SELECT `+grantColumns+` FROM source_grants g JOIN users gu ON gu.id=g.grantor_id JOIN users ge ON ge.id=g.grantee_id WHERE g.grantor_id=? AND g.account_id=? AND g.mailbox=? AND g.grantee_id=?`, owner, accountID, mailbox, granteeID)
	grant, err := scanGrant(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return grant, ErrNotFound
	}
	return grant, err
}
