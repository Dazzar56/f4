package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// browserWindow is the dialog with one key of its own.
type browserWindow struct {
	*vtui.Window
	browser *browser
}

// ProcessKey makes F9 do what the label above the SQL box has always claimed
// it does. Nothing handled the key here, so it fell through to the frame
// manager, which has its own use for F9 and no idea about this dialog: the
// statement stayed where it was typed and the only way to run it was to tab
// to the button.
func (w *browserWindow) ProcessKey(e *vtinput.InputEvent) bool {
	if e.KeyDown && e.VirtualKeyCode == vtinput.VK_F9 {
		w.browser.runQuery()
		return true
	}
	if e.KeyDown && e.VirtualKeyCode == vtinput.VK_F4 {
		w.browser.editCell()
		return true
	}
	if e.KeyDown && e.VirtualKeyCode == vtinput.VK_INSERT {
		w.browser.insertRow()
		return true
	}
	return w.Window.ProcessKey(e)
}

type browser struct {
	app          vfs.App
	session      *databaseSession
	path         string
	tables       []string
	currentTable string
	columns      []string
	rowIDs       []int64
	writable     bool
	// selectRowID moves the cursor onto a row once the table it belongs to
	// has been read again, which is how a newly inserted row is found.
	selectRowID int64
	dialog      *vtui.Window
	frame       *browserWindow
	tableList   *vtui.ListBox
	resultTable *vtui.Table
	query       *vtui.MultiLineEdit
	status      *vtui.Text
	closed      bool
}

