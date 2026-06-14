module github.com/unxed/f4

go 1.25.5

require (
	github.com/alecthomas/chroma/v2 v2.15.0
	github.com/jlaffaye/ftp v0.2.0
	github.com/mattn/go-runewidth v0.0.15
	github.com/mholt/archives v0.1.5
	github.com/pkg/sftp v1.13.6
	github.com/unxed/tar v0.1.51
	github.com/unxed/vtinput v0.0.0
	github.com/unxed/vtui v0.0.0
	github.com/unxed/zip v0.1.50
	github.com/unxed/zipper v0.1.23
	github.com/vmihailenco/msgpack/v5 v5.4.1
	golang.org/x/crypto v0.31.0
	golang.org/x/sys v0.44.0
	golang.org/x/term v0.40.0
	golang.org/x/text v0.37.0
)

require (
	github.com/STARRY-S/zip v0.2.3 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/sevenzip v1.6.1 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/dovydenkovas/ppmd v0.1.1 // indirect
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/emmansun/base64 v0.9.0 // indirect
	github.com/fogleman/gg v1.3.0 // indirect
	github.com/go-text/typesetting v0.3.4 // indirect
	github.com/go-webgpu/goffi v0.5.3 // indirect
	github.com/go-webgpu/webgpu v0.4.3 // indirect
	github.com/gogpu/gg v0.47.3 // indirect
	github.com/gogpu/gogpu v0.39.1 // indirect
	github.com/gogpu/gpucontext v0.19.0 // indirect
	github.com/gogpu/gputypes v0.5.0 // indirect
	github.com/gogpu/naga v0.17.13 // indirect
	github.com/gogpu/wgpu v0.28.7 // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jezek/xgb v1.3.1 // indirect
	github.com/klauspost/compress v1.18.7-0.20260521203646-ecdb779d8745 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	github.com/mikelolasagasti/xz v1.0.1 // indirect
	github.com/minio/minlz v1.0.1 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/neurlang/wayland v0.4.2 // indirect
	github.com/neurlang/winc v0.1.2 // indirect
	github.com/nwaples/rardecode/v2 v2.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/sorairolake/lzip-go v0.3.8 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/unxed/keytrans v0.1.27 // indirect
	github.com/unxed/localecp v0.1.4 // indirect
	github.com/unxed/par2 v0.1.2 // indirect
	github.com/unxed/winkeys v0.1.0 // indirect
	github.com/unxed/xkb-go v0.1.8 // indirect
	github.com/unxed/zipcharset v0.1.4 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/yalue/native_endian v1.0.2 // indirect
	github.com/zzl/go-win32api/v2 v2.1.0 // indirect
	go4.org v0.0.0-20230225012048-214862532bf5 // indirect
	golang.design/x/clipboard v0.7.0 // indirect
	golang.org/x/exp/shiny v0.0.0-20260508232706-74f9aab9d74a // indirect
	golang.org/x/image v0.40.0 // indirect
	golang.org/x/mobile v0.0.0-20260410095206-2cfb76559b7b // indirect
	golang.org/x/sync v0.20.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.41.0 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.7.2 // indirect
	modernc.org/sqlite v1.29.5 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)

replace github.com/unxed/vtinput => ./libs/vtinput

replace github.com/unxed/vtui => ./libs/vtui

replace github.com/ebitengine/purego => github.com/unxed/pureffi v0.1.8
