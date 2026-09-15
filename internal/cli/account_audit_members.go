// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	gormlib "gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/wepala/weos/v3/domain/repositories"
	gormdb "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"
)

var auditMembersAccount string

var accountAuditMembersCmd = &cobra.Command{
	Use:   "audit-members",
	Short: "List account memberships that no sign-up or invite explains (changes nothing)",
	Long: `Lists the memberships of an account that nothing on record explains. The command
only reads: it changes nothing, and it never removes a row. It reads the database
directly and needs no running server.

Earlier versions saved a role change made on the Users page into the instance's
first account, whatever account the owner or admin who made it acted in. Those
memberships are still there, and a person holding owner or admin through one of
them can manage the first account's people.

By default the command reads the instance's first account, the one with the
lowest id; --account names another. A membership is listed when:

  - the account's history does not record the person joining it, and no invite
    in the account names them, by the person or by an address on one of their
    credentials ("no sign-up or invite in this account"); or
  - its role is not the role their joining recorded ("role differs from the
    recorded <role>").

A role an owner or admin of the account changed on the Users page since is listed
as well, because that change is not recorded either. Check each row with the
account's owners before you act on it.

WRITTEN AT is the membership's created_at, which every role save rewrites, so it
is when the row was last written. The command prints agent ids and roles, and no
email address or name.

To remove a membership the command lists, back up the database, then run this
against the same database, with the account id and agent id it printed:

  DELETE FROM account_members WHERE account_id = '<account id>' AND agent_id = '<agent id>';

To put back the role the person's joining recorded instead:

  UPDATE account_members SET role_id = '<recorded role>' WHERE account_id = '<account id>' AND agent_id = '<agent id>';

The person keeps every other account they belong to. A browser session they hold
in the account is refused on its next request, and the features they resolved in
it expire within FEATURE_CACHE_MAX_AGE_SECONDS. No restart is needed.`,
	Args:         cobra.NoArgs,
	RunE:         runAccountAuditMembers,
	SilenceUsage: true,
}

func init() {
	accountAuditMembersCmd.Flags().StringVar(&auditMembersAccount, "account", "",
		"account to audit (default: the instance's first account, the one with the lowest id)")
	accountCmd.AddCommand(accountAuditMembersCmd)
}

func runAccountAuditMembers(cmd *cobra.Command, _ []string) error {
	if err := requireExplicitDSN("account audit-members"); err != nil {
		return err
	}
	dsn := GetConfig().DatabaseDSN
	if path, missing := sqliteFileMissing(dsn); missing {
		// Opening a SQLite path creates the file, and an audit must not leave
		// an empty database behind a mistyped path.
		return fmt.Errorf("no database at %s: point DATABASE_DSN or --database-dsn at the server's database", path)
	}

	// Opened on its own, not through the application module: starting the
	// module runs migrations, installs presets and starts background writers,
	// and a command that changes nothing must do none of that. It is not the
	// server's dialector either: the server's SQLite pragmas switch the file to
	// WAL journal mode, which writes to it. A SQLite file is opened read-only
	// with no pragma.
	db, err := gormlib.Open(gormdb.ReadOnlyDialectorForDSN(dsn), &gormlib.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return fmt.Errorf("failed to open the database (check DATABASE_DSN): %s", redactDSN(rootCause(err).Error(), dsn))
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to open the database (check DATABASE_DSN): %s", redactDSN(rootCause(err).Error(), dsn))
	}
	defer func() {
		// Everything the command needed has been read; a failed close loses
		// nothing and the process is about to exit.
		_ = sqlDB.Close()
	}()

	// Every read runs in one read-only transaction: Postgres refuses a write
	// inside it, and the audit reads one consistent view of the tables.
	tx := db.WithContext(cmd.Context()).Begin(&sql.TxOptions{ReadOnly: true})
	if tx.Error != nil {
		return fmt.Errorf("failed to open the database (check DATABASE_DSN): %s", redactDSN(rootCause(tx.Error).Error(), dsn))
	}
	defer func() {
		// The transaction only read, so rolling it back discards nothing.
		_ = tx.Rollback()
	}()

	if err := printMembershipAudit(cmd, gormdb.ProvideAccountMembershipAudit(tx), auditMembersAccount); err != nil {
		return errors.New(redactDSN(err.Error(), dsn))
	}
	return nil
}

// sqliteFileMissing reports the file a SQLite DSN names when that file does not
// exist. A Postgres DSN and an in-memory database are never missing. In-memory
// is judged as the read-only dialector judges it, so a file whose name only
// contains ":memory:" or "mode=memory" is still checked.
func sqliteFileMissing(dsn string) (string, bool) {
	if config.IsPostgresDSN(dsn) || gormdb.IsSQLiteMemoryDSN(dsn) {
		return "", false
	}
	path := strings.TrimPrefix(dsn, "file:")
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path, true
	}
	return "", false
}

// printMembershipAudit writes the audit of accountID, or of the instance's
// first account when accountID is "".
func printMembershipAudit(cmd *cobra.Command, audit repositories.AccountMembershipAudit, accountID string) error {
	ctx := cmd.Context()
	which := "the account named with --account"
	if accountID == "" {
		first, err := audit.FirstAccountID(ctx)
		if err != nil {
			return fmt.Errorf("could not read the accounts: %w", err)
		}
		if first == "" {
			cmd.Println("This instance has no accounts, so there is nothing to audit.")
			return nil
		}
		accountID, which = first, "the instance's first account"
	}

	report, err := audit.UnexplainedMemberships(ctx, accountID)
	if err != nil {
		return fmt.Errorf("could not audit account %s: %w", accountID, err)
	}
	if !report.AccountFound {
		return fmt.Errorf("there is no account %q on this instance", accountID)
	}

	var writeErr error
	printf := func(w io.Writer, format string, args ...any) {
		if writeErr == nil {
			_, writeErr = fmt.Fprintf(w, format, args...)
		}
	}

	if !report.HistoryFound && report.Memberships > 0 {
		printf(cmd.ErrOrStderr(), "warning: account %s has no recorded history, so a sign-up cannot be told "+
			"from a role the Users page saved; every membership no invite explains is listed.\n", accountID)
	}
	out := cmd.OutOrStdout()
	if len(report.Unexplained) == 0 {
		printf(out, "Account %s (%s): %d memberships, all explained by its history or an invite. Nothing to review.\n",
			accountID, which, report.Memberships)
		return writeErr
	}

	printf(out, "Account %s (%s): %d memberships, %d to review.\n\n",
		accountID, which, report.Memberships, len(report.Unexplained))
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	printf(w, "AGENT ID\tROLE\tWRITTEN AT\tWHY LISTED\n")
	for _, m := range report.Unexplained {
		why := "no sign-up or invite in this account"
		if m.RecordedRoleID != "" {
			why = "role differs from the recorded " + m.RecordedRoleID
		}
		printf(w, "%s\t%s\t%s\t%s\n", m.AgentID, m.RoleID, m.WrittenAt.UTC().Format(time.RFC3339), why)
	}
	if writeErr == nil {
		writeErr = w.Flush()
	}
	printf(out, "\nNothing was changed. To remove a membership or restore its recorded role, "+
		"see: weos account audit-members --help\n")
	return writeErr
}