func newBrowser(app vfs.App, session *databaseSession, tables []string) *browser {
	width, height := 110, 32
	if vtui.FrameManager != nil {
		if maxWidth := vtui.FrameManager.GetScreenSize() - 2; maxWidth > 20 && width > maxWidth {
			width = maxWidth
		}
		if maxHeight := vtui.FrameManager.GetScreenHeight() - 2; maxHeight > 18 && height > maxHeight {
			height = maxHeight
		}
	}
	if width < 72 {
		width = 72
	}
	if height < 20 {
		height = 20
	}

	b := &browser{app: app, session: session, path: session.path, tables: append([]string(nil), tables...)}
	b.dialog = vtui.NewCenteredDialog(width, height, " "+sqliteText("SQLite.Title", "SQLite", "SQLite")+": "+filepath.Base(session.path)+" ")
	b.dialog.ShowClose = true

	leftX := b.dialog.X1 + 2
	topY := b.dialog.Y1 + 2
	leftWidth := 24
	rightX := leftX + leftWidth + 2
	rightWidth := b.dialog.X2 - rightX - 1
	dataHeight := height - 13
	if dataHeight < 3 {
		dataHeight = 3
	}

	b.tableList = vtui.NewListBox(leftX, topY+1, leftWidth, dataHeight, b.tables)
	useDialogListColors(b.tableList)
	b.resultTable = vtui.NewTable(rightX, topY+1, rightWidth, dataHeight, nil)
	b.resultTable.CellSelection = true
	b.resultTable.ShowHeader = true
	b.resultTable.ShowSeparators = true
	b.resultTable.ColorTextIdx = vtui.ColDialogText
	b.resultTable.ColorSelectedTextIdx = vtui.ColDialogSelectedButton
	b.resultTable.ColorTitleIdx = vtui.ColDialogHighlightText
	b.resultTable.ColorBoxIdx = vtui.ColDialogBox

	b.dialog.AddItem(vtui.NewText(leftX, topY, sqliteText("SQLite.TablesViews", "Tables / views", "Таблицы / представления"), 0))
	b.dialog.AddItem(vtui.NewText(rightX, topY, sqliteText("SQLite.ResultLimit", "Result (maximum 100 rows)", "Результат (не более 100 строк)"), 0))
	b.dialog.AddItem(b.tableList)
	b.dialog.AddItem(b.resultTable)

	queryY := b.dialog.Y2 - 7
	b.dialog.AddItem(vtui.NewText(leftX, queryY-1, sqliteText("SQLite.SQLHint", "SQL (F9 or Run)", "SQL (F9 или Выполнить)"), 0))
	b.query = vtui.NewMultiLineEdit(leftX, queryY, b.dialog.X2-leftX-1, 3, "SELECT 1")
	b.query.SetGrowMode(vtui.GrowHiX | vtui.GrowHiY)
	b.dialog.AddItem(b.query)

	b.status = vtui.NewText(leftX, b.dialog.Y2-4, "", 0)
	b.dialog.AddItem(b.status)

	runButton := vtui.NewButton(0, 0, sqliteText("SQLite.Run", "&Run", "&Выполнить"))
	refreshButton := vtui.NewButton(0, 0, sqliteText("SQLite.Refresh", "&Refresh", "&Обновить"))
	closeButton := vtui.NewButton(0, 0, sqliteText("SQLite.Close", "&Close", "&Закрыть"))
	runButton.IsDefault = true
	runButton.OnClick = b.runQuery
	refreshButton.OnClick = b.refresh
	closeButton.OnClick = b.dialog.Close
	b.dialog.AddItem(runButton)
	b.dialog.AddItem(refreshButton)
	b.dialog.AddItem(closeButton)
	buttonLayout := vtui.NewHBoxLayout(b.dialog.X1+2, b.dialog.Y2-2, width-4, 1)
	buttonLayout.HorizontalAlign = vtui.AlignCenter
	buttonLayout.Spacing = 2
	buttonLayout.Add(runButton, vtui.Margins{}, vtui.AlignTop)
	buttonLayout.Add(refreshButton, vtui.Margins{}, vtui.AlignTop)
	buttonLayout.Add(closeButton, vtui.Margins{}, vtui.AlignTop)
	buttonLayout.Apply()
	runButton.SetGrowMode(vtui.GrowAll)
	refreshButton.SetGrowMode(vtui.GrowAll)
	closeButton.SetGrowMode(vtui.GrowAll)

	b.tableList.OnSelect = func(index int) {
		if index >= 0 && index < len(b.tables) {
			b.loadTable(b.tables[index])
		}
	}
	b.tableList.OnAction = func(index int) {
		if index >= 0 && index < len(b.tables) {
			b.loadTable(b.tables[index])
		}
	}
	b.dialog.OnResult = func(int) {
		if b.closed {
			return
		}
		b.closed = true
		b.session.Close()
	}

	b.frame = &browserWindow{Window: b.dialog, browser: b}

	if len(b.tables) > 0 {
		b.tableList.SetSelectPos(0)
		b.loadTable(b.tables[0])
	} else {
		b.setStatus(sqliteText("SQLite.NoTables", "The database has no user tables or views.", "В базе нет пользовательских таблиц или представлений."))
	}
	return b
}

// loadTable reads a table into the right hand side.
func (b *browser) loadTable(table string) {
	if b.closed {
		return
	}
	b.currentTable = table
	b.query.SetText(tableSelect(table))
	var browse tableBrowse
	b.app.RunProgressTask(sqliteText("SQLite.Title", " SQLite ", " SQLite "), fmt.Sprintf(sqliteText("SQLite.ReadingTable", "Reading %s...", "Чтение %s..."), table), false,
		func(ctx context.Context, update func(string, int)) error {
			update(sqliteText("SQLite.ReadingTableProgress", "Reading table...", "Чтение таблицы..."), -1)
			var err error
			browse, err = b.session.browseTable(ctx, table)
			return err
		},
		func(err error) {
			if b.closed || b.currentTable != table {
				return
			}
			if err != nil {
				b.setStatus(fmt.Sprintf(sqliteText("SQLite.ReadFailed", "Could not read %s: %v", "Не удалось прочитать %s: %v"), table, err))
				return
			}
			b.applyBrowse(table, browse)
			b.setStatus(fmt.Sprintf(sqliteText("SQLite.TableRows", "%s: %d row(s)", "%s: %d строк(и)"), table, len(browse.result.Rows)))
			b.takePendingSelection()
		})
}

