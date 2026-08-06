package vfs

import (
	"context"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"io"
	"os"
	"sync"
	"time"
)

var CustomConfigDir string

// App defines the interface for plugin-to-core UI interactions.
type App interface {
	GetActivePanelVFS() VFS
	GetPassivePanelVFS() VFS
	GetSelectedNames() []string
	GetSelectedName() string
	RefreshAll()
	SetPendingSelection(name string)
	RunProgressTask(title, startMsg string, forked bool, worker func(ctx context.Context, update func(msg string, percent int)) error, onComplete func(err error))
	RunAdvancedProgressTask(title string, forked bool, worker func(ctx context.Context, reporter TaskReporter) error, onComplete func(err error))
	// UI Bridge
	Message(title, msg string, buttons []string) int
	InputBox(title, prompt, history string, callback func(string))
	Menu(title string, items []string, callback func(int))
}

// HostAPI defines the functions f4 exposes to plugins.
type HostAPI interface {
	GetVersion() string
	Log(msg string)
	Message(msg string)

	RegisterHighlighter(p vtui.HighlighterProvider)
	RegisterVFSProvider(p VFSProvider)
	RegisterDrive(name string, factory func() VFS)
	RegisterGlobalHotkey(vk uint16, mods vtinput.ControlKeyState, handler func(app App))
	RegisterPluginMenuItem(label string, handler func(app App))
	RunAction(name string) bool
}

// VFSItem represents a generic file or directory entry.
type VFSItem struct {
	Name         string
	Size         int64
	IsDir        bool
	MTime        time.Time
	Mode         string
	IsExecutable bool
	IsHidden     bool
	// IsSymlink is true when the entry is a filesystem symlink (or a
	// Windows reparse point that Go reports as a symlink). IsDir may
	// still be true for symlink-to-directory — the two flags are
	// orthogonal, and callers that want find/far2l-style "leaf" scan
	// semantics should treat any IsSymlink as a leaf regardless of
	// IsDir. Populated by OSVFS.ReadDir; other VFSes leave it false.
	IsSymlink bool
	// Device / Inode identify the underlying filesystem object so a
	// scanner can dedup hard links (same inode reached through
	// multiple paths in one walk). Both zero means "not populated" —
	// the scanner then simply doesn't dedup, matching prior behaviour.
	// Populated by OSVFS on Unix (stat.Dev/Ino). Windows and remote
	// VFSes leave them zero.
	Device uint64
	Inode  uint64
	// PhysicalSize is the real on-disk footprint of the item (compressed
	// size on NTFS / actual allocated blocks on Unix). Zero means the
	// platform didn't populate it (network VFSes, non-Unix/-Windows).
	// Consumers that display "physical size" should hide the metric
	// entirely when the accumulated total is 0.
	PhysicalSize int64
	// Metadata for Attributes dialog
	ATime    time.Time // Last Access
	CTime    time.Time // Creation (Win) or Status Change (Unix)
	UnixMode uint32    // Raw numeric mode for chmod
	Uid, Gid int       // Ownership
	WinAttrs uint32    // Windows file attributes
}

// VFSCapabilities defines what the current VFS implementation can do efficiently.
type VFSCapabilities struct {
	HasServerSideCopy  bool
	HasServerSideMove  bool
	HasRandomAccess    bool // Supports ReadAt
	HasSearch          bool // Supports server-side search
	HasUnixPermissions bool // Indicates if VFS natively supports Unix-style permissions
}

// VFS is the core interface for file operations in f4.
type VFS interface {
	IsAtRoot() bool
	GetPath() string
	IsAbs(path string) bool
	SetPath(path string) error
	ReadDir(ctx context.Context, path string, onChunk func([]VFSItem)) error
	Stat(ctx context.Context, path string) (VFSItem, error)
	Join(elem ...string) string
	Abs(path string) (string, error)
	Base(path string) string
	Dir(path string) string

	// Mutations
	MkDir(ctx context.Context, path string) error
	Remove(ctx context.Context, path string) error
	Rename(ctx context.Context, oldpath, newpath string) error

	// Advanced / Remote Operations
	GetCapabilities() VFSCapabilities
	Search(ctx context.Context, path string, pattern string) (chan int64, error)

	// Random Access (required for high-performance Viewer/Editor)
	// Open returns a ReadAtCloser for the file.
	Open(ctx context.Context, path string) (ReadAtCloser, error)

	// Create returns a WriteCloser for new files.
	Create(ctx context.Context, path string) (io.WriteCloser, error)

	// SetAttributes updates file metadata (mode, ownership, times)
	SetAttributes(ctx context.Context, path string, item VFSItem) error

	ParentVFS() VFS // Returns the underlying VFS if this is a virtual mount, or nil

	Clone() VFS
	Close() error
}

