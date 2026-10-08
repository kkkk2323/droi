module github.com/kkkk2323/droi/apps/native

go 1.27.1

require (
	github.com/coder/websocket v1.8.15
	github.com/ebitengine/purego v0.11.1
	github.com/egoist/mygo v0.3.3
	github.com/frostybee/nuri v1.0.1
	github.com/google/uuid v1.6.0
	github.com/kkkk2323/droi/packages/droid-sdk-go v0.0.0-00010101000000-000000000000
	github.com/yuin/goldmark v1.8.6
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
	modernc.org/sqlite v1.49.1
	rsc.io/qr v0.2.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	modernc.org/libc v1.72.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace github.com/kkkk2323/droi/packages/droid-sdk-go => ../../packages/droid-sdk-go

tool github.com/egoist/mygo/cmd/mygo