// refresh re-reads the schema and then the table being shown.
//
// It used to reload the current table and nothing else, so on a database whose
// tables had changed -- and on a new one, where there is no current table at
// all -- the button did nothing whatsoever, and the only way to see a table
// that had just been created was to close the client and open it again.
func (b *browser) refresh() {
	if b.closed {
		return
	}
	current := b.currentTable
	var (
		tables []string
		table  string
		browse tableBrowse
	)
	b.app.RunProgressTask(sqliteText("SQLite.Title", " SQLite ", " SQLite "), sqliteText("SQLite.ReadingSchema", "Reading database schema...", "Чтение схемы базы данных..."), false,
		func(ctx context.Context, update func(string, int)) error {
			update(sqliteText("SQLite.ReadingSchema", "Reading database schema...", "Чтение схемы базы данных..."), -1)
			var err error
			if tables, err = b.session.listTables(ctx); err != nil {
				return err
			}
			table = tableToShow(tables, current)
			if table == "" {
				return nil
			}
			browse, err = b.session.browseTable(ctx, table)
			return err
		},
		func(err error) {
			if b.closed {
				return
			}
			if err != nil {
				b.setStatus(fmt.Sprintf(sqliteText("SQLite.SQLError", "SQL error: %v", "Ошибка SQL: %v"), err))
				return
			}
			b.setTables(tables)
			if table == "" {
				b.applyResult(queryResult{})
				b.setStatus(sqliteText("SQLite.NoTables", "The database has no user tables or views.", "В базе нет пользовательских таблиц или представлений."))
				return
			}
			b.selectTableInList(table)
			b.applyBrowse(table, browse)
			b.setStatus(fmt.Sprintf(sqliteText("SQLite.TableRows", "%s: %d row(s)", "%s: %d строк(и)"), table, len(browse.result.Rows)))
		})
}

// tableToShow keeps the table the user was looking at when it is still there,
// and otherwise falls back to the first one.
func tableToShow(tables []string, current string) string {
	for _, table := range tables {
		if table == current {
			return current
		}
	}
	if len(tables) > 0 {
		return tables[0]
	}
	return ""
}

func (b *browser) runQuery() {
	if b.closed {
		return
	}
	statement := strings.TrimSpace(b.query.GetText())
	if statement == "" {
		b.setStatus(sqliteText("SQLite.EmptySQL", "SQL statement is empty", "SQL-запрос пуст."))
		return
	}
	current := b.currentTable
	var (
		result queryResult
		tables []string
		table  string
		browse tableBrowse
	)
	b.app.RunProgressTask(sqliteText("SQLite.Title", " SQLite ", " SQLite "), sqliteText("SQLite.ExecutingSQL", "Executing SQL...", "Выполнение SQL..."), false,
		func(ctx context.Context, update func(string, int)) error {
			update(sqliteText("SQLite.ExecutingSQL", "Executing SQL...", "Выполнение SQL..."), -1)
			var err error
			if result, err = b.session.execute(ctx, statement); err != nil {
				return err
			}
			if result.ReturnsRows {
				return nil
			}
			// CREATE, DROP and ALTER change what the list on the left is
			// showing, and an INSERT or an UPDATE changes what the right hand
			// side is showing. Both are re-read here, on the worker that ran
			// the statement.
			if tables, err = b.session.listTables(ctx); err != nil {
				return err
			}
			if table = tableToShow(tables, current); table == "" {
				return nil
			}
			browse, err = b.session.browseTable(ctx, table)
			return err
		},
		func(err error) {
			if b.closed {
				return
			}
			if err != nil {
				message := fmt.Sprintf(sqliteText("SQLite.SQLError", "SQL error: %v", "Ошибка SQL: %v"), err)
				b.setStatus(message)
				// A rejected statement has to say so. Truncated into the
				// status line it reads like nothing happened at all, which is
				// exactly what a typo in the SQL box looked like: the file
				// stays empty, the list stays empty, and nobody is told why.
				// SQLite names what it did not like; put that in front of the
				// user and leave the statement in the box to be fixed.
				vtui.ShowMessageOn(b.frame,
					sqliteText("SQLite.Title", " SQLite ", " SQLite "),
					message,
					[]string{sqliteText("SQLite.OK", "&OK", "&ОК")})
				return
			}
			if result.ReturnsRows {
				// The rows of a hand written query are not tied to any one
				// table, so nothing here can be written back.
				b.rowIDs = nil
				b.writable = false
				b.applyResult(result)
				b.setStatus(fmt.Sprintf(sqliteText("SQLite.Rows", "%d row(s)", "%d строк(и)"), len(result.Rows)))
				return
			}
			b.setTables(tables)
			b.setStatus(fmt.Sprintf(sqliteText("SQLite.StatementCompleted", "Statement completed; %d row(s) affected", "Запрос выполнен; затронуто строк: %d"), result.RowsAffected))
			if table == "" {
				return
			}
			// A CREATE TABLE on a database that had nothing in it shows what
			// was just made instead of an empty right hand side.
			b.selectTableInList(table)
			b.applyBrowse(table, browse)
		})
}
func (b *browser) setTables(tables []string) {
	b.tables = tables
	b.tableList.Items = tables
	b.tableList.UpdateRows()
	if len(tables) == 0 {
		b.currentTable = ""
		return
	}
	if b.tableList.SelectPos >= len(tables) {
		b.tableList.SetSelectPos(0)
	}
	for _, table := range tables {
		if table == b.currentTable {
			return
		}
	}
	b.currentTable = ""
}