// TitleProvider allows a VFS to provide a custom display prefix (e.g. "user@host" for network drives).
type TitleProvider interface {
	GetTitle() string
}
type BulkCopier interface {
	CopyBulk(ctx context.Context, srcPaths []string, dstVfs VFS, dstDir string, reporter TaskReporter) error
}
type ArchiveLockManager struct {
	mu    sync.Mutex
	conds map[string]*sync.Cond
	busy  map[string]bool
}

var GlobalArchiveLockManager = &ArchiveLockManager{
	conds: make(map[string]*sync.Cond),
	busy:  make(map[string]bool),
}

func (m *ArchiveLockManager) Lock(path string) {
	m.mu.Lock()
	for m.busy[path] {
		cond, ok := m.conds[path]
		if !ok {
			cond = sync.NewCond(&m.mu)
			m.conds[path] = cond
		}
		cond.Wait()
	}
	m.busy[path] = true
	m.mu.Unlock()
}

func (m *ArchiveLockManager) TryLock(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[path] {
		return false
	}
	m.busy[path] = true
	return true
}

func (m *ArchiveLockManager) Unlock(path string) {
	m.mu.Lock()
	m.busy[path] = false
	if cond, ok := m.conds[path]; ok {
		cond.Broadcast()
	}
	m.mu.Unlock()
}

// PtyProvider allows a VFS to provide its own PTY implementation
// (e.g. an SSH session for remote systems).
type PtyProvider interface {
	OpenPty(cols, rows int) (any, error)
}

// VFSProvider умеет определять, может ли он открыть путь, и создавать экземпляр VFS.
type VFSProvider interface {
	Name() string
	// Priority: чем выше, тем раньше провайдер опрашивается (архивы обычно имеют низкий приоритет)
	Priority() int
	// CanOpen возвращает true, если провайдер понимает этот путь.
	// parent — текущая VFS, в которой находится объект.
	CanOpen(ctx context.Context, parent VFS, path string) bool
	// Open создает новый экземпляр VFS.
	Open(ctx context.Context, parent VFS, path string) (VFS, error)
}

var providers []VFSProvider

func RegisterProvider(p VFSProvider) {
	providers = append(providers, p)
	// Сортируем по приоритету
}

func FindProvider(ctx context.Context, parent VFS, path string) VFSProvider {
	for _, p := range providers {
		if p.CanOpen(ctx, parent, path) {
			return p
		}
	}
	return nil
}

// ReadAtCloser combines reader interfaces with context support.
// CommandRunner is implemented by a file system that can run a command where
// its files are. For a remote one that is the difference between downloading
// a tree to grep it and asking the host a question; for a local one there is
// nothing to implement, since a shell is already there.
type CommandRunner interface {
	// RunCommand runs the command line in dir and hands each line of its
	// output to cb as it arrives, returning the exit status. A non-zero
	// status is not an error: the command ran and said something.
	RunCommand(ctx context.Context, dir, command string, cb func(line string)) (int, error)
}

// DuplicateProgress reports how far a duplicate search has got. Total is how
// many files turned out to be worth reading at all, which is known only once
// the tree has been walked, so it is not the number of files in it.
type DuplicateProgress struct {
	Done  int
	Total int
	Path  string
}

// DuplicateFinder is implemented by a file system that can find files with
// identical content on its own side. Doing it from here means reading every
// candidate across the network, which for a remote tree costs more than the
// answer is worth; a file system that cannot do it simply does not offer the
// command. Each group holds two or more paths with the same content.
type DuplicateFinder interface {
	FindDuplicates(ctx context.Context, dir string, cb func(DuplicateProgress)) ([][]string, error)
}

// PatchPiece is one piece of a file being assembled. Data set means literal
// new bytes; Data nil means Length bytes taken from the existing file at
// Offset, which then never have to travel.
type PatchPiece struct {
	Offset int64
	Length int64
	Data   []byte
}

// DeltaWriter is implemented by a file system that can build a file out of
// pieces of another one on its own side. An editor saving a one byte change
// in a large remote file then sends one byte rather than the file. Like the
// other optional interfaces here, a caller that does not find it writes the
// file out in full as before.
type DeltaWriter interface {
	PatchFile(ctx context.Context, src, dst string, pieces []PatchPiece) error
}

