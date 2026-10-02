// This package provides Go bindings for the Bark Bitcoin protocol.
//
// Bark is an implementation of the Ark protocol, a solution for Bitcoin
// that enables fast, private, and cheap off-chain transactions.
//
// # Installation
//
// Add the dependency to your project:
//
//	go get gitlab.com/ark-bitcoin/bark-ffi-bindings/golang/bark
//
// # Usage Example
//
//	package main
//
//	import (
//		"fmt"
//		"gitlab.com/ark-bitcoin/bark-ffi-bindings/golang/bark"
//	)
//
//	func main() {
//		// Generate a new mnemonic
//		mnemonic, err := bark.GenerateMnemonic()
//		if err != nil {
//			panic(err)
//		}
//		fmt.Println("Mnemonic:", mnemonic)
//
//		// Create a wallet
//		config := bark.Config{
//			ServerAddress:  "https://ark.signet.2nd.dev",
//			EsploraAddress: strPtr("https://esplora.signet.2nd.dev"),
//			Network:        bark.NetworkSignet,
//		}
//
//		wallet, err := bark.WalletCreate(mnemonic, config, "/path/to/data", false)
//		if err != nil {
//			panic(err)
//		}
//		defer wallet.Destroy()
//
//		// Generate an address
//		address, err := wallet.NewAddress()
//		if err != nil {
//			panic(err)
//		}
//		fmt.Println("Address:", address)
//	}
//
// # Platform Support
//
// Pre-built static libraries are included for:
//   - macOS (Intel and Apple Silicon)
//   - Linux (x86-64 and ARM64)
//   - Windows (x86-64)
//   - Android (arm64, arm, x86_64, x86)
//   - iOS (arm64, arm64-sim, x86_64)
//
// No Rust toolchain or environment variables are required.
//
// # Links
//
//   - Documentation: https://second.tech/docs
//   - Repository: https://gitlab.com/ark-bitcoin/bark-ffi-bindings
package bark