func (b *browser) applyResult(result queryResult) {
	b.columns = result.Columns
	if b.resultTable.SelectCol >= len(result.Columns) {
		b.resultTable.SelectCol = 0
	}
	columns := make([]vtui.TableColumn, len(result.Columns))
	for index, column := range result.Columns {
		columns[index] = vtui.TableColumn{Title: column, Width: 0, MinWidth: 8}
	}
	rows := make([]vtui.TableRow, len(result.Rows))
	for index, cells := range result.Rows {
		rows[index] = resultRow{cells: cells}
	}
	b.resultTable.Columns = columns
	b.resultTable.SetRows(rows)
}

// editCell edits the cell under the cursor and writes it back.
//
// Only a table browse can be edited: the rows of a hand written query are not
// tied to a table, and a view or a WITHOUT ROWID table has no rowid to write
// against. The value is read again before editing rather than taken off the
// screen, because what is on screen is escaped and cut at 512 characters.
func (b *browser) editCell() {
	if b.closed {
		return
	}
	column, rowID, ok := b.cellUnderCursor()
	if !ok {
		b.setStatus(sqliteText("SQLite.NotEditable",
			"Only a table from the list on the left can be edited, and only one with rowids.",
			"Редактировать можно только таблицу из списка слева, и только имеющую rowid."))
		return
	}

	var value any
	b.app.RunProgressTask(sqliteText("SQLite.Title", " SQLite ", " SQLite "), sqliteText("SQLite.ReadingValue", "Reading the value...", "Чтение значения..."), false,
		func(ctx context.Context, update func(string, int)) error {
			update(sqliteText("SQLite.ReadingValue", "Reading the value...", "Чтение значения..."), -1)
			var err error
			value, err = b.session.cellValue(ctx, b.currentTable, column, rowID)
			return err
		},
		func(err error) {
			if b.closed {
				return
			}
			if err != nil {
				b.setStatus(fmt.Sprintf(sqliteText("SQLite.SQLError", "SQL error: %v", "Ошибка SQL: %v"), err))
				return
			}
			text, editable := editableText(value)
			if !editable {
				b.setStatus(sqliteText("SQLite.CellNotEditable",
					"This cell holds binary data or line breaks; change it with SQL.",
					"В ячейке двоичные данные или переводы строк; измените её запросом SQL."))
				return
			}
			b.app.InputBox(
				sqliteText("SQLite.Title", " SQLite ", " SQLite "),
				fmt.Sprintf(sqliteText("SQLite.EditPrompt", "New value for %s:", "Новое значение для %s:"), column),
				text,
				func(answer string) {
					// Unchanged means unchanged. Pressing Enter over a NULL
					// cell must not quietly turn it into an empty string.
					if answer == text {
						return
					}
					b.writeCell(column, rowID, answer)
				})
		})
}

