package netfox

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"github.com/unxed/f4/vfs"
)

type NetFoxConfig struct {
	Type string `json:"Type"`
	Host string `json:"Host"`
	Port string `json:"Port"`
	User string `json:"User"`
	Pass string `json:"Pass"`
}

type NetFoxVFS struct {
	path string
}

func NewNetFoxVFS(dbPath string) *NetFoxVFS {
	os.MkdirAll(filepath.Dir(dbPath), 0755)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		os.WriteFile(dbPath, []byte("{}"), 0644)
	}
	return &NetFoxVFS{path: dbPath}
}

func (v *NetFoxVFS) getConfigs() map[string]vfs.NetFoxConfig {
	data, _ := os.ReadFile(v.path)
	var configs map[string]vfs.NetFoxConfig
	json.Unmarshal(data, &configs)
	if configs == nil { configs = make(map[string]vfs.NetFoxConfig) }
	return configs
}

func (v *NetFoxVFS) saveConfigs(configs map[string]vfs.NetFoxConfig) {
	data, _ := json.MarshalIndent(configs, "", "  ")
	os.WriteFile(v.path, data, 0644)
}

func (v *NetFoxVFS) SaveConfig(name string, cfg vfs.NetFoxConfig) {
	configs := v.getConfigs()
	configs[name] = cfg
	v.saveConfigs(configs)
}

func (v *NetFoxVFS) IsAtRoot() bool { return true }
func (v *NetFoxVFS) GetPath() string { return "net://" }
func (v *NetFoxVFS) SetPath(p string) error { return nil }

func (v *NetFoxVFS) ReadDir(ctx context.Context, p string, onChunk func([]vfs.VFSItem)) error {
	configs := v.getConfigs()
	var items []vfs.VFSItem
	for name := range configs {
		items = append(items, vfs.VFSItem{Name: name, IsDir: false, IsExecutable: false})
	}
	if len(items) > 0 { onChunk(items) }
	return nil
}

func (v *NetFoxVFS) Stat(ctx context.Context, p string) (vfs.VFSItem, error) {
	name := v.Base(p)
	configs := v.getConfigs()
	if _, ok := configs[name]; ok { return vfs.VFSItem{Name: name, IsDir: false}, nil }
	return vfs.VFSItem{}, os.ErrNotExist
}

func (v *NetFoxVFS) Join(e ...string) string      { return path.Join(e...) }
func (v *NetFoxVFS) Abs(p string) (string, error) { return p, nil }
func (v *NetFoxVFS) Base(p string) string         { return path.Base(p) }
func (v *NetFoxVFS) Dir(p string) string          { return "net://" }

func (v *NetFoxVFS) MkDir(ctx context.Context, p string) error {
	name := v.Base(p)
	configs := v.getConfigs()
	configs[name] = vfs.NetFoxConfig{Host: name, Port: "22", User: "root"}
	v.saveConfigs(configs)
	return nil
}

func (v *NetFoxVFS) Remove(ctx context.Context, p string) error {
	name := v.Base(p)
	configs := v.getConfigs()
	delete(configs, name)
	v.saveConfigs(configs)
	return nil
}

func (v *NetFoxVFS) Rename(ctx context.Context, old, new string) error {
	oldName := v.Base(old)
	newName := v.Base(new)
	configs := v.getConfigs()
	if cfg, ok := configs[oldName]; ok {
		configs[newName] = cfg
		delete(configs, oldName)
		v.saveConfigs(configs)
	}
	return nil
}

func (v *NetFoxVFS) GetCapabilities() vfs.VFSCapabilities { return vfs.VFSCapabilities{HasRandomAccess: true} }
func (v *NetFoxVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }

type bufferReadAtCloser struct { *bytes.Reader }
func (b *bufferReadAtCloser) Close() error { return nil }
func (b *bufferReadAtCloser) Read(ctx context.Context, p []byte) (int, error) { return b.Reader.Read(p) }
func (b *bufferReadAtCloser) ReadAt(ctx context.Context, p []byte, off int64) (int, error) { return b.Reader.ReadAt(p, off) }
func (b *bufferReadAtCloser) Size() int64 { return int64(b.Reader.Len()) }

func (v *NetFoxVFS) Open(ctx context.Context, p string) (vfs.ReadAtCloser, error) {
	name := v.Base(p)
	configs := v.getConfigs()
	cfg, ok := configs[name]
	if !ok { return nil, os.ErrNotExist }
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return &bufferReadAtCloser{Reader: bytes.NewReader(data)}, nil
}

type netfoxWriter struct {
	v    *NetFoxVFS
	name string
	buf  bytes.Buffer
}
func (w *netfoxWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *netfoxWriter) Close() error {
	var cfg vfs.NetFoxConfig
	json.Unmarshal(w.buf.Bytes(), &cfg)
	configs := w.v.getConfigs()
	configs[w.name] = cfg
	w.v.saveConfigs(configs)
	return nil
}
func (v *NetFoxVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	return &netfoxWriter{v: v, name: v.Base(p)}, nil
}
func (v *NetFoxVFS) ParentVFS() vfs.VFS { return nil }
func (v *NetFoxVFS) Close() error   { return nil }