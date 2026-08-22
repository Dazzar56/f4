package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

type browser struct {
	app          vfs.App
	session      *databaseSession
	path         string
	tables       []string
	currentTable string
	dialog       *vtui.Window
	tableList    *vtui.ListBox
	resultTable  *vtui.Table
	query        *vtui.MultiLineEdit
	status       *vtui.Text
	closed       bool
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

	if len(b.tables) > 0 {
		b.tableList.SetSelectPos(0)
		b.loadTable(b.tables[0])
	} else {
		b.setStatus(sqliteText("SQLite.NoTables", "The database has no user tables or views.", "В базе нет пользовательских таблиц или представлений."))
	}
	return b
}

func (b *browser) loadTable(table string) {
	if b.closed {
		return
	}
	b.currentTable = table
	b.query.SetText(tableSelect(table))
	var result queryResult
	b.app.RunProgressTask(sqliteText("SQLite.Title", " SQLite ", " SQLite "), fmt.Sprintf(sqliteText("SQLite.ReadingTable", "Reading %s...", "Чтение %s..."), table), false,
		func(ctx context.Context, update func(string, int)) error {
			update(sqliteText("SQLite.ReadingTableProgress", "Reading table...", "Чтение таблицы..."), -1)
			var err error
			result, err = b.session.execute(ctx, tableSelect(table))
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
			b.applyResult(result)
			b.setStatus(fmt.Sprintf(sqliteText("SQLite.TableRows", "%s: %d row(s)", "%s: %d строк(и)"), table, len(result.Rows)))
		})
}

func (b *browser) refresh() {
	if b.closed || b.currentTable == "" {
		return
	}
	b.loadTable(b.currentTable)
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
	var result queryResult
	b.app.RunProgressTask(sqliteText("SQLite.Title", " SQLite ", " SQLite "), sqliteText("SQLite.ExecutingSQL", "Executing SQL...", "Выполнение SQL..."), false,
		func(ctx context.Context, update func(string, int)) error {
			update(sqliteText("SQLite.ExecutingSQL", "Executing SQL...", "Выполнение SQL..."), -1)
			var err error
			result, err = b.session.execute(ctx, statement)
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
			if result.ReturnsRows {
				b.applyResult(result)
				b.setStatus(fmt.Sprintf(sqliteText("SQLite.Rows", "%d row(s)", "%d строк(и)"), len(result.Rows)))
				return
			}
			b.setStatus(fmt.Sprintf(sqliteText("SQLite.StatementCompleted", "Statement completed; %d row(s) affected", "Запрос выполнен; затронуто строк: %d"), result.RowsAffected))
			if b.currentTable != "" {
				b.loadTable(b.currentTable)
			}
		})
}

func (b *browser) applyResult(result queryResult) {
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
