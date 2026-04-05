package vfs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

type NetFoxConfig struct {
	Type string `json:"Type"` // "sftp" or "ftp"
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

func (v *NetFoxVFS) getConfigs() map[string]NetFoxConfig {
	data, _ := os.ReadFile(v.path)
	var configs map[string]NetFoxConfig
	json.Unmarshal(data, &configs)
	if configs == nil {
		configs = make(map[string]NetFoxConfig)
	}
	return configs
}

func (v *NetFoxVFS) saveConfigs(configs map[string]NetFoxConfig) {
	data, _ := json.MarshalIndent(configs, "", "  ")
	os.WriteFile(v.path, data, 0644)
}

func (v *NetFoxVFS) SaveConfig(name string, cfg NetFoxConfig) {
	configs := v.getConfigs()
	configs[name] = cfg
	v.saveConfigs(configs)
}

func (v *NetFoxVFS) GetPath() string { return "net://" }
func (v *NetFoxVFS) SetPath(p string) error { return nil }

func (v *NetFoxVFS) ReadDir(ctx context.Context, p string, onChunk func([]VFSItem)) error {
	configs := v.getConfigs()
	var items []VFSItem
	for name := range configs {
		items = append(items, VFSItem{
			Name:         name,
			IsDir:        false, // Позволяет по Enter вызвать FindProvider -> NetFoxProvider -> SFTP
			IsExecutable: false,
		})
	}
	if len(items) > 0 {
		onChunk(items)
	}
	return nil
}

func (v *NetFoxVFS) Stat(ctx context.Context, p string) (VFSItem, error) {
	name := v.Base(p)
	configs := v.getConfigs()
	if _, ok := configs[name]; ok {
		return VFSItem{Name: name, IsDir: false}, nil
	}
	return VFSItem{}, os.ErrNotExist
}

func (v *NetFoxVFS) Join(e ...string) string      { return path.Join(e...) }
func (v *NetFoxVFS) Abs(p string) (string, error) { return p, nil }
func (v *NetFoxVFS) Base(p string) string         { return path.Base(p) }
func (v *NetFoxVFS) Dir(p string) string          { return "net://" }

func (v *NetFoxVFS) MkDir(ctx context.Context, p string) error {
	name := v.Base(p)
	configs := v.getConfigs()
	// Используем введенное имя как дефолтный хост, чтобы не ломиться на example.com
	configs[name] = NetFoxConfig{Host: name, Port: "22", User: "root"}
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

func (v *NetFoxVFS) GetCapabilities() VFSCapabilities {
	return VFSCapabilities{HasRandomAccess: true}
}
func (v *NetFoxVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }

// Позволяет редактировать настройки подключения по F4 (открывая JSON как текстовый файл)
type bufferReadAtCloser struct {
	*bytes.Reader
}

func (b *bufferReadAtCloser) Close() error { return nil }
func (b *bufferReadAtCloser) Read(ctx context.Context, p []byte) (int, error) {
	return b.Reader.Read(p)
}
func (b *bufferReadAtCloser) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	return b.Reader.ReadAt(p, off)
}

func (v *NetFoxVFS) Open(ctx context.Context, p string) (ReadAtCloser, error) {
	name := v.Base(p)
	configs := v.getConfigs()
	cfg, ok := configs[name]
	if !ok {
		return nil, os.ErrNotExist
	}
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
	var cfg NetFoxConfig
	if err := json.Unmarshal(w.buf.Bytes(), &cfg); err != nil {
		return fmt.Errorf("Invalid JSON config: %v", err)
	}
	configs := w.v.getConfigs()
	configs[w.name] = cfg
	w.v.saveConfigs(configs)
	return nil
}

func (v *NetFoxVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	return &netfoxWriter{v: v, name: v.Base(p)}, nil
}

func (v *NetFoxVFS) ParentVFS() VFS { return nil }
func (v *NetFoxVFS) Close() error   { return nil }


// NetFoxProvider intercepts Enter on connections and creates SFTP session
type NetFoxProvider struct{}

func (p *NetFoxProvider) Name() string  { return "NetFox" }
func (p *NetFoxProvider) Priority() int { return 100 }

func (p *NetFoxProvider) CanOpen(ctx context.Context, parent VFS, pth string) bool {
	nr, ok := parent.(*NetFoxVFS)
	if !ok { return false }
	// Проверяем, что это не корневой вызов net://
	name := nr.Base(pth)
	if name == "" || name == "." || name == "net://" { return false }

	configs := nr.getConfigs()
	cfg, ok := configs[name]
	// Если тип не указан, считаем sftp для совместимости
	return ok && (cfg.Type == "sftp" || cfg.Type == "")
}

func (p *NetFoxProvider) Open(ctx context.Context, parent VFS, pth string) (VFS, error) {
	nr, _ := parent.(*NetFoxVFS)
	name := nr.Base(pth)
	configs := nr.getConfigs()
	cfg, ok := configs[name]
	if !ok {
		return nil, os.ErrNotExist
	}

	port := cfg.Port
	if port == "" {
		port = "22"
	}

	return NewSFTPVFS(parent, cfg.Host, port, cfg.User, cfg.Pass)
}// FTPProvider handles FTP connections
type FTPProvider struct{}

func (p *FTPProvider) Name() string  { return "FTP" }
func (p *FTPProvider) Priority() int { return 100 }

func (p *FTPProvider) CanOpen(ctx context.Context, parent VFS, pth string) bool {
	nr, ok := parent.(*NetFoxVFS)
	if !ok { return false }
	name := nr.Base(pth)
	if name == "" || name == "." || name == "net://" { return false }

	configs := nr.getConfigs()
	cfg, ok := configs[name]
	return ok && cfg.Type == "ftp"
}

func (p *FTPProvider) Open(ctx context.Context, parent VFS, pth string) (VFS, error) {
	nr, _ := parent.(*NetFoxVFS)
	name := nr.Base(pth)
	configs := nr.getConfigs()
	cfg := configs[name]

	port := cfg.Port
	if port == "" {
		port = "21"
	}

	return NewFTPVFS(parent, cfg.Host, port, cfg.User, cfg.Pass)
}
