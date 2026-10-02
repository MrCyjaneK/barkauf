package bark

/*
#cgo CFLAGS: -I${SRCDIR}

// Desktop platforms
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../lib/darwin_amd64 -lbark_ffi_go
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../lib/darwin_arm64 -lbark_ffi_go
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../lib/linux_amd64 -lbark_ffi_go -lm
#cgo linux,arm64 LDFLAGS: -L${SRCDIR}/../lib/linux_arm64 -lbark_ffi_go -lm
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../lib/windows_amd64 -lbark_ffi_go -lbcrypt -lntdll -lws2_32 -luserenv -ladvapi32

// Android platforms
#cgo android,arm64 LDFLAGS: -L${SRCDIR}/../lib/android_arm64 -lbark_ffi_go
#cgo android,arm LDFLAGS: -L${SRCDIR}/../lib/android_arm -lbark_ffi_go
#cgo android,amd64 LDFLAGS: -L${SRCDIR}/../lib/android_amd64 -lbark_ffi_go
#cgo android,386 LDFLAGS: -L${SRCDIR}/../lib/android_386 -lbark_ffi_go

// iOS platforms
#cgo ios,arm64 LDFLAGS: -L${SRCDIR}/../lib/ios_arm64 -lbark_ffi_go
#cgo ios,amd64 LDFLAGS: -L${SRCDIR}/../lib/ios_amd64 -lbark_ffi_go
*/
import "C"

// This file contains CGo directives for linking the Bark FFI native library.
//
// The directives are platform-specific and automatically select the correct
// pre-built static library based on GOOS and GOARCH.
//
// # Supported Platforms (Pre-built)
//
// Desktop:
//   - darwin/amd64  (macOS Intel)
//   - darwin/arm64  (macOS Apple Silicon)
//   - linux/amd64   (Linux x86-64)
//   - linux/arm64   (Linux ARM64)
//   - windows/amd64 (Windows x86-64)
//
// Mobile:
//   - android/arm64  (Android arm64-v8a)
//   - android/arm    (Android armeabi-v7a)
//   - android/amd64  (Android x86_64)
//   - android/386    (Android x86)
//   - ios/arm64      (iOS device + simulator M1)
//   - ios/amd64      (iOS simulator Intel)
//
// # Default Behavior
//
// For supported platforms, everything works automatically with `go get`.
// No environment variables or manual configuration needed.
//
// # Custom Builds
//
// To override the default library paths (e.g., for custom builds or
// unsupported platforms), you can:
//
// 1. Set environment variables (they take precedence):
//
//	export CGO_LDFLAGS="-L/custom/path -lbark_ffi_go"
//	export CGO_CFLAGS="-I/custom/include"
//	go build
//
// 2. Build locally and use go.mod replace:
//
//	cd bark-ffi-bindings/golang && ./generate-bindings.sh
//	cd your-project
//	go mod edit -replace gitlab.com/ark-bitcoin/bark-ffi-bindings/golang/bark=/path/to/bark-ffi-bindings/golang/bark
//	go build
//
// 3. For unsupported platforms, add a directive to this file:
//
//	#cgo freebsd,amd64 LDFLAGS: -L${SRCDIR}/../lib/freebsd_amd64 -lbark_ffi_go
//
// # Mobile Usage
//
// For mobile apps (Android/iOS) that use Go backends compiled as C libraries:
//
// 1. Compile your Go code with Bark as a C shared library:
//
//	// Android arm64
//	CGO_ENABLED=1 GOOS=android GOARCH=arm64 go build -buildmode=c-shared -o libmyapp.so
//
//	// iOS arm64
//	CGO_ENABLED=1 GOOS=ios GOARCH=arm64 go build -buildmode=c-archive -o libmyapp.a
//
// 2. Link the resulting library in your Android/iOS native app
//
// 3. The Bark static library will be automatically linked via CGo directives
