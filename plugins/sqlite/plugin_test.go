package sqlite

import (
	"context"
	"path/filepath"
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
	if host.command.ID != sqliteCommandID || host.command.Location != vfs.PluginCommandPanel || host.command.Run == nil {
		t.Fatalf("command metadata = %#v", host.command)
	}
	// The main-menu row belongs to the host action App.SQLite, and the
	// command is offered wherever the cursor happens to be: a predicate on
	// the panel selection took the entry out of the menus that lead to it.
	if host.command.MenuPath != "" || host.command.Visible != nil {
		t.Fatalf("command hides itself: MenuPath = %q, Visible set = %t", host.command.MenuPath, host.command.Visible != nil)
	}
	if host.command.Label != "SQLite client" || host.command.LabelKey != "SQLite.Command.Open" || host.command.DescriptionKey != "SQLite.Command.Open.Desc" {
		t.Fatalf("localization metadata = %#v", host.command)
	}
	if err := plugin.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabasePathIsResolvedAgainstThePanel(t *testing.T) {
	dir := t.TempDir()
	app := &sqliteTestApp{fs: vfs.NewOSVFS(dir)}
	if err := app.fs.SetPath(dir); err != nil {
		t.Fatal(err)
	}

	got := databasePathIn(app, "new.sqlite")
	if want := filepath.Join(dir, "new.sqlite"); got != want {
		t.Fatalf("databasePathIn = %q, want %q", got, want)
	}

	absolute := filepath.Join(dir, "elsewhere.sqlite")
	if got := databasePathIn(app, absolute); got != absolute {
		t.Fatalf("databasePathIn(absolute) = %q, want %q", got, absolute)
	}

	// A database the client created is a database it can open again.
	session, tables, err := openDatabase(context.Background(), filepath.Join(dir, "new.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if len(tables) != 0 {
		t.Fatalf("a fresh database reported tables: %#v", tables)
	}
	if _, err := session.execute(context.Background(), `CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	tables, err = session.listTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0] != "t" {
		t.Fatalf("tables after CREATE TABLE = %#v", tables)
	}
}

// sqliteTestApp is the slice of vfs.App this plugin reads: the active panel.
type sqliteTestApp struct {
	vfs.App
	fs *vfs.OSVFS
}

func (a *sqliteTestApp) GetActivePanelVFS() vfs.VFS { return a.fs }

func TestBrowseTableCarriesRowIDsAndEditsACell(t *testing.T) {
	path := t.TempDir() + "/edit.db"
	db, err := driver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, note TEXT, size INTEGER)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notes VALUES (1, 'first', 10), (2, NULL, 20)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE VIEW big AS SELECT * FROM notes WHERE size > 5`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	session, _, err := openDatabase(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	result, rowIDs, writable, err := session.browseTable(context.Background(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	// The rowid alias is the browser's business and never reaches the screen.
	if !reflect.DeepEqual(result.Columns, []string{"id", "note", "size"}) {
		t.Fatalf("columns = %#v", result.Columns)
	}
	if !reflect.DeepEqual(rowIDs, []int64{1, 2}) || !writable {
		t.Fatalf("rowIDs = %#v, writable = %t", rowIDs, writable)
	}

	// A view has no rowid: it comes back readable and unwritable.
	viewResult, viewRowIDs, viewWritable, err := session.browseTable(context.Background(), "big")
	if err != nil {
		t.Fatal(err)
	}
	if viewRowIDs != nil || viewWritable || len(viewResult.Rows) != 2 {
		t.Fatalf("view browse = %d row(s), rowIDs %#v, writable %t", len(viewResult.Rows), viewRowIDs, viewWritable)
	}

	affected, err := session.updateCell(context.Background(), "notes", "note", 2, "second")
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("updateCell affected %d rows, want 1", affected)
	}
	value, err := session.cellValue(context.Background(), "notes", "note", 2)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := editableText(value); !ok || text != "second" {
		t.Fatalf("cell after the edit = %q (editable %t), want %q", text, ok, "second")
	}

	// Column affinity turns a typed number back into one.
	if _, err := session.updateCell(context.Background(), "notes", "size", 2, "42"); err != nil {
		t.Fatal(err)
	}
	value, err = session.cellValue(context.Background(), "notes", "size", 2)
	if err != nil {
		t.Fatal(err)
	}
	if number, ok := value.(int64); !ok || number != 42 {
		t.Fatalf("size after the edit = %#v, want int64(42)", value)
	}
}

func TestEditableTextRefusesWhatALineBoxWouldCorrupt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    any
		want     string
		editable bool
	}{
		{"NULL edits as an empty line", nil, "", true},
		{"text passes through", "hello", "hello", true},
		{"a number is written out", int64(42), "42", true},
		{"binary is refused", []byte{0, 1, 255}, "", false},
		{"line breaks are refused", "two\nlines", "", false},
	} {
		got, editable := editableText(tc.value)
		if got != tc.want || editable != tc.editable {
			t.Errorf("%s: editableText(%#v) = %q, %t; want %q, %t", tc.name, tc.value, got, editable, tc.want, tc.editable)
		}
	}
}

func TestInsertRowAddsADefaultRowAndReportsRefusals(t *testing.T) {
	path := t.TempDir() + "/insert.db"
	db, err := driver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, note TEXT)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE strict (id INTEGER PRIMARY KEY, note TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	session, _, err := openDatabase(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	// An empty table is still writable: that is where a first row is wanted.
	result, rowIDs, writable, err := session.browseTable(context.Background(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	if !writable || len(rowIDs) != 0 || len(result.Rows) != 0 {
		t.Fatalf("empty browse = %d row(s), rowIDs %#v, writable %t", len(result.Rows), rowIDs, writable)
	}

	rowID, err := session.insertRow(context.Background(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	_, rowIDs, _, err = session.browseTable(context.Background(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rowIDs, []int64{rowID}) {
		t.Fatalf("rowIDs after the insert = %#v, want [%d]", rowIDs, rowID)
	}
	value, err := session.cellValue(context.Background(), "notes", "note", rowID)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatalf("the new row's note = %#v, want NULL", value)
	}

	// NOT NULL without a default cannot take a row of defaults, and the error
	// is what the user is shown instead of a guess at the value.
	if _, err := session.insertRow(context.Background(), "strict"); err == nil {
		t.Fatal("a NOT NULL column accepted a default row")
	}
}