// FoundEntry is one hit of a tree search.
type FoundEntry struct {
	// Path is the full path of the file, in the file system's own notation.
	Path string
	// Item describes it the way a listing would.
	Item VFSItem
}

// FindQuery describes a tree search.
type FindQuery struct {
	// Masks are shell globs matched against the file name; at least one.
	Masks []string
	// Text, when set, keeps only files containing it as a plain string.
	Text string
	// IgnoreCase folds case for Text.
	IgnoreCase bool
	// Limit caps the number of hits; zero leaves it to the file system.
	Limit int
}

// FileFinder is implemented by a file system that can walk a tree on its own
// side. Like LineIndexer it is an optional interface: a local file system is
// no faster for it, so only the ones that gain from it carry it, and a
// caller that does not find it walks the tree itself as before.
type FileFinder interface {
	FindFiles(ctx context.Context, dir string, q FindQuery) ([]FoundEntry, error)
}

// LineIndexResult is what a LineIndexer answers with.
type LineIndexResult struct {
	// First is the one-based number of the line Offsets[0] belongs to.
	First int64
	// Offsets holds the byte offset of each line start, in file order. It is
	// shorter than requested when the file ends first.
	Offsets []int64
	// Total is the number of lines in the file.
	Total int64
}

// LineIndexer is implemented by a file system that can have the far side
// work out where lines begin, so that a viewer does not have to read a file
// in order to count it. It is deliberately an optional interface rather than
// a method on VFS: a local file system gains nothing from it, and an archive
// cannot answer it at all, so neither should be made to carry it. A caller
// type asserts for it and keeps its own behaviour when the assertion fails.
type LineIndexer interface {
	LineIndex(ctx context.Context, path string, first, count int64) (LineIndexResult, error)
}

// SessionReconnector is implemented by a file system that lives on a
// connection and can rebuild it. It is optional for the same reason
// LineIndexer is: a local file system has no session to lose, and one that
// was handed a stream it cannot open a second time has nothing to rebuild
// with, so neither should be made to carry it.
//
// Reconnecting is deliberately not done inside a failing request. A request
// that reconnected on its own would turn one failure into a delay of unknown
// length in the middle of an operation, with no way for the user to say no.
// What the interface offers instead is the three questions a caller that met
// the failure needs answered: was the session lost, can it be rebuilt, and
// rebuild it.
type SessionReconnector interface {
	// SessionLost reports whether an error means the connection died rather
	// than the operation itself failing. A missing file is not a lost session
	// and must not be answered as one.
	SessionLost(err error) bool
	// CanReconnect reports whether a new connection can be built. A caller
	// asks before offering the user a choice it cannot honour.
	CanReconnect() bool
	// Reconnect builds it. What survives is what lives on this side; anything
	// the far side was doing is gone, which is why the caller decides what to
	// retry rather than this method retrying anything itself.
	Reconnect(ctx context.Context) error
}
type ReadAtCloser interface {
	ReadAt(ctx context.Context, p []byte, off int64) (n int, err error)
	Read(ctx context.Context, p []byte) (n int, err error)
	io.Closer
	Size() int64
} // TempFileWrapper is a helper for VFS that need to extract files to temp storage.
type TempFileWrapper struct {
	*os.File
	SizeVal  int64
	TempPath string
}

func (w *TempFileWrapper) Size() int64 { return w.SizeVal }
func (w *TempFileWrapper) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	return w.File.ReadAt(p, off)
}
func (w *TempFileWrapper) Read(ctx context.Context, p []byte) (int, error) {
	return w.File.Read(p)
}

func (w *TempFileWrapper) Close() error {
	err := w.File.Close()
	os.Remove(w.TempPath)
	return err
}

type progressKeyType struct{}
type reporterKeyType struct{}

var ProgressKey = progressKeyType{}
var ReporterKey = reporterKeyType{}

type ProgressCallback func(msg string, percent int)

type TaskReporter interface {
	UpdateScan(currentPath string, files, dirs int64)
	UpdateTransfer(action string, filename string, currentPct int, totalText string, totalPct int, speedText string)
	IsCancelled() bool
}

type FileProgress interface {
	StartFile(name string, size int64)
	UpdateBytes(n int)
	FileDone()
	DirDone()
	FileSkipped()
}
