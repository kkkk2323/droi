module github.com/kkkk2323/droi/apps/native

go 1.27.1

require github.com/egoist/mygo v0.2.15

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/frostybee/nuri v1.0.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/yuin/goldmark v1.8.6 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/kkkk2323/droi/packages/droid-sdk-go => ../../packages/droid-sdk-go

tool github.com/egoist/mygo/cmd/mygo
