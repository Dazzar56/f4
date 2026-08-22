package sqlite

import (
	"context"
	"reflect"
	"testing"

	"github.com/ncruces/go-sqlite3/driver"
	"github.com/unxed/f4/vfs"
)

func TestDatabaseSessionReadsAndWritesSQLiteValues(t *testing.T) {
	path := t.TempDir() + "/sample.db"
	db, err := driver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE "odd""name" (id INTEGER, note TEXT, payload BLOB)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "odd""name" VALUES (1, NULL, ?)`, []byte{0, 1, 255}); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	session, tables, err := openDatabase(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !reflect.DeepEqual(tables, []string{`odd"name`}) {
		t.Fatalf("tables = %#v, want [odd\\\"name]", tables)
	}

	result, err := session.execute(context.Background(), tableSelect(`odd"name`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.ReturnsRows || !reflect.DeepEqual(result.Columns, []string{"id", "note", "payload"}) {
		t.Fatalf("query metadata = %#v", result)
	}
	wantRows := [][]string{{"1", "NULL", "x'0001ff'"}}
	if !reflect.DeepEqual(result.Rows, wantRows) {
		t.Fatalf("rows = %#v, want %#v", result.Rows, wantRows)
	}

	result, err = session.execute(context.Background(), `UPDATE "odd""name" SET note = 'changed' WHERE id = 1`)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReturnsRows || result.RowsAffected != 1 {
		t.Fatalf("write result = %#v", result)
	}
}

func TestSQLiteSQLClassificationAndIdentifierQuoting(t *testing.T) {
	if got, want := tableSelect(`a"b`), `SELECT * FROM "a""b" LIMIT 100`; got != want {
		t.Fatalf("tableSelect = %q, want %q", got, want)
	}
	for _, statement := range []string{
		"-- explain the query\n SELECT 1",
		"/* leading comment */ PRAGMA user_version",
		"WITH rows AS (SELECT 1) SELECT * FROM rows",
	} {
		if !statementReturnsRows(statement) {
			t.Errorf("statementReturnsRows(%q) = false", statement)
		}
	}
	for _, statement := range []string{"CREATE TABLE x (id INTEGER)", "UPDATE x SET id = 1"} {
		if statementReturnsRows(statement) {
			t.Errorf("statementReturnsRows(%q) = true", statement)
		}
	}
}

type sqliteTestRegistration struct{}

func (*sqliteTestRegistration) Unregister() {}

type sqliteTestHost struct {
	vfs.HostAPI
	vfs.ContributionHost
	command vfs.PluginCommand
}

func (host *sqliteTestHost) RegisterPluginCommand(command vfs.PluginCommand) (vfs.Registration, error) {
	host.command = command
	return &sqliteTestRegistration{}, nil
}

func TestPluginRegistersLocalizedPanelCommand(t *testing.T) {
	host := &sqliteTestHost{}
	plugin := NewPlugin()
	if err := plugin.Init(host); err != nil {
		t.Fatal(err)
	}
	if host.command.ID != sqliteCommandID || host.command.Location != vfs.PluginCommandPanel || host.command.MenuPath != "Files" || host.command.Run == nil {
		t.Fatalf("command metadata = %#v", host.command)
	}
	if host.command.Label != "SQLite client" || host.command.LabelKey != "SQLite.Command.Open" || host.command.DescriptionKey != "SQLite.Command.Open.Desc" {
		t.Fatalf("localization metadata = %#v", host.command)
	}
	if err := plugin.Close(); err != nil {
		t.Fatal(err)
	}
}