// cellUnderCursor resolves the table cursor to a column and a rowid.
func (b *browser) cellUnderCursor() (column string, rowID int64, ok bool) {
	if !b.writable || b.currentTable == "" || len(b.rowIDs) == 0 {
		return "", 0, false
	}
	row, col := b.resultTable.SelectPos, b.resultTable.SelectCol
	if row < 0 || row >= len(b.rowIDs) || col < 0 || col >= len(b.columns) {
		return "", 0, false
	}
	return b.columns[col], b.rowIDs[row], true
}

// insertRow adds a row of defaults to the table being browsed and puts the
// cursor on it, ready for F4.
func (b *browser) insertRow() {
	if b.closed {
		return
	}
	if !b.writable || b.currentTable == "" {
		b.setStatus(sqliteText("SQLite.NotEditable",
			"Only a table from the list on the left can be edited, and only one with rowids.",
			"Редактировать можно только таблицу из списка слева, и только имеющую rowid."))
		return
	}
	table := b.currentTable
	var (
		rowID  int64
		browse tableBrowse
	)
	b.app.RunProgressTask(sqliteText("SQLite.Title", " SQLite ", " SQLite "), sqliteText("SQLite.AddingRow", "Adding a row...", "Добавление строки..."), false,
		func(ctx context.Context, update func(string, int)) error {
			update(sqliteText("SQLite.AddingRow", "Adding a row...", "Добавление строки..."), -1)
			var err error
			if rowID, err = b.session.insertRow(ctx, table); err != nil {
				return err
			}
			browse, err = b.session.browseTable(ctx, table)
			return err
		},
		func(err error) {
			if b.closed || b.currentTable != table {
				return
			}
			if err != nil {
				// SQLite names the column that would not take a default, and
				// that is the whole answer to why the row was refused.
				message := fmt.Sprintf(sqliteText("SQLite.SQLError", "SQL error: %v", "Ошибка SQL: %v"), err)
				b.setStatus(message)
				vtui.ShowMessageOn(b.frame,
					sqliteText("SQLite.Title", " SQLite ", " SQLite "),
					message,
					[]string{sqliteText("SQLite.OK", "&OK", "&ОК")})
				return
			}
			b.applyBrowse(table, browse)
			b.selectRowID = rowID
			b.takePendingSelection()
		})
}
func (b *browser) takePendingSelection() {
	rowID := b.selectRowID
	b.selectRowID = 0
	if rowID == 0 {
		return
	}
	for index, candidate := range b.rowIDs {
		if candidate != rowID {
			continue
		}
		b.resultTable.SetSelectPos(index)
		b.resultTable.SelectCol = 0
		b.setStatus(sqliteText("SQLite.RowAdded", "Row added; fill it in with F4.", "Строка добавлена; заполните её по F4."))
		return
	}
	b.setStatus(sqliteText("SQLite.RowAddedBeyondLimit",
		"Row added, past the 100 rows shown here.",
		"Строка добавлена, но она за пределами показанных 100 строк."))
}

func (b *browser) setStatus(message string) {
	if b.status != nil {
		b.status.SetText(vtui.TruncateMiddle(message, b.dialog.X2-b.dialog.X1-4))
	}
}

type resultRow struct{ cells []string }

func (row resultRow) GetCellText(column int) string {
	if column < 0 || column >= len(row.cells) {
		return ""
	}
	return row.cells[column]
}

func useDialogListColors(list *vtui.ListBox) {
	list.ColorTextIdx = vtui.ColDialogText
	list.ColorSelectedTextIdx = vtui.ColDialogSelectedButton
	list.ColorItemSelectTextIdx = vtui.ColDialogHighlightText
	list.ColorItemSelectCursorIdx = vtui.ColDialogHighlightSelectedButton
	list.ColorTitleIdx = vtui.ColDialogHighlightText
	list.ColorBoxIdx = vtui.ColDialogBox
	if list.ScrollBar != nil {
		list.ScrollBar.ColorIdx = vtui.ColDialogBox
	}
}
