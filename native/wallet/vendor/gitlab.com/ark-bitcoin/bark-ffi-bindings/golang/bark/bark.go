package bark

// #include <bark.h>
import "C"

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"runtime"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"unsafe"
)

// This is needed, because as of go 1.24
// type RustBuffer C.RustBuffer cannot have methods,
// RustBuffer is treated as non-local type
type GoRustBuffer struct {
	inner C.RustBuffer
}

type RustBufferI interface {
	AsReader() *bytes.Reader
	Free()
	ToGoBytes() []byte
	Data() unsafe.Pointer
	Len() uint64
	Capacity() uint64
}

// C.RustBuffer fields exposed as an interface so they can be accessed in different Go packages.
// See https://github.com/golang/go/issues/13467
type ExternalCRustBuffer interface {
	Data() unsafe.Pointer
	Len() uint64
	Capacity() uint64
}

func RustBufferFromC(b C.RustBuffer) ExternalCRustBuffer {
	return GoRustBuffer{
		inner: b,
	}
}

func CFromRustBuffer(b ExternalCRustBuffer) C.RustBuffer {
	return C.RustBuffer{
		capacity: C.uint64_t(b.Capacity()),
		len:      C.uint64_t(b.Len()),
		data:     (*C.uchar)(b.Data()),
	}
}

func RustBufferFromExternal(b ExternalCRustBuffer) GoRustBuffer {
	return GoRustBuffer{
		inner: C.RustBuffer{
			capacity: C.uint64_t(b.Capacity()),
			len:      C.uint64_t(b.Len()),
			data:     (*C.uchar)(b.Data()),
		},
	}
}

func (cb GoRustBuffer) Capacity() uint64 {
	return uint64(cb.inner.capacity)
}

func (cb GoRustBuffer) Len() uint64 {
	return uint64(cb.inner.len)
}

func (cb GoRustBuffer) Data() unsafe.Pointer {
	return unsafe.Pointer(cb.inner.data)
}

func (cb GoRustBuffer) AsReader() *bytes.Reader {
	b := unsafe.Slice((*byte)(cb.inner.data), C.uint64_t(cb.inner.len))
	return bytes.NewReader(b)
}

func (cb GoRustBuffer) Free() {
	rustCall(func(status *C.RustCallStatus) bool {
		C.ffi_bark_ffi_rustbuffer_free(cb.inner, status)
		return false
	})
}

func (cb GoRustBuffer) ToGoBytes() []byte {
	return C.GoBytes(unsafe.Pointer(cb.inner.data), C.int(cb.inner.len))
}

func stringToRustBuffer(str string) C.RustBuffer {
	return bytesToRustBuffer([]byte(str))
}

func bytesToRustBuffer(b []byte) C.RustBuffer {
	if len(b) == 0 {
		return C.RustBuffer{}
	}
	// We can pass the pointer along here, as it is pinned
	// for the duration of this call
	foreign := C.ForeignBytes{
		len:  C.int(len(b)),
		data: (*C.uchar)(unsafe.Pointer(&b[0])),
	}

	return rustCall(func(status *C.RustCallStatus) C.RustBuffer {
		return C.ffi_bark_ffi_rustbuffer_from_bytes(foreign, status)
	})
}

type BufLifter[GoType any] interface {
	Lift(value RustBufferI) GoType
}

type BufLowerer[GoType any] interface {
	Lower(value GoType) C.RustBuffer
}

type BufReader[GoType any] interface {
	Read(reader io.Reader) GoType
}

type BufWriter[GoType any] interface {
	Write(writer io.Writer, value GoType)
}

func LowerIntoRustBuffer[GoType any](bufWriter BufWriter[GoType], value GoType) C.RustBuffer {
	// This might be not the most efficient way but it does not require knowing allocation size
	// beforehand
	var buffer bytes.Buffer
	bufWriter.Write(&buffer, value)

	bytes, err := io.ReadAll(&buffer)
	if err != nil {
		panic(fmt.Errorf("reading written data: %w", err))
	}
	return bytesToRustBuffer(bytes)
}

func LiftFromRustBuffer[GoType any](bufReader BufReader[GoType], rbuf RustBufferI) GoType {
	defer rbuf.Free()
	reader := rbuf.AsReader()
	item := bufReader.Read(reader)
	if reader.Len() > 0 {
		// TODO: Remove this
		leftover, _ := io.ReadAll(reader)
		panic(fmt.Errorf("Junk remaining in buffer after lifting: %s", string(leftover)))
	}
	return item
}

func rustCallWithError[E any, U any](converter BufReader[E], callback func(*C.RustCallStatus) U) (U, E) {
	var status C.RustCallStatus
	returnValue := callback(&status)
	err := checkCallStatus(converter, status)
	return returnValue, err
}

func checkCallStatus[E any](converter BufReader[E], status C.RustCallStatus) E {
	switch status.code {
	case 0:
		var zero E
		return zero
	case 1:
		return LiftFromRustBuffer(converter, GoRustBuffer{inner: status.errorBuf})
	case 2:
		// when the rust code sees a panic, it tries to construct a rustBuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(GoRustBuffer{inner: status.errorBuf})))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		panic(fmt.Errorf("unknown status code: %d", status.code))
	}
}

func checkCallStatusUnknown(status C.RustCallStatus) error {
	switch status.code {
	case 0:
		return nil
	case 1:
		panic(fmt.Errorf("function not returning an error returned an error"))
	case 2:
		// when the rust code sees a panic, it tries to construct a C.RustBuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: status.errorBuf,
			})))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		return fmt.Errorf("unknown status code: %d", status.code)
	}
}

func rustCall[U any](callback func(*C.RustCallStatus) U) U {
	returnValue, err := rustCallWithError[error](nil, callback)
	if err != nil {
		panic(err)
	}
	return returnValue
}

type NativeError interface {
	AsError() error
}

func writeInt8(writer io.Writer, value int8) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint8(writer io.Writer, value uint8) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt16(writer io.Writer, value int16) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint16(writer io.Writer, value uint16) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt32(writer io.Writer, value int32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint32(writer io.Writer, value uint32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt64(writer io.Writer, value int64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint64(writer io.Writer, value uint64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeFloat32(writer io.Writer, value float32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeFloat64(writer io.Writer, value float64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func readInt8(reader io.Reader) int8 {
	var result int8
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint8(reader io.Reader) uint8 {
	var result uint8
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt16(reader io.Reader) int16 {
	var result int16
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint16(reader io.Reader) uint16 {
	var result uint16
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt32(reader io.Reader) int32 {
	var result int32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint32(reader io.Reader) uint32 {
	var result uint32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt64(reader io.Reader) int64 {
	var result int64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint64(reader io.Reader) uint64 {
	var result uint64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readFloat32(reader io.Reader) float32 {
	var result float32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readFloat64(reader io.Reader) float64 {
	var result float64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func init() {

	FfiConverterBarkLoggerINSTANCE.register()
	FfiConverterCustomOnchainWalletCallbacksINSTANCE.register()
	uniffiCheckChecksums()
}

func uniffiCheckChecksums() {
	// Get the bindings contract version from our ComponentInterface
	bindingsContractVersion := 30
	// Get the scaffolding contract version by calling the into the dylib
	scaffoldingContractVersion := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.ffi_bark_ffi_uniffi_contract_version()
	})
	if bindingsContractVersion != int(scaffoldingContractVersion) {
		// If this happens try cleaning and rebuilding your project
		panic("bark: UniFFI contract version mismatch")
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_default_vtxo_key_gap_limit()
		})
		if checksum != 3344 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_default_vtxo_key_gap_limit: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_extract_tx_from_psbt()
		})
		if checksum != 8893 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_extract_tx_from_psbt: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_generate_mnemonic()
		})
		if checksum != 65033 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_generate_mnemonic: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_max_vtxo_key_gap_limit()
		})
		if checksum != 15808 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_max_vtxo_key_gap_limit: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_validate_ark_address()
		})
		if checksum != 43084 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_validate_ark_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_validate_mnemonic()
		})
		if checksum != 33507 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_validate_mnemonic: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_set_logger()
		})
		if checksum != 16477 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_set_logger: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_func_init_wallet()
		})
		if checksum != 41495 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_func_init_wallet: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_notificationholder_cancel_next_notification_wait()
		})
		if checksum != 45269 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_notificationholder_cancel_next_notification_wait: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_notificationholder_next_notification()
		})
		if checksum != 25507 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_notificationholder_next_notification: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_get_balance()
		})
		if checksum != 22074 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_get_balance: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_prepare_tx()
		})
		if checksum != 41818 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_prepare_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_prepare_drain_tx()
		})
		if checksum != 43328 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_prepare_drain_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_finish_psbt()
		})
		if checksum != 24435 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_finish_psbt: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_is_mine()
		})
		if checksum != 18867 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_is_mine: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_register_tx()
		})
		if checksum != 17970 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_register_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_evict_tx()
		})
		if checksum != 36298 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_evict_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_make_signed_p2a_cpfp()
		})
		if checksum != 46075 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_make_signed_p2a_cpfp: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_store_signed_p2a_cpfp()
		})
		if checksum != 64815 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_store_signed_p2a_cpfp: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_sync()
		})
		if checksum != 29018 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_customonchainwalletcallbacks_sync: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_barklogger_log()
		})
		if checksum != 30279 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_barklogger_log: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_balance()
		})
		if checksum != 39213 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_balance: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_evict_tx()
		})
		if checksum != 26786 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_evict_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_fee_rates()
		})
		if checksum != 30741 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_fee_rates: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_initial_scan()
		})
		if checksum != 19010 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_initial_scan: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_new_address()
		})
		if checksum != 7405 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_new_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_send()
		})
		if checksum != 30495 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_send: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_sync()
		})
		if checksum != 4373 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_sync: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_tip_height()
		})
		if checksum != 12254 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_tip_height: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_transactions()
		})
		if checksum != 566 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_transactions: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_onchainwallet_utxos()
		})
		if checksum != 44926 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_onchainwallet_utxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_all_exits_claimable_at_height()
		})
		if checksum != 59034 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_all_exits_claimable_at_height: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_all_vtxos()
		})
		if checksum != 62209 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_all_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_allow_lightning_send_to_exit()
		})
		if checksum != 56991 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_allow_lightning_send_to_exit: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_ark_info()
		})
		if checksum != 54388 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_ark_info: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_attempt_lightning_receive_exit()
		})
		if checksum != 6368 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_attempt_lightning_receive_exit: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_balance()
		})
		if checksum != 37345 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_balance: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_board_all()
		})
		if checksum != 5667 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_board_all: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_board_amount()
		})
		if checksum != 54692 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_board_amount: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_board_funding_address()
		})
		if checksum != 8961 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_board_funding_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_board_psbt()
		})
		if checksum != 28300 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_board_psbt: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_bolt11_invoice()
		})
		if checksum != 54959 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_bolt11_invoice: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_bolt11_invoice_for_address()
		})
		if checksum != 56368 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_bolt11_invoice_for_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_broadcast_tx()
		})
		if checksum != 46367 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_broadcast_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_cancel_all_pending_rounds()
		})
		if checksum != 53455 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_cancel_all_pending_rounds: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_cancel_exit()
		})
		if checksum != 63172 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_cancel_exit: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_cancel_lightning_receive()
		})
		if checksum != 49444 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_cancel_lightning_receive: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_cancel_pending_round()
		})
		if checksum != 13433 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_cancel_pending_round: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_check_lightning_payment()
		})
		if checksum != 50033 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_check_lightning_payment: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_claimable_lightning_receive_balance_sats()
		})
		if checksum != 7743 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_claimable_lightning_receive_balance_sats: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_config()
		})
		if checksum != 23594 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_config: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_drain_exits()
		})
		if checksum != 45364 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_drain_exits: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_arkoor_payment_fee()
		})
		if checksum != 35980 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_arkoor_payment_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_board_fee()
		})
		if checksum != 55967 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_board_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_emergency_exit_fee()
		})
		if checksum != 44713 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_emergency_exit_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_lightning_receive_fee()
		})
		if checksum != 33637 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_lightning_receive_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_lightning_send_fee()
		})
		if checksum != 11290 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_lightning_send_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_offboard_all_fee()
		})
		if checksum != 24629 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_offboard_all_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_offboard_fee()
		})
		if checksum != 37073 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_offboard_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_refresh_fee()
		})
		if checksum != 18182 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_refresh_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_estimate_send_onchain_fee()
		})
		if checksum != 1027 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_estimate_send_onchain_fee: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_fingerprint()
		})
		if checksum != 51021 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_fingerprint: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_get_exit_status()
		})
		if checksum != 33054 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_get_exit_status: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_get_exit_vtxos()
		})
		if checksum != 20147 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_get_exit_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_get_expiring_vtxos()
		})
		if checksum != 39689 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_get_expiring_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_get_first_expiring_vtxo_blockheight()
		})
		if checksum != 33536 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_get_first_expiring_vtxo_blockheight: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_get_next_required_refresh_blockheight()
		})
		if checksum != 58530 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_get_next_required_refresh_blockheight: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_get_vtxo_by_id()
		})
		if checksum != 34050 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_get_vtxo_by_id: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_get_vtxos_to_refresh()
		})
		if checksum != 52553 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_get_vtxos_to_refresh: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_has_pending_exits()
		})
		if checksum != 19790 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_has_pending_exits: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_history()
		})
		if checksum != 10552 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_history: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_history_by_payment_method()
		})
		if checksum != 11845 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_history_by_payment_method: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_import_vtxo()
		})
		if checksum != 26027 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_import_vtxo: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_import_vtxos()
		})
		if checksum != 13727 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_import_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_is_invoice_paid()
		})
		if checksum != 5699 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_is_invoice_paid: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_lightning_receive_state()
		})
		if checksum != 27834 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_lightning_receive_state: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_lightning_send_state()
		})
		if checksum != 53422 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_lightning_send_state: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_list_claimable_exits()
		})
		if checksum != 58660 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_list_claimable_exits: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_lock_vtxos()
		})
		if checksum != 13415 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_lock_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_mailbox_authorization()
		})
		if checksum != 25098 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_mailbox_authorization: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_mailbox_identifier()
		})
		if checksum != 15231 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_mailbox_identifier: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_maintenance()
		})
		if checksum != 33161 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_maintenance: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_maintenance_delegated()
		})
		if checksum != 31668 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_maintenance_delegated: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_maintenance_refresh()
		})
		if checksum != 34548 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_maintenance_refresh: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_network()
		})
		if checksum != 2007 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_network: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_new_address()
		})
		if checksum != 65478 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_new_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_new_address_with_index()
		})
		if checksum != 37344 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_new_address_with_index: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_next_round_start_time()
		})
		if checksum != 20748 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_next_round_start_time: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_notifications()
		})
		if checksum != 8597 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_notifications: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_offboard_all()
		})
		if checksum != 7986 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_offboard_all: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_offboard_vtxos()
		})
		if checksum != 64605 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_offboard_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pay_lightning_address()
		})
		if checksum != 43061 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pay_lightning_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pay_lightning_invoice()
		})
		if checksum != 26921 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pay_lightning_invoice: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pay_lightning_offer()
		})
		if checksum != 25462 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pay_lightning_offer: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pay_lnurl()
		})
		if checksum != 1459 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pay_lnurl: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_peek_address()
		})
		if checksum != 31847 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_peek_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_board_vtxos()
		})
		if checksum != 53566 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_board_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_boards()
		})
		if checksum != 61501 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_boards: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_exits_total_sats()
		})
		if checksum != 38418 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_exits_total_sats: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_lightning_receives()
		})
		if checksum != 26676 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_lightning_receives: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_lightning_send_vtxos()
		})
		if checksum != 62766 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_lightning_send_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_lightning_sends()
		})
		if checksum != 8847 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_lightning_sends: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_round_input_vtxos()
		})
		if checksum != 54307 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_round_input_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_pending_round_states()
		})
		if checksum != 11480 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_pending_round_states: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_progress_exits()
		})
		if checksum != 6255 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_progress_exits: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_progress_pending_rounds()
		})
		if checksum != 43537 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_progress_pending_rounds: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_properties()
		})
		if checksum != 9773 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_properties: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_recover_vtxos()
		})
		if checksum != 56476 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_recover_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_recovery_report()
		})
		if checksum != 10331 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_recovery_report: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_recovery_status()
		})
		if checksum != 14132 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_recovery_status: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_refresh_server()
		})
		if checksum != 41162 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_refresh_server: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_refresh_vtxos()
		})
		if checksum != 52101 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_refresh_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_refresh_vtxos_delegated()
		})
		if checksum != 61476 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_refresh_vtxos_delegated: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_refresh_vtxos_scheduled()
		})
		if checksum != 50488 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_refresh_vtxos_scheduled: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_run_daemon()
		})
		if checksum != 9520 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_run_daemon: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_send_arkoor_payment()
		})
		if checksum != 32252 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_send_arkoor_payment: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_send_onchain()
		})
		if checksum != 42570 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_send_onchain: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_sign_exit_claim_inputs()
		})
		if checksum != 12930 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_sign_exit_claim_inputs: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_spendable_vtxos()
		})
		if checksum != 22755 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_spendable_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_start_exit_for_entire_wallet()
		})
		if checksum != 6525 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_start_exit_for_entire_wallet: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_start_exit_for_vtxos()
		})
		if checksum != 56154 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_start_exit_for_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_start_exit_for_vtxos_including_non_standard()
		})
		if checksum != 27907 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_start_exit_for_vtxos_including_non_standard: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_stop_daemon()
		})
		if checksum != 7109 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_stop_daemon: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_stop_daemon_wait()
		})
		if checksum != 1734 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_stop_daemon_wait: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_stuck_failed_lightning_sends()
		})
		if checksum != 31613 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_stuck_failed_lightning_sends: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_sync()
		})
		if checksum != 2151 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_sync: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_sync_exits()
		})
		if checksum != 12141 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_sync_exits: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_sync_force_exited_vtxos()
		})
		if checksum != 25363 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_sync_force_exited_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_sync_pending_boards()
		})
		if checksum != 63453 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_sync_pending_boards: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_try_claim_all_lightning_receives()
		})
		if checksum != 14560 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_try_claim_all_lightning_receives: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_try_claim_lightning_receive()
		})
		if checksum != 43209 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_try_claim_lightning_receive: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_unlock_vtxos()
		})
		if checksum != 11806 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_unlock_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_validate_arkoor_address()
		})
		if checksum != 30498 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_validate_arkoor_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_vtxo_encoded()
		})
		if checksum != 25637 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_vtxo_encoded: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_method_wallet_vtxos()
		})
		if checksum != 33207 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_method_wallet_vtxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_constructor_onchainwallet_custom()
		})
		if checksum != 33645 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_constructor_onchainwallet_custom: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_constructor_onchainwallet_default()
		})
		if checksum != 61714 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_constructor_onchainwallet_default: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bark_ffi_checksum_constructor_wallet_open()
		})
		if checksum != 45531 {
			// If this happens try cleaning and rebuilding your project
			panic("bark: uniffi_bark_ffi_checksum_constructor_wallet_open: UniFFI API checksum mismatch")
		}
	}
}

type FfiConverterUint8 struct{}

var FfiConverterUint8INSTANCE = FfiConverterUint8{}

func (FfiConverterUint8) Lower(value uint8) C.uint8_t {
	return C.uint8_t(value)
}

func (FfiConverterUint8) Write(writer io.Writer, value uint8) {
	writeUint8(writer, value)
}

func (FfiConverterUint8) Lift(value C.uint8_t) uint8 {
	return uint8(value)
}

func (FfiConverterUint8) Read(reader io.Reader) uint8 {
	return readUint8(reader)
}

type FfiDestroyerUint8 struct{}

func (FfiDestroyerUint8) Destroy(_ uint8) {}

type FfiConverterUint16 struct{}

var FfiConverterUint16INSTANCE = FfiConverterUint16{}

func (FfiConverterUint16) Lower(value uint16) C.uint16_t {
	return C.uint16_t(value)
}

func (FfiConverterUint16) Write(writer io.Writer, value uint16) {
	writeUint16(writer, value)
}

func (FfiConverterUint16) Lift(value C.uint16_t) uint16 {
	return uint16(value)
}

func (FfiConverterUint16) Read(reader io.Reader) uint16 {
	return readUint16(reader)
}

type FfiDestroyerUint16 struct{}

func (FfiDestroyerUint16) Destroy(_ uint16) {}

type FfiConverterUint32 struct{}

var FfiConverterUint32INSTANCE = FfiConverterUint32{}

func (FfiConverterUint32) Lower(value uint32) C.uint32_t {
	return C.uint32_t(value)
}

func (FfiConverterUint32) Write(writer io.Writer, value uint32) {
	writeUint32(writer, value)
}

func (FfiConverterUint32) Lift(value C.uint32_t) uint32 {
	return uint32(value)
}

func (FfiConverterUint32) Read(reader io.Reader) uint32 {
	return readUint32(reader)
}

type FfiDestroyerUint32 struct{}

func (FfiDestroyerUint32) Destroy(_ uint32) {}

type FfiConverterUint64 struct{}

var FfiConverterUint64INSTANCE = FfiConverterUint64{}

func (FfiConverterUint64) Lower(value uint64) C.uint64_t {
	return C.uint64_t(value)
}

func (FfiConverterUint64) Write(writer io.Writer, value uint64) {
	writeUint64(writer, value)
}

func (FfiConverterUint64) Lift(value C.uint64_t) uint64 {
	return uint64(value)
}

func (FfiConverterUint64) Read(reader io.Reader) uint64 {
	return readUint64(reader)
}

type FfiDestroyerUint64 struct{}

func (FfiDestroyerUint64) Destroy(_ uint64) {}

type FfiConverterInt64 struct{}

var FfiConverterInt64INSTANCE = FfiConverterInt64{}

func (FfiConverterInt64) Lower(value int64) C.int64_t {
	return C.int64_t(value)
}

func (FfiConverterInt64) Write(writer io.Writer, value int64) {
	writeInt64(writer, value)
}

func (FfiConverterInt64) Lift(value C.int64_t) int64 {
	return int64(value)
}

func (FfiConverterInt64) Read(reader io.Reader) int64 {
	return readInt64(reader)
}

type FfiDestroyerInt64 struct{}

func (FfiDestroyerInt64) Destroy(_ int64) {}

type FfiConverterBool struct{}

var FfiConverterBoolINSTANCE = FfiConverterBool{}

func (FfiConverterBool) Lower(value bool) C.int8_t {
	if value {
		return C.int8_t(1)
	}
	return C.int8_t(0)
}

func (FfiConverterBool) Write(writer io.Writer, value bool) {
	if value {
		writeInt8(writer, 1)
	} else {
		writeInt8(writer, 0)
	}
}

func (FfiConverterBool) Lift(value C.int8_t) bool {
	return value != 0
}

func (FfiConverterBool) Read(reader io.Reader) bool {
	return readInt8(reader) != 0
}

type FfiDestroyerBool struct{}

func (FfiDestroyerBool) Destroy(_ bool) {}

type FfiConverterString struct{}

var FfiConverterStringINSTANCE = FfiConverterString{}

func (FfiConverterString) Lift(rb RustBufferI) string {
	defer rb.Free()
	reader := rb.AsReader()
	b, err := io.ReadAll(reader)
	if err != nil {
		panic(fmt.Errorf("reading reader: %w", err))
	}
	return string(b)
}

func (FfiConverterString) Read(reader io.Reader) string {
	length := readInt32(reader)
	buffer := make([]byte, length)
	read_length, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		panic(err)
	}
	if read_length != int(length) {
		panic(fmt.Errorf("bad read length when reading string, expected %d, read %d", length, read_length))
	}
	return string(buffer)
}

func (FfiConverterString) Lower(value string) C.RustBuffer {
	return stringToRustBuffer(value)
}

func (c FfiConverterString) LowerExternal(value string) ExternalCRustBuffer {
	return RustBufferFromC(stringToRustBuffer(value))
}

func (FfiConverterString) Write(writer io.Writer, value string) {
	if len(value) > math.MaxInt32 {
		panic("String is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	write_length, err := io.WriteString(writer, value)
	if err != nil {
		panic(err)
	}
	if write_length != len(value) {
		panic(fmt.Errorf("bad write length when writing string, expected %d, written %d", len(value), write_length))
	}
}

type FfiDestroyerString struct{}

func (FfiDestroyerString) Destroy(_ string) {}

// Below is an implementation of synchronization requirements outlined in the link.
// https://github.com/mozilla/uniffi-rs/blob/0dc031132d9493ca812c3af6e7dd60ad2ea95bf0/uniffi_bindgen/src/bindings/kotlin/templates/ObjectRuntime.kt#L31

type FfiObject struct {
	handle        C.uint64_t
	callCounter   atomic.Int64
	cloneFunction func(C.uint64_t, *C.RustCallStatus) C.uint64_t
	freeFunction  func(C.uint64_t, *C.RustCallStatus)
	destroyed     atomic.Bool
}

func newFfiObject(
	handle C.uint64_t,
	cloneFunction func(C.uint64_t, *C.RustCallStatus) C.uint64_t,
	freeFunction func(C.uint64_t, *C.RustCallStatus),
) FfiObject {
	return FfiObject{
		handle:        handle,
		cloneFunction: cloneFunction,
		freeFunction:  freeFunction,
	}
}

func (ffiObject *FfiObject) incrementPointer(debugName string) C.uint64_t {
	for {
		counter := ffiObject.callCounter.Load()
		if counter <= -1 {
			panic(fmt.Errorf("%v object has already been destroyed", debugName))
		}
		if counter == math.MaxInt64 {
			panic(fmt.Errorf("%v object call counter would overflow", debugName))
		}
		if ffiObject.callCounter.CompareAndSwap(counter, counter+1) {
			break
		}
	}

	return rustCall(func(status *C.RustCallStatus) C.uint64_t {
		return ffiObject.cloneFunction(ffiObject.handle, status)
	})
}

func (ffiObject *FfiObject) decrementPointer() {
	if ffiObject.callCounter.Add(-1) == -1 {
		ffiObject.freeRustArcPtr()
	}
}

func (ffiObject *FfiObject) destroy() {
	if ffiObject.destroyed.CompareAndSwap(false, true) {
		if ffiObject.callCounter.Add(-1) == -1 {
			ffiObject.freeRustArcPtr()
		}
	}
}

func (ffiObject *FfiObject) freeRustArcPtr() {
	if ffiObject.handle == 0 {
		return
	}
	rustCall(func(status *C.RustCallStatus) int32 {
		ffiObject.freeFunction(ffiObject.handle, status)
		return 0
	})
}

// Foreign-implemented sink for log records.
//
// IMPORTANT: implementations must not call back into bark APIs that
// themselves emit log records, or the foreign runtime may stack-overflow
// or deadlock.
type BarkLogger interface {
	Log(level LogLevel, target string, message string)
}

// Foreign-implemented sink for log records.
//
// IMPORTANT: implementations must not call back into bark APIs that
// themselves emit log records, or the foreign runtime may stack-overflow
// or deadlock.
type BarkLoggerImpl struct {
	ffiObject FfiObject
}

func (_self *BarkLoggerImpl) Log(level LogLevel, target string, message string) {
	_pointer := _self.ffiObject.incrementPointer("BarkLogger")
	defer _self.ffiObject.decrementPointer()
	rustCall(func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bark_ffi_fn_method_barklogger_log(
			_pointer, FfiConverterLogLevelINSTANCE.Lower(level), FfiConverterStringINSTANCE.Lower(target), FfiConverterStringINSTANCE.Lower(message), _uniffiStatus)
		return false
	})
}
func (object *BarkLoggerImpl) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterBarkLogger struct {
	handleMap *concurrentHandleMap[BarkLogger]
}

var FfiConverterBarkLoggerINSTANCE = FfiConverterBarkLogger{
	handleMap: newConcurrentHandleMap[BarkLogger](),
}

func (c FfiConverterBarkLogger) Lift(handle C.uint64_t) BarkLogger {
	if uint64(handle)&1 == 0 {
		// Rust-generated handle (even), construct a new object wrapping the handle
		result := &BarkLoggerImpl{
			newFfiObject(
				handle,
				func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
					return C.uniffi_bark_ffi_fn_clone_barklogger(handle, status)
				},
				func(handle C.uint64_t, status *C.RustCallStatus) {
					C.uniffi_bark_ffi_fn_free_barklogger(handle, status)
				},
			),
		}
		runtime.SetFinalizer(result, (*BarkLoggerImpl).Destroy)
		return result
	} else {
		// Go-generated handle (odd), retrieve from the handle map
		val, ok := c.handleMap.tryGet(uint64(handle))
		if !ok {
			panic(fmt.Errorf("no callback in handle map: %d", handle))
		}
		c.handleMap.remove(uint64(handle))
		return val
	}
}

func (c FfiConverterBarkLogger) Read(reader io.Reader) BarkLogger {
	return c.Lift(C.uint64_t(readUint64(reader)))
}

func (c FfiConverterBarkLogger) Lower(value BarkLogger) C.uint64_t {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the handle will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked handle.
	if val, ok := value.(*BarkLoggerImpl); ok {
		// Rust-backed object, clone the handle
		handle := val.ffiObject.incrementPointer("BarkLogger")
		defer val.ffiObject.decrementPointer()
		return handle
	} else {
		// Go-backed object, insert into handle map
		return C.uint64_t(c.handleMap.insert(value))
	}
}

func (c FfiConverterBarkLogger) Write(writer io.Writer, value BarkLogger) {
	writeUint64(writer, uint64(c.Lower(value)))
}

func LiftFromExternalBarkLogger(handle uint64) BarkLogger {
	return FfiConverterBarkLoggerINSTANCE.Lift(C.uint64_t(handle))
}

func LowerToExternalBarkLogger(value BarkLogger) uint64 {
	return uint64(FfiConverterBarkLoggerINSTANCE.Lower(value))
}

type FfiDestroyerBarkLogger struct{}

func (_ FfiDestroyerBarkLogger) Destroy(value BarkLogger) {
	if val, ok := value.(*BarkLoggerImpl); ok {
		val.Destroy()
	}
}

type uniffiCallbackResult C.int8_t

const (
	uniffiIdxCallbackFree               uniffiCallbackResult = 0
	uniffiCallbackResultSuccess         uniffiCallbackResult = 0
	uniffiCallbackResultError           uniffiCallbackResult = 1
	uniffiCallbackUnexpectedResultError uniffiCallbackResult = 2
	uniffiCallbackCancelled             uniffiCallbackResult = 3
)

type concurrentHandleMap[T any] struct {
	handles       map[uint64]T
	currentHandle uint64
	lock          sync.RWMutex
}

func newConcurrentHandleMap[T any]() *concurrentHandleMap[T] {
	return &concurrentHandleMap[T]{
		handles:       map[uint64]T{},
		currentHandle: 1,
	}
}

func (cm *concurrentHandleMap[T]) insert(obj T) uint64 {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	handle := cm.currentHandle
	cm.currentHandle = cm.currentHandle + 2
	cm.handles[handle] = obj
	return handle
}

func (cm *concurrentHandleMap[T]) remove(handle uint64) {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	delete(cm.handles, handle)
}

func (cm *concurrentHandleMap[T]) tryGet(handle uint64) (T, bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	val, ok := cm.handles[handle]
	return val, ok
}

//export bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerMethod0
func bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerMethod0(uniffiHandle C.uint64_t, level C.RustBuffer, target C.RustBuffer, message C.RustBuffer, uniffiOutReturn *C.void, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterBarkLoggerINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	uniffiObj.Log(
		FfiConverterLogLevelINSTANCE.Lift(GoRustBuffer{
			inner: level,
		}),
		FfiConverterStringINSTANCE.Lift(GoRustBuffer{
			inner: target,
		}),
		FfiConverterStringINSTANCE.Lift(GoRustBuffer{
			inner: message,
		}),
	)

}

var UniffiVTableCallbackInterfaceBarkLoggerINSTANCE = C.UniffiVTableCallbackInterfaceBarkLogger{
	uniffiFree:  (C.UniffiCallbackInterfaceFree)(C.bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerFree),
	uniffiClone: (C.UniffiCallbackInterfaceClone)(C.bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerClone),
	log:         (C.UniffiCallbackInterfaceBarkLoggerMethod0)(C.bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerMethod0),
}

//export bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerFree
func bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerFree(handle C.uint64_t) {
	FfiConverterBarkLoggerINSTANCE.handleMap.remove(uint64(handle))
}

//export bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerClone
func bark_ffi_muniffi_logger_cgo_dispatchCallbackInterfaceBarkLoggerClone(handle C.uint64_t) C.uint64_t {
	val, ok := FfiConverterBarkLoggerINSTANCE.handleMap.tryGet(uint64(handle))
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}
	return C.uint64_t(FfiConverterBarkLoggerINSTANCE.handleMap.insert(val))
}

func (c FfiConverterBarkLogger) register() {
	C.uniffi_bark_ffi_fn_init_callback_vtable_barklogger(&UniffiVTableCallbackInterfaceBarkLoggerINSTANCE)
}

// Callback interface for custom onchain wallet implementations
//
// Foreign languages implement this trait to provide their own wallet functionality.
type CustomOnchainWalletCallbacks interface {
	// Get the wallet balance in satoshis
	//
	// An error falls back to the last value this returned, or 0 if none yet.
	GetBalance() (uint64, error)
	// Prepare a transaction to send to given destinations
	//
	// # Arguments
	// * `destinations` - List of destinations with addresses and amounts
	// * `fee_rate_sat_per_vb` - Fee rate in sats per vbyte
	//
	// # Returns
	// Base64-encoded PSBT. Rejected unless it pays every destination the
	// exact amount asked for; extra outputs (change) are fine.
	PrepareTx(destinations []Destination, feeRateSatPerVb uint64) (string, error)
	// Prepare a transaction that drains the wallet to a single address
	//
	// # Arguments
	// * `address` - Bitcoin address to drain to
	// * `fee_rate_sat_per_vb` - Fee rate in sats per vbyte
	//
	// # Returns
	// Base64-encoded PSBT. Rejected unless every output pays `address`.
	PrepareDrainTx(address string, feeRateSatPerVb uint64) (string, error)
	// Sign and finalize a PSBT
	//
	// # Arguments
	// * `psbt_base64` - Base64-encoded PSBT
	//
	// # Returns
	// Base64-encoded fully signed PSBT (all witnesses filled in). Rejected
	// unless the unsigned transaction is unchanged.
	FinishPsbt(psbtBase64 string) (string, error)
	// Whether a script pubkey belongs to the wallet's keychains
	//
	// # Arguments
	// * `script_pubkey_hex` - Hex-encoded script pubkey
	IsMine(scriptPubkeyHex string) (bool, error)
	// Register an unconfirmed transaction relevant to the wallet
	//
	// # Arguments
	// * `tx_hex` - Hex-encoded transaction
	RegisterTx(txHex string) error
	// Mark a wallet-known transaction as evicted from the mempool
	//
	// Bark calls this when a CPFP it broadcast was RBF-replaced by a competing
	// party, so the tx's inputs should return to coin selection immediately
	// instead of waiting for the sync eviction grace period. Only ever called
	// for a tx that has definitively been superseded on-chain.
	//
	// # Arguments
	// * `txid` - Hex-encoded transaction id
	EvictTx(txid string) error
	// Create a signed P2A CPFP transaction
	//
	// # Arguments
	// * `params` - CPFP transaction parameters
	//
	// # Returns
	// Hex-encoded signed CPFP transaction. Rejected unless it spends the
	// parent it was given.
	MakeSignedP2aCpfp(params CpfpParams) (string, error)
	// Store a signed P2A CPFP transaction in the wallet
	//
	// # Arguments
	// * `tx_hex` - Hex-encoded transaction
	StoreSignedP2aCpfp(txHex string) error
	// Sync the wallet with the chain.
	//
	// Called by Bark (e.g. the background daemon) to ask the wallet to refresh
	// its view of the chain before reading balances or building transactions.
	// Implementations bring their own chain backend up to date — Bark's chain
	// source is intentionally not passed across the FFI boundary.
	Sync() error
}

// Callback interface for custom onchain wallet implementations
//
// Foreign languages implement this trait to provide their own wallet functionality.
type CustomOnchainWalletCallbacksImpl struct {
	ffiObject FfiObject
}

// Get the wallet balance in satoshis
//
// An error falls back to the last value this returned, or 0 if none yet.
func (_self *CustomOnchainWalletCallbacksImpl) GetBalance() (uint64, error) {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) C.uint64_t {
		return C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_get_balance(
			_pointer, _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue uint64
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterUint64INSTANCE.Lift(_uniffiRV), nil
	}
}

// Prepare a transaction to send to given destinations
//
// # Arguments
// * `destinations` - List of destinations with addresses and amounts
// * `fee_rate_sat_per_vb` - Fee rate in sats per vbyte
//
// # Returns
// Base64-encoded PSBT. Rejected unless it pays every destination the
// exact amount asked for; extra outputs (change) are fine.
func (_self *CustomOnchainWalletCallbacksImpl) PrepareTx(destinations []Destination, feeRateSatPerVb uint64) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_prepare_tx(
				_pointer, FfiConverterSequenceDestinationINSTANCE.Lower(destinations), FfiConverterUint64INSTANCE.Lower(feeRateSatPerVb), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Prepare a transaction that drains the wallet to a single address
//
// # Arguments
// * `address` - Bitcoin address to drain to
// * `fee_rate_sat_per_vb` - Fee rate in sats per vbyte
//
// # Returns
// Base64-encoded PSBT. Rejected unless every output pays `address`.
func (_self *CustomOnchainWalletCallbacksImpl) PrepareDrainTx(address string, feeRateSatPerVb uint64) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_prepare_drain_tx(
				_pointer, FfiConverterStringINSTANCE.Lower(address), FfiConverterUint64INSTANCE.Lower(feeRateSatPerVb), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Sign and finalize a PSBT
//
// # Arguments
// * `psbt_base64` - Base64-encoded PSBT
//
// # Returns
// Base64-encoded fully signed PSBT (all witnesses filled in). Rejected
// unless the unsigned transaction is unchanged.
func (_self *CustomOnchainWalletCallbacksImpl) FinishPsbt(psbtBase64 string) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_finish_psbt(
				_pointer, FfiConverterStringINSTANCE.Lower(psbtBase64), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Whether a script pubkey belongs to the wallet's keychains
//
// # Arguments
// * `script_pubkey_hex` - Hex-encoded script pubkey
func (_self *CustomOnchainWalletCallbacksImpl) IsMine(scriptPubkeyHex string) (bool, error) {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_is_mine(
			_pointer, FfiConverterStringINSTANCE.Lower(scriptPubkeyHex), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue bool
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBoolINSTANCE.Lift(_uniffiRV), nil
	}
}

// Register an unconfirmed transaction relevant to the wallet
//
// # Arguments
// * `tx_hex` - Hex-encoded transaction
func (_self *CustomOnchainWalletCallbacksImpl) RegisterTx(txHex string) error {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_register_tx(
			_pointer, FfiConverterStringINSTANCE.Lower(txHex), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Mark a wallet-known transaction as evicted from the mempool
//
// Bark calls this when a CPFP it broadcast was RBF-replaced by a competing
// party, so the tx's inputs should return to coin selection immediately
// instead of waiting for the sync eviction grace period. Only ever called
// for a tx that has definitively been superseded on-chain.
//
// # Arguments
// * `txid` - Hex-encoded transaction id
func (_self *CustomOnchainWalletCallbacksImpl) EvictTx(txid string) error {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_evict_tx(
			_pointer, FfiConverterStringINSTANCE.Lower(txid), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Create a signed P2A CPFP transaction
//
// # Arguments
// * `params` - CPFP transaction parameters
//
// # Returns
// Hex-encoded signed CPFP transaction. Rejected unless it spends the
// parent it was given.
func (_self *CustomOnchainWalletCallbacksImpl) MakeSignedP2aCpfp(params CpfpParams) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_make_signed_p2a_cpfp(
				_pointer, FfiConverterCpfpParamsINSTANCE.Lower(params), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Store a signed P2A CPFP transaction in the wallet
//
// # Arguments
// * `tx_hex` - Hex-encoded transaction
func (_self *CustomOnchainWalletCallbacksImpl) StoreSignedP2aCpfp(txHex string) error {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_store_signed_p2a_cpfp(
			_pointer, FfiConverterStringINSTANCE.Lower(txHex), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Sync the wallet with the chain.
//
// Called by Bark (e.g. the background daemon) to ask the wallet to refresh
// its view of the chain before reading balances or building transactions.
// Implementations bring their own chain backend up to date — Bark's chain
// source is intentionally not passed across the FFI boundary.
func (_self *CustomOnchainWalletCallbacksImpl) Sync() error {
	_pointer := _self.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bark_ffi_fn_method_customonchainwalletcallbacks_sync(
			_pointer, _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}
func (object *CustomOnchainWalletCallbacksImpl) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterCustomOnchainWalletCallbacks struct {
	handleMap *concurrentHandleMap[CustomOnchainWalletCallbacks]
}

var FfiConverterCustomOnchainWalletCallbacksINSTANCE = FfiConverterCustomOnchainWalletCallbacks{
	handleMap: newConcurrentHandleMap[CustomOnchainWalletCallbacks](),
}

func (c FfiConverterCustomOnchainWalletCallbacks) Lift(handle C.uint64_t) CustomOnchainWalletCallbacks {
	if uint64(handle)&1 == 0 {
		// Rust-generated handle (even), construct a new object wrapping the handle
		result := &CustomOnchainWalletCallbacksImpl{
			newFfiObject(
				handle,
				func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
					return C.uniffi_bark_ffi_fn_clone_customonchainwalletcallbacks(handle, status)
				},
				func(handle C.uint64_t, status *C.RustCallStatus) {
					C.uniffi_bark_ffi_fn_free_customonchainwalletcallbacks(handle, status)
				},
			),
		}
		runtime.SetFinalizer(result, (*CustomOnchainWalletCallbacksImpl).Destroy)
		return result
	} else {
		// Go-generated handle (odd), retrieve from the handle map
		val, ok := c.handleMap.tryGet(uint64(handle))
		if !ok {
			panic(fmt.Errorf("no callback in handle map: %d", handle))
		}
		c.handleMap.remove(uint64(handle))
		return val
	}
}

func (c FfiConverterCustomOnchainWalletCallbacks) Read(reader io.Reader) CustomOnchainWalletCallbacks {
	return c.Lift(C.uint64_t(readUint64(reader)))
}

func (c FfiConverterCustomOnchainWalletCallbacks) Lower(value CustomOnchainWalletCallbacks) C.uint64_t {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the handle will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked handle.
	if val, ok := value.(*CustomOnchainWalletCallbacksImpl); ok {
		// Rust-backed object, clone the handle
		handle := val.ffiObject.incrementPointer("CustomOnchainWalletCallbacks")
		defer val.ffiObject.decrementPointer()
		return handle
	} else {
		// Go-backed object, insert into handle map
		return C.uint64_t(c.handleMap.insert(value))
	}
}

func (c FfiConverterCustomOnchainWalletCallbacks) Write(writer io.Writer, value CustomOnchainWalletCallbacks) {
	writeUint64(writer, uint64(c.Lower(value)))
}

func LiftFromExternalCustomOnchainWalletCallbacks(handle uint64) CustomOnchainWalletCallbacks {
	return FfiConverterCustomOnchainWalletCallbacksINSTANCE.Lift(C.uint64_t(handle))
}

func LowerToExternalCustomOnchainWalletCallbacks(value CustomOnchainWalletCallbacks) uint64 {
	return uint64(FfiConverterCustomOnchainWalletCallbacksINSTANCE.Lower(value))
}

type FfiDestroyerCustomOnchainWalletCallbacks struct{}

func (_ FfiDestroyerCustomOnchainWalletCallbacks) Destroy(value CustomOnchainWalletCallbacks) {
	if val, ok := value.(*CustomOnchainWalletCallbacksImpl); ok {
		val.Destroy()
	}
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod0
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod0(uniffiHandle C.uint64_t, uniffiOutReturn *C.uint64_t, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	res, err :=
		uniffiObj.GetBalance()

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

	*uniffiOutReturn = FfiConverterUint64INSTANCE.Lower(res)
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod1
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod1(uniffiHandle C.uint64_t, destinations C.RustBuffer, feeRateSatPerVb C.uint64_t, uniffiOutReturn *C.RustBuffer, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	res, err :=
		uniffiObj.PrepareTx(
			FfiConverterSequenceDestinationINSTANCE.Lift(GoRustBuffer{
				inner: destinations,
			}),
			FfiConverterUint64INSTANCE.Lift(feeRateSatPerVb),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

	*uniffiOutReturn = FfiConverterStringINSTANCE.Lower(res)
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod2
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod2(uniffiHandle C.uint64_t, address C.RustBuffer, feeRateSatPerVb C.uint64_t, uniffiOutReturn *C.RustBuffer, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	res, err :=
		uniffiObj.PrepareDrainTx(
			FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: address,
			}),
			FfiConverterUint64INSTANCE.Lift(feeRateSatPerVb),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

	*uniffiOutReturn = FfiConverterStringINSTANCE.Lower(res)
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod3
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod3(uniffiHandle C.uint64_t, psbtBase64 C.RustBuffer, uniffiOutReturn *C.RustBuffer, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	res, err :=
		uniffiObj.FinishPsbt(
			FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: psbtBase64,
			}),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

	*uniffiOutReturn = FfiConverterStringINSTANCE.Lower(res)
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod4
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod4(uniffiHandle C.uint64_t, scriptPubkeyHex C.RustBuffer, uniffiOutReturn *C.int8_t, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	res, err :=
		uniffiObj.IsMine(
			FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: scriptPubkeyHex,
			}),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

	*uniffiOutReturn = FfiConverterBoolINSTANCE.Lower(res)
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod5
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod5(uniffiHandle C.uint64_t, txHex C.RustBuffer, uniffiOutReturn *C.void, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	err :=
		uniffiObj.RegisterTx(
			FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: txHex,
			}),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod6
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod6(uniffiHandle C.uint64_t, txid C.RustBuffer, uniffiOutReturn *C.void, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	err :=
		uniffiObj.EvictTx(
			FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: txid,
			}),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod7
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod7(uniffiHandle C.uint64_t, params C.RustBuffer, uniffiOutReturn *C.RustBuffer, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	res, err :=
		uniffiObj.MakeSignedP2aCpfp(
			FfiConverterCpfpParamsINSTANCE.Lift(GoRustBuffer{
				inner: params,
			}),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

	*uniffiOutReturn = FfiConverterStringINSTANCE.Lower(res)
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod8
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod8(uniffiHandle C.uint64_t, txHex C.RustBuffer, uniffiOutReturn *C.void, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	err :=
		uniffiObj.StoreSignedP2aCpfp(
			FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: txHex,
			}),
		)

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod9
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod9(uniffiHandle C.uint64_t, uniffiOutReturn *C.void, callStatus *C.RustCallStatus) {
	handle := uint64(uniffiHandle)
	uniffiObj, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(handle)
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}

	err :=
		uniffiObj.Sync()

	if err != nil {
		var actualError *Error
		if errors.As(err, &actualError) {
			*callStatus = C.RustCallStatus{
				code:     C.int8_t(uniffiCallbackResultError),
				errorBuf: FfiConverterErrorINSTANCE.Lower(actualError),
			}
		} else {
			*callStatus = C.RustCallStatus{
				code: C.int8_t(uniffiCallbackUnexpectedResultError),
			}
		}
		return
	}

}

var UniffiVTableCallbackInterfaceCustomOnchainWalletCallbacksINSTANCE = C.UniffiVTableCallbackInterfaceCustomOnchainWalletCallbacks{
	uniffiFree:         (C.UniffiCallbackInterfaceFree)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksFree),
	uniffiClone:        (C.UniffiCallbackInterfaceClone)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksClone),
	getBalance:         (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod0)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod0),
	prepareTx:          (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod1)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod1),
	prepareDrainTx:     (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod2)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod2),
	finishPsbt:         (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod3)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod3),
	isMine:             (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod4)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod4),
	registerTx:         (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod5)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod5),
	evictTx:            (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod6)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod6),
	makeSignedP2aCpfp:  (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod7)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod7),
	storeSignedP2aCpfp: (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod8)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod8),
	sync:               (C.UniffiCallbackInterfaceCustomOnchainWalletCallbacksMethod9)(C.bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksMethod9),
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksFree
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksFree(handle C.uint64_t) {
	FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.remove(uint64(handle))
}

//export bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksClone
func bark_ffi_muniffi_custom_onchain_wallet_cgo_dispatchCallbackInterfaceCustomOnchainWalletCallbacksClone(handle C.uint64_t) C.uint64_t {
	val, ok := FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.tryGet(uint64(handle))
	if !ok {
		panic(fmt.Errorf("no callback in handle map: %d", handle))
	}
	return C.uint64_t(FfiConverterCustomOnchainWalletCallbacksINSTANCE.handleMap.insert(val))
}

func (c FfiConverterCustomOnchainWalletCallbacks) register() {
	C.uniffi_bark_ffi_fn_init_callback_vtable_customonchainwalletcallbacks(&UniffiVTableCallbackInterfaceCustomOnchainWalletCallbacksINSTANCE)
}

// Pull-based notification handle exposed over FFI.
//
// Obtain via `Wallet::notifications()`. Call `next_notification()` in a loop
// to receive events. Call `cancel_next_notification_wait()` to unblock a
// pending wait without destroying the underlying stream.
//
// Each call to `Wallet::notifications()` creates an independent stream backed
// by a new broadcast receiver — existing holders are unaffected.
//
// This holder is intended for a single consumer loop. Concurrent calls to
// `next_notification()` on the same holder are not supported and will return
// `None` immediately.
type NotificationHolderInterface interface {
	// Cancel the currently pending `next_notification()` wait.
	//
	// Causes a blocked `next_notification()` to return `None`.
	// Has no effect if no wait is currently active.
	//
	// This does NOT destroy the underlying `NotificationStream`; a subsequent
	// call to `next_notification()` will work normally and wait for new events.
	CancelNextNotificationWait()
	// Wait for the next wallet notification.
	//
	// Returns `None` when:
	// - `cancel_next_notification_wait()` was called while this was pending
	// (cancellation only affects the current wait; the stream lives on)
	// - The wallet's notification source was shut down permanently
	//
	// Returns an error if called concurrently on the same holder.
	//
	// After a cancellation this method can be called again normally — the
	// underlying `NotificationStream` is preserved in `self.stream` and a
	// fresh per-wait cancel channel is created on every entry.
	NextNotification() (*WalletNotification, error)
}

// Pull-based notification handle exposed over FFI.
//
// Obtain via `Wallet::notifications()`. Call `next_notification()` in a loop
// to receive events. Call `cancel_next_notification_wait()` to unblock a
// pending wait without destroying the underlying stream.
//
// Each call to `Wallet::notifications()` creates an independent stream backed
// by a new broadcast receiver — existing holders are unaffected.
//
// This holder is intended for a single consumer loop. Concurrent calls to
// `next_notification()` on the same holder are not supported and will return
// `None` immediately.
type NotificationHolder struct {
	ffiObject FfiObject
}

// Cancel the currently pending `next_notification()` wait.
//
// Causes a blocked `next_notification()` to return `None`.
// Has no effect if no wait is currently active.
//
// This does NOT destroy the underlying `NotificationStream`; a subsequent
// call to `next_notification()` will work normally and wait for new events.
func (_self *NotificationHolder) CancelNextNotificationWait() {
	_pointer := _self.ffiObject.incrementPointer("*NotificationHolder")
	defer _self.ffiObject.decrementPointer()
	rustCall(func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bark_ffi_fn_method_notificationholder_cancel_next_notification_wait(
			_pointer, _uniffiStatus)
		return false
	})
}

// Wait for the next wallet notification.
//
// Returns `None` when:
// - `cancel_next_notification_wait()` was called while this was pending
// (cancellation only affects the current wait; the stream lives on)
// - The wallet's notification source was shut down permanently
//
// Returns an error if called concurrently on the same holder.
//
// After a cancellation this method can be called again normally — the
// underlying `NotificationStream` is preserved in `self.stream` and a
// fresh per-wait cancel channel is created on every entry.
func (_self *NotificationHolder) NextNotification() (*WalletNotification, error) {
	_pointer := _self.ffiObject.incrementPointer("*NotificationHolder")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *WalletNotification {
			return FfiConverterOptionalWalletNotificationINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_notificationholder_next_notification(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}
func (object *NotificationHolder) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterNotificationHolder struct{}

var FfiConverterNotificationHolderINSTANCE = FfiConverterNotificationHolder{}

func (c FfiConverterNotificationHolder) Lift(handle C.uint64_t) *NotificationHolder {
	result := &NotificationHolder{
		newFfiObject(
			handle,
			func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
				return C.uniffi_bark_ffi_fn_clone_notificationholder(handle, status)
			},
			func(handle C.uint64_t, status *C.RustCallStatus) {
				C.uniffi_bark_ffi_fn_free_notificationholder(handle, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*NotificationHolder).Destroy)
	return result
}

func (c FfiConverterNotificationHolder) Read(reader io.Reader) *NotificationHolder {
	return c.Lift(C.uint64_t(readUint64(reader)))
}

func (c FfiConverterNotificationHolder) Lower(value *NotificationHolder) C.uint64_t {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the handle will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked handle.
	handle := value.ffiObject.incrementPointer("*NotificationHolder")
	defer value.ffiObject.decrementPointer()
	return handle
}

func (c FfiConverterNotificationHolder) Write(writer io.Writer, value *NotificationHolder) {
	writeUint64(writer, uint64(c.Lower(value)))
}

func LiftFromExternalNotificationHolder(handle uint64) *NotificationHolder {
	return FfiConverterNotificationHolderINSTANCE.Lift(C.uint64_t(handle))
}

func LowerToExternalNotificationHolder(value *NotificationHolder) uint64 {
	return uint64(FfiConverterNotificationHolderINSTANCE.Lower(value))
}

type FfiDestroyerNotificationHolder struct{}

func (_ FfiDestroyerNotificationHolder) Destroy(value *NotificationHolder) {
	value.Destroy()
}

// UniFFI-facing onchain wallet. Supports two backends:
// - BDK (real onchain wallet via `bark::onchain::OnchainWallet`)
// - Callback (foreign-language implementation)
type OnchainWalletInterface interface {
	Balance() (OnchainBalance, error)
	// Mark a wallet-known transaction as evicted from the mempool, so its
	// inputs return to coin selection immediately. Only for a tx that has
	// definitively been superseded on-chain (e.g. an RBF-replaced exit CPFP);
	// evicting a still-in-flight tx invites a self-inflicted double-spend.
	EvictTx(txid string) error
	// Cached network fee-rate estimates from the wallet's chain source.
	FeeRates() (FeeRates, error)
	// Discover the wallet's pre-existing on-chain history. Run once after
	// restoring a wallet from a mnemonic: `sync` only covers addresses this
	// wallet instance has already revealed, so it never finds transactions
	// made by a previous incarnation. Gap-limited full scan on esplora
	// (`birthday_height` is ignored there), block scan from `birthday_height`
	// on bitcoind. Returns the total balance in sats afterwards.
	InitialScan(birthdayHeight *uint32) (uint64, error)
	NewAddress() (string, error)
	Send(address string, amountSats uint64, feeRateSatPerVb uint64) (string, error)
	Sync() (uint64, error)
	// Current chain tip height from the wallet's chain source.
	TipHeight() (uint32, error)
	// Every wallet transaction with fee, balance change, confirmation and
	// CPFP flag. Requires a prior `sync` to be meaningful.
	Transactions() ([]WalletTransaction, error)
	// The wallet's unspent outputs. Requires a prior `sync` to be meaningful.
	Utxos() ([]OnchainUtxo, error)
}

// UniFFI-facing onchain wallet. Supports two backends:
// - BDK (real onchain wallet via `bark::onchain::OnchainWallet`)
// - Callback (foreign-language implementation)
type OnchainWallet struct {
	ffiObject FfiObject
}

// Callback-backed wallet for foreign-language implementations.
func OnchainWalletCustom(callbacks CustomOnchainWalletCallbacks) (*OnchainWallet, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) C.uint64_t {
		return C.uniffi_bark_ffi_fn_constructor_onchainwallet_custom(FfiConverterCustomOnchainWalletCallbacksINSTANCE.Lower(callbacks), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *OnchainWallet
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterOnchainWalletINSTANCE.Lift(_uniffiRV), nil
	}
}

// BDK-backed wallet. Opens the shared sqlite cache.
func OnchainWalletDefault(network Network, mnemonic string, config Config, datadir string) (*OnchainWallet, error) {
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
			res := C.ffi_bark_ffi_rust_future_complete_u64(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint64_t) *OnchainWallet {
			return FfiConverterOnchainWalletINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_constructor_onchainwallet_default(FfiConverterNetworkINSTANCE.Lower(network), FfiConverterStringINSTANCE.Lower(mnemonic), FfiConverterConfigINSTANCE.Lower(config), FfiConverterStringINSTANCE.Lower(datadir)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u64(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u64(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *OnchainWallet) Balance() (OnchainBalance, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) OnchainBalance {
			return FfiConverterOnchainBalanceINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_balance(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Mark a wallet-known transaction as evicted from the mempool, so its
// inputs return to coin selection immediately. Only for a tx that has
// definitively been superseded on-chain (e.g. an RBF-replaced exit CPFP);
// evicting a still-in-flight tx invites a self-inflicted double-spend.
func (_self *OnchainWallet) EvictTx(txid string) error {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_onchainwallet_evict_tx(
			_pointer, FfiConverterStringINSTANCE.Lower(txid)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Cached network fee-rate estimates from the wallet's chain source.
func (_self *OnchainWallet) FeeRates() (FeeRates, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeRates {
			return FfiConverterFeeRatesINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_fee_rates(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Discover the wallet's pre-existing on-chain history. Run once after
// restoring a wallet from a mnemonic: `sync` only covers addresses this
// wallet instance has already revealed, so it never finds transactions
// made by a previous incarnation. Gap-limited full scan on esplora
// (`birthday_height` is ignored there), block scan from `birthday_height`
// on bitcoind. Returns the total balance in sats afterwards.
func (_self *OnchainWallet) InitialScan(birthdayHeight *uint32) (uint64, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
			res := C.ffi_bark_ffi_rust_future_complete_u64(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint64_t) uint64 {
			return FfiConverterUint64INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_initial_scan(
			_pointer, FfiConverterOptionalUint32INSTANCE.Lower(birthdayHeight)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u64(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u64(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *OnchainWallet) NewAddress() (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_new_address(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *OnchainWallet) Send(address string, amountSats uint64, feeRateSatPerVb uint64) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_send(
			_pointer, FfiConverterStringINSTANCE.Lower(address), FfiConverterUint64INSTANCE.Lower(amountSats), FfiConverterUint64INSTANCE.Lower(feeRateSatPerVb)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *OnchainWallet) Sync() (uint64, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
			res := C.ffi_bark_ffi_rust_future_complete_u64(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint64_t) uint64 {
			return FfiConverterUint64INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_sync(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u64(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u64(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Current chain tip height from the wallet's chain source.
func (_self *OnchainWallet) TipHeight() (uint32, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint32_t {
			res := C.ffi_bark_ffi_rust_future_complete_u32(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint32_t) uint32 {
			return FfiConverterUint32INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_tip_height(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u32(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u32(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Every wallet transaction with fee, balance change, confirmation and
// CPFP flag. Requires a prior `sync` to be meaningful.
func (_self *OnchainWallet) Transactions() ([]WalletTransaction, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []WalletTransaction {
			return FfiConverterSequenceWalletTransactionINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_transactions(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// The wallet's unspent outputs. Requires a prior `sync` to be meaningful.
func (_self *OnchainWallet) Utxos() ([]OnchainUtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*OnchainWallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []OnchainUtxo {
			return FfiConverterSequenceOnchainUtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_onchainwallet_utxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}
func (object *OnchainWallet) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterOnchainWallet struct{}

var FfiConverterOnchainWalletINSTANCE = FfiConverterOnchainWallet{}

func (c FfiConverterOnchainWallet) Lift(handle C.uint64_t) *OnchainWallet {
	result := &OnchainWallet{
		newFfiObject(
			handle,
			func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
				return C.uniffi_bark_ffi_fn_clone_onchainwallet(handle, status)
			},
			func(handle C.uint64_t, status *C.RustCallStatus) {
				C.uniffi_bark_ffi_fn_free_onchainwallet(handle, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*OnchainWallet).Destroy)
	return result
}

func (c FfiConverterOnchainWallet) Read(reader io.Reader) *OnchainWallet {
	return c.Lift(C.uint64_t(readUint64(reader)))
}

func (c FfiConverterOnchainWallet) Lower(value *OnchainWallet) C.uint64_t {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the handle will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked handle.
	handle := value.ffiObject.incrementPointer("*OnchainWallet")
	defer value.ffiObject.decrementPointer()
	return handle
}

func (c FfiConverterOnchainWallet) Write(writer io.Writer, value *OnchainWallet) {
	writeUint64(writer, uint64(c.Lower(value)))
}

func LiftFromExternalOnchainWallet(handle uint64) *OnchainWallet {
	return FfiConverterOnchainWalletINSTANCE.Lift(C.uint64_t(handle))
}

func LowerToExternalOnchainWallet(value *OnchainWallet) uint64 {
	return uint64(FfiConverterOnchainWalletINSTANCE.Lower(value))
}

type FfiDestroyerOnchainWallet struct{}

func (_ FfiDestroyerOnchainWallet) Destroy(value *OnchainWallet) {
	value.Destroy()
}

// UniFFI-facing Bark wallet.
//
// Wraps `core::Wallet` and adds:
// - `run_async` runtime offload on every entry point
// - daemon shutdown on drop
// - LNURL `pay_lightning_address`
// - Daemon control methods
// - Notification holder
// - Callback onchain dispatch
type WalletInterface interface {
	AllExitsClaimableAtHeight() (*uint32, error)
	AllVtxos() ([]Vtxo, error)
	// Opt one stuck send into auto-exiting its HTLCs as they near expiry.
	AllowLightningSendToExit(paymentHash string) error
	ArkInfo() *ArkInfo
	// Force-exit an unfinished lightning receive.
	AttemptLightningReceiveExit(paymentHash string) error
	Balance() (Balance, error)
	BoardAll() (PendingBoard, error)
	BoardAmount(amountSats uint64) (PendingBoard, error)
	BoardFundingAddress() (BoardFundingInfo, error)
	BoardPsbt(psbtBase64 string, keypairIndex uint32, expiryHeight uint32) (PendingBoard, error)
	Bolt11Invoice(amountSats uint64, description *string, token *string) (LightningInvoice, error)
	// Create an invoice whose claimed VTXO is delivered to `claim_destination`
	// (an Ark address), letting that wallet receive while offline.
	//
	// The claim is signed directly to that address's own policy, so this wallet
	// has no custodial control and cannot redirect the payment. It can still
	// strand it: delivering the signed output to the destination's mailbox is a
	// separate step only this wallet can perform, and the recipient has no
	// independent way to recover the funds until it happens. Delivery resumes
	// automatically on restart, so a crash recovers on its own — but running
	// this for someone else means they trust you to stay online and eventually
	// deliver, not that they trust you with custody.
	//
	// A `claim_destination` owned by this wallet is claimed locally instead of
	// going through its mailbox.
	Bolt11InvoiceForAddress(amountSats uint64, claimDestination string, description *string, token *string) (LightningInvoice, error)
	BroadcastTx(txHex string) (string, error)
	CancelAllPendingRounds() error
	// Cancel a unilateral exit still in its abortable window.
	//
	// The VTXO stays spendable either way, and a lock you took yourself is
	// yours to release. Expected refusals come back in the result rather than
	// as an error, and cancelling twice succeeds.
	CancelExit(vtxoId string) (ExitCancelResult, error)
	CancelLightningReceive(paymentHash string) error
	CancelPendingRound(roundId uint32) error
	CheckLightningPayment(paymentHash string, wait bool) (LightningSendStatus, error)
	ClaimableLightningReceiveBalanceSats() (uint64, error)
	Config() Config
	// Build a PSBT claiming exited VTXOs to `address`.
	//
	// Draining everything must be asked for with `drain_all`, so an id list
	// filtered down to nothing is an error rather than a sweep. Ids are parsed
	// all-or-nothing; well-formed ids that are not claimable are skipped.
	DrainExits(vtxoIds []string, drainAll bool, address string, feeRateSatPerVb *uint64) (ExitClaimTransaction, error)
	EstimateArkoorPaymentFee(amountSats uint64) (FeeEstimate, error)
	EstimateBoardFee(amountSats uint64) (FeeEstimate, error)
	// Estimate the onchain cost of unilaterally (emergency) exiting VTXOs.
	// Mirrors bark-rest `GET /exits/fee`.
	//
	// Pass an empty `vtxo_ids` to price exiting the whole wallet (every
	// spendable VTXO). `fee_rate_sat_per_vb` applies to both legs; omit it to
	// use the chain's fast rate for the broadcast leg and regular rate for
	// the claim leg. `destination` only affects the claim transaction's weight.
	//
	// The onchain wallet is synced first so `fundable` reflects the current
	// confirmed balance. Requires a BDK onchain wallet: callback-backed
	// wallets (`CustomOnchainWalletCallbacks`) cannot simulate the CPFP walk
	// and return an error.
	EstimateEmergencyExitFee(vtxoIds []string, feeRateSatPerVb *uint64, destination *string) (EmergencyExitFeeEstimate, error)
	EstimateLightningReceiveFee(amountSats uint64) (FeeEstimate, error)
	EstimateLightningSendFee(amountSats uint64) (FeeEstimate, error)
	EstimateOffboardAllFee(address string) (FeeEstimate, error)
	EstimateOffboardFee(address string, vtxoIds []string) (FeeEstimate, error)
	EstimateRefreshFee(vtxoIds []string) (FeeEstimate, error)
	EstimateSendOnchainFee(address string, amountSats uint64) (FeeEstimate, error)
	Fingerprint() string
	GetExitStatus(vtxoId string, includeHistory bool, includeTransactions bool) (*ExitTransactionStatus, error)
	GetExitVtxos() ([]ExitVtxo, error)
	GetExpiringVtxos(thresholdBlocks uint32) ([]Vtxo, error)
	GetFirstExpiringVtxoBlockheight() (*uint32, error)
	GetNextRequiredRefreshBlockheight() (*uint32, error)
	GetVtxoById(vtxoId string) (Vtxo, error)
	GetVtxosToRefresh() ([]Vtxo, error)
	HasPendingExits() (bool, error)
	History() ([]Movement, error)
	HistoryByPaymentMethod(paymentMethodType string, paymentMethodValue string) ([]Movement, error)
	// Import a VTXO from its serialized form (hex or base64).
	//
	// The VTXO is stored in the state the server reports for it, so one that
	// was already spent is recorded as spent rather than refused. Pass `args`
	// to widen the key-scan gap limit, skip the server status check, or allow
	// partial success; omit it for the defaults.
	//
	// The first parameter keeps its historical `vtxo_base64` name for foreign
	// binding compatibility (uniffi exposes parameter names), but hex as
	// returned by [`Wallet::vtxo_encoded`] is accepted too.
	ImportVtxo(vtxoBase64 string, args *ImportVtxoArgs) error
	// Import several VTXOs (hex or base64) under a single key scan and a
	// single write, which is why this is not just a loop over `import_vtxo`.
	//
	// Returns the ids now held — whether this call stored them or found them
	// already present — so a failed batch can be retried. One VTXO that cannot
	// be imported discards the whole batch unless `args.allow_partial` is set,
	// in which case the ones that did import are kept.
	ImportVtxos(encodedVtxos []string, args *ImportVtxoArgs) ([]string, error)
	// Cheap "has this invoice ever been paid?", answered from the local fact
	// table without consulting the server.
	IsInvoicePaid(paymentHash string) (bool, error)
	// Triage a payment hash: settled or in-progress. Errors if no lightning
	// receive is known for this payment hash.
	LightningReceiveState(paymentHash string) (LightningReceive, error)
	// Read-only triage, for polling after a send started with `wait = false`.
	// Unlike `checkLightningPayment`, never advances the action.
	LightningSendState(paymentHash string) (LightningSendStatus, error)
	ListClaimableExits() ([]ExitVtxo, error)
	// Reserve VTXOs so wallet-driven flows leave them alone: a locked VTXO is
	// excluded from coin selection.
	//
	// `holder` is what `unlockVtxos` matches on to stop one subsystem
	// releasing another's lock. Atomic, and re-locking with the same holder
	// is a no-op.
	LockVtxos(vtxoIds []string, holder *VtxoLockHolder) error
	// Create a hex-encoded authorization that lets whoever holds it read this
	// wallet's mailbox from the Ark server for `expiry_secs` from now, so 86400
	// for 24 hours. An authorization cannot be revoked early, so keep the
	// window short.
	MailboxAuthorization(expirySecs uint32) (string, error)
	MailboxIdentifier() (string, error)
	Maintenance() error
	MaintenanceDelegated() error
	MaintenanceRefresh() (*string, error)
	Network() (Network, error)
	NewAddress() (string, error)
	NewAddressWithIndex() (AddressWithIndex, error)
	NextRoundStartTime() (uint64, error)
	Notifications() *NotificationHolder
	OffboardAll(bitcoinAddress string) (OffboardResult, error)
	OffboardVtxos(vtxoIds []string, bitcoinAddress string) (OffboardResult, error)
	// Pay to a Lightning Address (`user@domain`), resolved via LNURL-pay.
	PayLightningAddress(lightningAddress string, amountSats uint64, comment *string, wait bool) (LightningSendStatus, error)
	PayLightningInvoice(invoice string, amountSats *uint64, wait bool) (LightningSendStatus, error)
	PayLightningOffer(offer string, amountSats *uint64, wait bool) (LightningSendStatus, error)
	// Pay a raw LNURL-pay link (`lnurl1…`).
	//
	// Resolves the LNURL-pay endpoint to a BOLT11 invoice and pays it. Errors
	// if the link decodes to a non-pay LNURL (auth, withdraw, channel).
	PayLnurl(lnurl string, amountSats uint64, comment *string, wait bool) (LightningSendStatus, error)
	PeekAddress(index uint32) (string, error)
	PendingBoardVtxos() ([]Vtxo, error)
	PendingBoards() ([]PendingBoard, error)
	PendingExitsTotalSats() (uint64, error)
	PendingLightningReceives() ([]LightningReceive, error)
	PendingLightningSendVtxos() ([]Vtxo, error)
	PendingLightningSends() ([]LightningSend, error)
	PendingRoundInputVtxos() ([]Vtxo, error)
	PendingRoundStates() ([]RoundState, error)
	ProgressExits(feeRateSatPerVb *uint64) ([]ExitProgressStatus, error)
	ProgressPendingRounds() error
	Properties() (WalletProperties, error)
	// Recover the given VTXO ids from the server, importing the ones this
	// wallet owns that are still spendable. Use it to retry ids a previous
	// scan reported as `failed`.
	//
	// `gap_limit` overrides `Config.vtxo_key_gap_limit` for the key scan that
	// decides which of `vtxo_ids` this wallet owns. Widen it to reach ids a
	// previous scan bucketed as `foreign`.
	RecoverVtxos(vtxoIds []string, gapLimit *uint32) (RecoveryReport, error)
	// The report of the seed-recovery scan that ran during `Wallet::open`, or
	// none if the scan did not complete. Use `recovery_status` to tell a scan
	// that failed apart from one that never ran.
	RecoveryReport() *RecoveryReport
	// Outcome of the seed-recovery scan that ran during `Wallet::open`.
	//
	// Recovery only runs on the open that creates the wallet locally, and not
	// at all when `WalletOpenArgs.skip_recovery` is set, so this is `NotRun`
	// on every subsequent open. `Failed` means the scan errored before
	// producing a report — bark logs that and lets open succeed — so funds
	// may be missing until a retry; `Completed` carries the report, and
	// `isComplete == false` there means funds may still be missing. Retry the
	// report's `failed` ids with `recoverVtxos`.
	RecoveryStatus() RecoveryStatus
	RefreshServer() error
	RefreshVtxos(vtxoIds []string) (*string, error)
	RefreshVtxosDelegated(vtxoIds []string) (*RoundState, error)
	// Schedule a delegated refresh for `scheduled_height` instead of the next
	// round. The refresh fee is priced against the VTXO's remaining lifetime at
	// that height, and the server charges less the closer a VTXO is to expiry,
	// so scheduling further out never costs more than refreshing now.
	RefreshVtxosScheduled(vtxoIds []string, scheduledHeight uint32) (*RoundState, error)
	// Start the background daemon. The onchain wallet used by the daemon is
	// the one supplied to `Wallet::open` (see `WalletOpenArgs.onchain`).
	RunDaemon() error
	// Send an out-of-round payment to an Ark address.
	//
	// An address this bark cannot deliver to (see `validateArkoorAddress`) is
	// rejected up front, leaving the VTXOs spendable rather than cosigned.
	SendArkoorPayment(arkAddress string, amountSats uint64) error
	SendOnchain(address string, amountSats uint64) (string, error)
	SignExitClaimInputs(psbtBase64 string) (string, error)
	SpendableVtxos() ([]Vtxo, error)
	StartExitForEntireWallet() error
	StartExitForVtxos(vtxoIds []string) error
	// Like `startExitForVtxos`, but skips dust and standardness checks.
	//
	// Only for VTXOs already onchain, or a node that accepts non-standard
	// transactions; otherwise the exit transactions may be unrelayable.
	StartExitForVtxosIncludingNonStandard(vtxoIds []string) error
	StopDaemon() error
	// Stop the background daemon and wait until its tasks have finished, so
	// nothing runs in the background afterwards (e.g. before deleting the
	// wallet's datadir). No-op when no daemon is running.
	StopDaemonWait() error
	// Failed lightning sends whose HTLC revocation also failed.
	StuckFailedLightningSends() ([]LightningSend, error)
	Sync() error
	SyncExits() error
	// Scan for VTXOs that were force-exited on-chain without the user asking
	// and route them into the unilateral-exit flow so the funds can be claimed.
	//
	// This already runs automatically as part of [`Self::sync`]; call it
	// directly to trigger the scan on demand.
	SyncForceExitedVtxos() error
	SyncPendingBoards() error
	TryClaimAllLightningReceives(wait bool) ([]LightningReceive, error)
	TryClaimLightningReceive(paymentHash string, wait bool) (LightningReceive, error)
	// Release VTXOs locked by `lockVtxos`.
	//
	// Every VTXO must currently be held by `expected_holder` or nothing is
	// unlocked; that is what stops cleanup freeing a VTXO a payment or
	// in-flight round has since claimed. Leaving it unset bypasses the guard
	// entirely, so reserve that for recovery.
	UnlockVtxos(vtxoIds []string, expectedHolder *VtxoLockHolder) error
	// Whether this wallet can pay `address` out-of-round: same network and
	// server, a supported VTXO policy, and only delivery mechanisms this bark
	// supports. An address listing no delivery mechanism is valid. Errors only
	// when the address does not parse.
	ValidateArkoorAddress(address string) (bool, error)
	// Hex-encoded serialization of the full VTXO (genesis chain included),
	// re-importable via [`Wallet::import_vtxo`]. Mirrors bark-rest
	// `GET /vtxos/{id}/encoded`.
	VtxoEncoded(vtxoId string) (string, error)
	Vtxos() ([]Vtxo, error)
}

// UniFFI-facing Bark wallet.
//
// Wraps `core::Wallet` and adds:
// - `run_async` runtime offload on every entry point
// - daemon shutdown on drop
// - LNURL `pay_lightning_address`
// - Daemon control methods
// - Notification holder
// - Callback onchain dispatch
type Wallet struct {
	ffiObject FfiObject
}

// Open a wallet (creating it first if `create_if_not_exists` is set),
// mirroring [`bark::Wallet::open`]: a single entry point with everything
// else optional (see [`WalletOpenArgs`]). For the explicit
// initialize-but-don't-open path, use the top-level `init_wallet`.
//
// `mnemonic_or_seed` accepts either a BIP-39 mnemonic phrase or a 64-byte
// hex-encoded seed.
func WalletOpen(network Network, mnemonicOrSeed string, config Config, args WalletOpenArgs) (*Wallet, error) {
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
			res := C.ffi_bark_ffi_rust_future_complete_u64(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint64_t) *Wallet {
			return FfiConverterWalletINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_constructor_wallet_open(FfiConverterNetworkINSTANCE.Lower(network), FfiConverterStringINSTANCE.Lower(mnemonicOrSeed), FfiConverterConfigINSTANCE.Lower(config), FfiConverterWalletOpenArgsINSTANCE.Lower(args)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u64(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u64(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) AllExitsClaimableAtHeight() (*uint32, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *uint32 {
			return FfiConverterOptionalUint32INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_all_exits_claimable_at_height(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) AllVtxos() ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_all_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Opt one stuck send into auto-exiting its HTLCs as they near expiry.
func (_self *Wallet) AllowLightningSendToExit(paymentHash string) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_allow_lightning_send_to_exit(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) ArkInfo() *ArkInfo {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, _ := uniffiRustCallAsync[error](
		nil,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *ArkInfo {
			return FfiConverterOptionalArkInfoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_ark_info(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	return res
}

// Force-exit an unfinished lightning receive.
func (_self *Wallet) AttemptLightningReceiveExit(paymentHash string) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_attempt_lightning_receive_exit(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) Balance() (Balance, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) Balance {
			return FfiConverterBalanceINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_balance(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) BoardAll() (PendingBoard, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) PendingBoard {
			return FfiConverterPendingBoardINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_board_all(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) BoardAmount(amountSats uint64) (PendingBoard, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) PendingBoard {
			return FfiConverterPendingBoardINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_board_amount(
			_pointer, FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) BoardFundingAddress() (BoardFundingInfo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) BoardFundingInfo {
			return FfiConverterBoardFundingInfoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_board_funding_address(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) BoardPsbt(psbtBase64 string, keypairIndex uint32, expiryHeight uint32) (PendingBoard, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) PendingBoard {
			return FfiConverterPendingBoardINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_board_psbt(
			_pointer, FfiConverterStringINSTANCE.Lower(psbtBase64), FfiConverterUint32INSTANCE.Lower(keypairIndex), FfiConverterUint32INSTANCE.Lower(expiryHeight)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) Bolt11Invoice(amountSats uint64, description *string, token *string) (LightningInvoice, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningInvoice {
			return FfiConverterLightningInvoiceINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_bolt11_invoice(
			_pointer, FfiConverterUint64INSTANCE.Lower(amountSats), FfiConverterOptionalStringINSTANCE.Lower(description), FfiConverterOptionalStringINSTANCE.Lower(token)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Create an invoice whose claimed VTXO is delivered to `claim_destination`
// (an Ark address), letting that wallet receive while offline.
//
// The claim is signed directly to that address's own policy, so this wallet
// has no custodial control and cannot redirect the payment. It can still
// strand it: delivering the signed output to the destination's mailbox is a
// separate step only this wallet can perform, and the recipient has no
// independent way to recover the funds until it happens. Delivery resumes
// automatically on restart, so a crash recovers on its own — but running
// this for someone else means they trust you to stay online and eventually
// deliver, not that they trust you with custody.
//
// A `claim_destination` owned by this wallet is claimed locally instead of
// going through its mailbox.
func (_self *Wallet) Bolt11InvoiceForAddress(amountSats uint64, claimDestination string, description *string, token *string) (LightningInvoice, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningInvoice {
			return FfiConverterLightningInvoiceINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_bolt11_invoice_for_address(
			_pointer, FfiConverterUint64INSTANCE.Lower(amountSats), FfiConverterStringINSTANCE.Lower(claimDestination), FfiConverterOptionalStringINSTANCE.Lower(description), FfiConverterOptionalStringINSTANCE.Lower(token)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) BroadcastTx(txHex string) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_broadcast_tx(
			_pointer, FfiConverterStringINSTANCE.Lower(txHex)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) CancelAllPendingRounds() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_cancel_all_pending_rounds(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Cancel a unilateral exit still in its abortable window.
//
// The VTXO stays spendable either way, and a lock you took yourself is
// yours to release. Expected refusals come back in the result rather than
// as an error, and cancelling twice succeeds.
func (_self *Wallet) CancelExit(vtxoId string) (ExitCancelResult, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) ExitCancelResult {
			return FfiConverterExitCancelResultINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_cancel_exit(
			_pointer, FfiConverterStringINSTANCE.Lower(vtxoId)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) CancelLightningReceive(paymentHash string) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_cancel_lightning_receive(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) CancelPendingRound(roundId uint32) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_cancel_pending_round(
			_pointer, FfiConverterUint32INSTANCE.Lower(roundId)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) CheckLightningPayment(paymentHash string, wait bool) (LightningSendStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningSendStatus {
			return FfiConverterLightningSendStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_check_lightning_payment(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash), FfiConverterBoolINSTANCE.Lower(wait)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) ClaimableLightningReceiveBalanceSats() (uint64, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
			res := C.ffi_bark_ffi_rust_future_complete_u64(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint64_t) uint64 {
			return FfiConverterUint64INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_claimable_lightning_receive_balance_sats(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u64(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u64(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) Config() Config {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, _ := uniffiRustCallAsync[error](
		nil,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) Config {
			return FfiConverterConfigINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_config(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	return res
}

// Build a PSBT claiming exited VTXOs to `address`.
//
// Draining everything must be asked for with `drain_all`, so an id list
// filtered down to nothing is an error rather than a sweep. Ids are parsed
// all-or-nothing; well-formed ids that are not claimable are skipped.
func (_self *Wallet) DrainExits(vtxoIds []string, drainAll bool, address string, feeRateSatPerVb *uint64) (ExitClaimTransaction, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) ExitClaimTransaction {
			return FfiConverterExitClaimTransactionINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_drain_exits(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds), FfiConverterBoolINSTANCE.Lower(drainAll), FfiConverterStringINSTANCE.Lower(address), FfiConverterOptionalUint64INSTANCE.Lower(feeRateSatPerVb)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateArkoorPaymentFee(amountSats uint64) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_arkoor_payment_fee(
			_pointer, FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateBoardFee(amountSats uint64) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_board_fee(
			_pointer, FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Estimate the onchain cost of unilaterally (emergency) exiting VTXOs.
// Mirrors bark-rest `GET /exits/fee`.
//
// Pass an empty `vtxo_ids` to price exiting the whole wallet (every
// spendable VTXO). `fee_rate_sat_per_vb` applies to both legs; omit it to
// use the chain's fast rate for the broadcast leg and regular rate for
// the claim leg. `destination` only affects the claim transaction's weight.
//
// The onchain wallet is synced first so `fundable` reflects the current
// confirmed balance. Requires a BDK onchain wallet: callback-backed
// wallets (`CustomOnchainWalletCallbacks`) cannot simulate the CPFP walk
// and return an error.
func (_self *Wallet) EstimateEmergencyExitFee(vtxoIds []string, feeRateSatPerVb *uint64, destination *string) (EmergencyExitFeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) EmergencyExitFeeEstimate {
			return FfiConverterEmergencyExitFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_emergency_exit_fee(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds), FfiConverterOptionalUint64INSTANCE.Lower(feeRateSatPerVb), FfiConverterOptionalStringINSTANCE.Lower(destination)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateLightningReceiveFee(amountSats uint64) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_lightning_receive_fee(
			_pointer, FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateLightningSendFee(amountSats uint64) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_lightning_send_fee(
			_pointer, FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateOffboardAllFee(address string) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_offboard_all_fee(
			_pointer, FfiConverterStringINSTANCE.Lower(address)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateOffboardFee(address string, vtxoIds []string) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_offboard_fee(
			_pointer, FfiConverterStringINSTANCE.Lower(address), FfiConverterSequenceStringINSTANCE.Lower(vtxoIds)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateRefreshFee(vtxoIds []string) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_refresh_fee(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) EstimateSendOnchainFee(address string, amountSats uint64) (FeeEstimate, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) FeeEstimate {
			return FfiConverterFeeEstimateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_estimate_send_onchain_fee(
			_pointer, FfiConverterStringINSTANCE.Lower(address), FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) Fingerprint() string {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_wallet_fingerprint(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Wallet) GetExitStatus(vtxoId string, includeHistory bool, includeTransactions bool) (*ExitTransactionStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *ExitTransactionStatus {
			return FfiConverterOptionalExitTransactionStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_get_exit_status(
			_pointer, FfiConverterStringINSTANCE.Lower(vtxoId), FfiConverterBoolINSTANCE.Lower(includeHistory), FfiConverterBoolINSTANCE.Lower(includeTransactions)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) GetExitVtxos() ([]ExitVtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []ExitVtxo {
			return FfiConverterSequenceExitVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_get_exit_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) GetExpiringVtxos(thresholdBlocks uint32) ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_get_expiring_vtxos(
			_pointer, FfiConverterUint32INSTANCE.Lower(thresholdBlocks)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) GetFirstExpiringVtxoBlockheight() (*uint32, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *uint32 {
			return FfiConverterOptionalUint32INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_get_first_expiring_vtxo_blockheight(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) GetNextRequiredRefreshBlockheight() (*uint32, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *uint32 {
			return FfiConverterOptionalUint32INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_get_next_required_refresh_blockheight(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) GetVtxoById(vtxoId string) (Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) Vtxo {
			return FfiConverterVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_get_vtxo_by_id(
			_pointer, FfiConverterStringINSTANCE.Lower(vtxoId)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) GetVtxosToRefresh() ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_get_vtxos_to_refresh(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) HasPendingExits() (bool, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.int8_t {
			res := C.ffi_bark_ffi_rust_future_complete_i8(handle, status)
			return res
		},
		// liftFn
		func(ffi C.int8_t) bool {
			return FfiConverterBoolINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_has_pending_exits(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_i8(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_i8(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) History() ([]Movement, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Movement {
			return FfiConverterSequenceMovementINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_history(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) HistoryByPaymentMethod(paymentMethodType string, paymentMethodValue string) ([]Movement, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Movement {
			return FfiConverterSequenceMovementINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_history_by_payment_method(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentMethodType), FfiConverterStringINSTANCE.Lower(paymentMethodValue)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Import a VTXO from its serialized form (hex or base64).
//
// The VTXO is stored in the state the server reports for it, so one that
// was already spent is recorded as spent rather than refused. Pass `args`
// to widen the key-scan gap limit, skip the server status check, or allow
// partial success; omit it for the defaults.
//
// The first parameter keeps its historical `vtxo_base64` name for foreign
// binding compatibility (uniffi exposes parameter names), but hex as
// returned by [`Wallet::vtxo_encoded`] is accepted too.
func (_self *Wallet) ImportVtxo(vtxoBase64 string, args *ImportVtxoArgs) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_import_vtxo(
			_pointer, FfiConverterStringINSTANCE.Lower(vtxoBase64), FfiConverterOptionalImportVtxoArgsINSTANCE.Lower(args)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Import several VTXOs (hex or base64) under a single key scan and a
// single write, which is why this is not just a loop over `import_vtxo`.
//
// Returns the ids now held — whether this call stored them or found them
// already present — so a failed batch can be retried. One VTXO that cannot
// be imported discards the whole batch unless `args.allow_partial` is set,
// in which case the ones that did import are kept.
func (_self *Wallet) ImportVtxos(encodedVtxos []string, args *ImportVtxoArgs) ([]string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []string {
			return FfiConverterSequenceStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_import_vtxos(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(encodedVtxos), FfiConverterOptionalImportVtxoArgsINSTANCE.Lower(args)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Cheap "has this invoice ever been paid?", answered from the local fact
// table without consulting the server.
func (_self *Wallet) IsInvoicePaid(paymentHash string) (bool, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.int8_t {
			res := C.ffi_bark_ffi_rust_future_complete_i8(handle, status)
			return res
		},
		// liftFn
		func(ffi C.int8_t) bool {
			return FfiConverterBoolINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_is_invoice_paid(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_i8(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_i8(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Triage a payment hash: settled or in-progress. Errors if no lightning
// receive is known for this payment hash.
func (_self *Wallet) LightningReceiveState(paymentHash string) (LightningReceive, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningReceive {
			return FfiConverterLightningReceiveINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_lightning_receive_state(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Read-only triage, for polling after a send started with `wait = false`.
// Unlike `checkLightningPayment`, never advances the action.
func (_self *Wallet) LightningSendState(paymentHash string) (LightningSendStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningSendStatus {
			return FfiConverterLightningSendStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_lightning_send_state(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) ListClaimableExits() ([]ExitVtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []ExitVtxo {
			return FfiConverterSequenceExitVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_list_claimable_exits(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Reserve VTXOs so wallet-driven flows leave them alone: a locked VTXO is
// excluded from coin selection.
//
// `holder` is what `unlockVtxos` matches on to stop one subsystem
// releasing another's lock. Atomic, and re-locking with the same holder
// is a no-op.
func (_self *Wallet) LockVtxos(vtxoIds []string, holder *VtxoLockHolder) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_lock_vtxos(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds), FfiConverterOptionalVtxoLockHolderINSTANCE.Lower(holder)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Create a hex-encoded authorization that lets whoever holds it read this
// wallet's mailbox from the Ark server for `expiry_secs` from now, so 86400
// for 24 hours. An authorization cannot be revoked early, so keep the
// window short.
func (_self *Wallet) MailboxAuthorization(expirySecs uint32) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_wallet_mailbox_authorization(
				_pointer, FfiConverterUint32INSTANCE.Lower(expirySecs), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) MailboxIdentifier() (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_wallet_mailbox_identifier(
				_pointer, _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) Maintenance() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_maintenance(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) MaintenanceDelegated() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_maintenance_delegated(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) MaintenanceRefresh() (*string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *string {
			return FfiConverterOptionalStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_maintenance_refresh(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) Network() (Network, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) Network {
			return FfiConverterNetworkINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_network(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) NewAddress() (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_new_address(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) NewAddressWithIndex() (AddressWithIndex, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) AddressWithIndex {
			return FfiConverterAddressWithIndexINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_new_address_with_index(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) NextRoundStartTime() (uint64, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
			res := C.ffi_bark_ffi_rust_future_complete_u64(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint64_t) uint64 {
			return FfiConverterUint64INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_next_round_start_time(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u64(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u64(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) Notifications() *NotificationHolder {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterNotificationHolderINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint64_t {
		return C.uniffi_bark_ffi_fn_method_wallet_notifications(
			_pointer, _uniffiStatus)
	}))
}

func (_self *Wallet) OffboardAll(bitcoinAddress string) (OffboardResult, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) OffboardResult {
			return FfiConverterOffboardResultINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_offboard_all(
			_pointer, FfiConverterStringINSTANCE.Lower(bitcoinAddress)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) OffboardVtxos(vtxoIds []string, bitcoinAddress string) (OffboardResult, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) OffboardResult {
			return FfiConverterOffboardResultINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_offboard_vtxos(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds), FfiConverterStringINSTANCE.Lower(bitcoinAddress)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Pay to a Lightning Address (`user@domain`), resolved via LNURL-pay.
func (_self *Wallet) PayLightningAddress(lightningAddress string, amountSats uint64, comment *string, wait bool) (LightningSendStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningSendStatus {
			return FfiConverterLightningSendStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pay_lightning_address(
			_pointer, FfiConverterStringINSTANCE.Lower(lightningAddress), FfiConverterUint64INSTANCE.Lower(amountSats), FfiConverterOptionalStringINSTANCE.Lower(comment), FfiConverterBoolINSTANCE.Lower(wait)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PayLightningInvoice(invoice string, amountSats *uint64, wait bool) (LightningSendStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningSendStatus {
			return FfiConverterLightningSendStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pay_lightning_invoice(
			_pointer, FfiConverterStringINSTANCE.Lower(invoice), FfiConverterOptionalUint64INSTANCE.Lower(amountSats), FfiConverterBoolINSTANCE.Lower(wait)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PayLightningOffer(offer string, amountSats *uint64, wait bool) (LightningSendStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningSendStatus {
			return FfiConverterLightningSendStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pay_lightning_offer(
			_pointer, FfiConverterStringINSTANCE.Lower(offer), FfiConverterOptionalUint64INSTANCE.Lower(amountSats), FfiConverterBoolINSTANCE.Lower(wait)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Pay a raw LNURL-pay link (`lnurl1…`).
//
// Resolves the LNURL-pay endpoint to a BOLT11 invoice and pays it. Errors
// if the link decodes to a non-pay LNURL (auth, withdraw, channel).
func (_self *Wallet) PayLnurl(lnurl string, amountSats uint64, comment *string, wait bool) (LightningSendStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningSendStatus {
			return FfiConverterLightningSendStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pay_lnurl(
			_pointer, FfiConverterStringINSTANCE.Lower(lnurl), FfiConverterUint64INSTANCE.Lower(amountSats), FfiConverterOptionalStringINSTANCE.Lower(comment), FfiConverterBoolINSTANCE.Lower(wait)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PeekAddress(index uint32) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_peek_address(
			_pointer, FfiConverterUint32INSTANCE.Lower(index)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingBoardVtxos() ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_board_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingBoards() ([]PendingBoard, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []PendingBoard {
			return FfiConverterSequencePendingBoardINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_boards(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingExitsTotalSats() (uint64, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
			res := C.ffi_bark_ffi_rust_future_complete_u64(handle, status)
			return res
		},
		// liftFn
		func(ffi C.uint64_t) uint64 {
			return FfiConverterUint64INSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_exits_total_sats(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_u64(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_u64(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingLightningReceives() ([]LightningReceive, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []LightningReceive {
			return FfiConverterSequenceLightningReceiveINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_lightning_receives(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingLightningSendVtxos() ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_lightning_send_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingLightningSends() ([]LightningSend, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []LightningSend {
			return FfiConverterSequenceLightningSendINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_lightning_sends(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingRoundInputVtxos() ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_round_input_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) PendingRoundStates() ([]RoundState, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []RoundState {
			return FfiConverterSequenceRoundStateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_pending_round_states(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) ProgressExits(feeRateSatPerVb *uint64) ([]ExitProgressStatus, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []ExitProgressStatus {
			return FfiConverterSequenceExitProgressStatusINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_progress_exits(
			_pointer, FfiConverterOptionalUint64INSTANCE.Lower(feeRateSatPerVb)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) ProgressPendingRounds() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_progress_pending_rounds(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) Properties() (WalletProperties, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) WalletProperties {
			return FfiConverterWalletPropertiesINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_properties(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Recover the given VTXO ids from the server, importing the ones this
// wallet owns that are still spendable. Use it to retry ids a previous
// scan reported as `failed`.
//
// `gap_limit` overrides `Config.vtxo_key_gap_limit` for the key scan that
// decides which of `vtxo_ids` this wallet owns. Widen it to reach ids a
// previous scan bucketed as `foreign`.
func (_self *Wallet) RecoverVtxos(vtxoIds []string, gapLimit *uint32) (RecoveryReport, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) RecoveryReport {
			return FfiConverterRecoveryReportINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_recover_vtxos(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds), FfiConverterOptionalUint32INSTANCE.Lower(gapLimit)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// The report of the seed-recovery scan that ran during `Wallet::open`, or
// none if the scan did not complete. Use `recovery_status` to tell a scan
// that failed apart from one that never ran.
func (_self *Wallet) RecoveryReport() *RecoveryReport {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterOptionalRecoveryReportINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_wallet_recovery_report(
				_pointer, _uniffiStatus),
		}
	}))
}

// Outcome of the seed-recovery scan that ran during `Wallet::open`.
//
// Recovery only runs on the open that creates the wallet locally, and not
// at all when `WalletOpenArgs.skip_recovery` is set, so this is `NotRun`
// on every subsequent open. `Failed` means the scan errored before
// producing a report — bark logs that and lets open succeed — so funds
// may be missing until a retry; `Completed` carries the report, and
// `isComplete == false` there means funds may still be missing. Retry the
// report's `failed` ids with `recoverVtxos`.
func (_self *Wallet) RecoveryStatus() RecoveryStatus {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterRecoveryStatusINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_method_wallet_recovery_status(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Wallet) RefreshServer() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_refresh_server(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) RefreshVtxos(vtxoIds []string) (*string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *string {
			return FfiConverterOptionalStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_refresh_vtxos(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) RefreshVtxosDelegated(vtxoIds []string) (*RoundState, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *RoundState {
			return FfiConverterOptionalRoundStateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_refresh_vtxos_delegated(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Schedule a delegated refresh for `scheduled_height` instead of the next
// round. The refresh fee is priced against the VTXO's remaining lifetime at
// that height, and the server charges less the closer a VTXO is to expiry,
// so scheduling further out never costs more than refreshing now.
func (_self *Wallet) RefreshVtxosScheduled(vtxoIds []string, scheduledHeight uint32) (*RoundState, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) *RoundState {
			return FfiConverterOptionalRoundStateINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_refresh_vtxos_scheduled(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds), FfiConverterUint32INSTANCE.Lower(scheduledHeight)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Start the background daemon. The onchain wallet used by the daemon is
// the one supplied to `Wallet::open` (see `WalletOpenArgs.onchain`).
func (_self *Wallet) RunDaemon() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_run_daemon(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Send an out-of-round payment to an Ark address.
//
// An address this bark cannot deliver to (see `validateArkoorAddress`) is
// rejected up front, leaving the VTXOs spendable rather than cosigned.
func (_self *Wallet) SendArkoorPayment(arkAddress string, amountSats uint64) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_send_arkoor_payment(
			_pointer, FfiConverterStringINSTANCE.Lower(arkAddress), FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) SendOnchain(address string, amountSats uint64) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_send_onchain(
			_pointer, FfiConverterStringINSTANCE.Lower(address), FfiConverterUint64INSTANCE.Lower(amountSats)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) SignExitClaimInputs(psbtBase64 string) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_sign_exit_claim_inputs(
			_pointer, FfiConverterStringINSTANCE.Lower(psbtBase64)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) SpendableVtxos() ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_spendable_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) StartExitForEntireWallet() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_start_exit_for_entire_wallet(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) StartExitForVtxos(vtxoIds []string) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_start_exit_for_vtxos(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Like `startExitForVtxos`, but skips dust and standardness checks.
//
// Only for VTXOs already onchain, or a node that accepts non-standard
// transactions; otherwise the exit transactions may be unrelayable.
func (_self *Wallet) StartExitForVtxosIncludingNonStandard(vtxoIds []string) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_start_exit_for_vtxos_including_non_standard(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) StopDaemon() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_stop_daemon(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Stop the background daemon and wait until its tasks have finished, so
// nothing runs in the background afterwards (e.g. before deleting the
// wallet's datadir). No-op when no daemon is running.
func (_self *Wallet) StopDaemonWait() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_stop_daemon_wait(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Failed lightning sends whose HTLC revocation also failed.
func (_self *Wallet) StuckFailedLightningSends() ([]LightningSend, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []LightningSend {
			return FfiConverterSequenceLightningSendINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_stuck_failed_lightning_sends(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) Sync() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_sync(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) SyncExits() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_sync_exits(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Scan for VTXOs that were force-exited on-chain without the user asking
// and route them into the unilateral-exit flow so the funds can be claimed.
//
// This already runs automatically as part of [`Self::sync`]; call it
// directly to trigger the scan on demand.
func (_self *Wallet) SyncForceExitedVtxos() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_sync_force_exited_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) SyncPendingBoards() error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_sync_pending_boards(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

func (_self *Wallet) TryClaimAllLightningReceives(wait bool) ([]LightningReceive, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []LightningReceive {
			return FfiConverterSequenceLightningReceiveINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_try_claim_all_lightning_receives(
			_pointer, FfiConverterBoolINSTANCE.Lower(wait)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) TryClaimLightningReceive(paymentHash string, wait bool) (LightningReceive, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) LightningReceive {
			return FfiConverterLightningReceiveINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_try_claim_lightning_receive(
			_pointer, FfiConverterStringINSTANCE.Lower(paymentHash), FfiConverterBoolINSTANCE.Lower(wait)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Release VTXOs locked by `lockVtxos`.
//
// Every VTXO must currently be held by `expected_holder` or nothing is
// unlocked; that is what stops cleanup freeing a VTXO a payment or
// in-flight round has since claimed. Leaving it unset bypasses the guard
// entirely, so reserve that for recovery.
func (_self *Wallet) UnlockVtxos(vtxoIds []string, expectedHolder *VtxoLockHolder) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_method_wallet_unlock_vtxos(
			_pointer, FfiConverterSequenceStringINSTANCE.Lower(vtxoIds), FfiConverterOptionalVtxoLockHolderINSTANCE.Lower(expectedHolder)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}

// Whether this wallet can pay `address` out-of-round: same network and
// server, a supported VTXO policy, and only delivery mechanisms this bark
// supports. An address listing no delivery mechanism is valid. Errors only
// when the address does not parse.
func (_self *Wallet) ValidateArkoorAddress(address string) (bool, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) C.int8_t {
			res := C.ffi_bark_ffi_rust_future_complete_i8(handle, status)
			return res
		},
		// liftFn
		func(ffi C.int8_t) bool {
			return FfiConverterBoolINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_validate_arkoor_address(
			_pointer, FfiConverterStringINSTANCE.Lower(address)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_i8(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_i8(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

// Hex-encoded serialization of the full VTXO (genesis chain included),
// re-importable via [`Wallet::import_vtxo`]. Mirrors bark-rest
// `GET /vtxos/{id}/encoded`.
func (_self *Wallet) VtxoEncoded(vtxoId string) (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) string {
			return FfiConverterStringINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_vtxo_encoded(
			_pointer, FfiConverterStringINSTANCE.Lower(vtxoId)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}

func (_self *Wallet) Vtxos() ([]Vtxo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	res, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) RustBufferI {
			res := C.ffi_bark_ffi_rust_future_complete_rust_buffer(handle, status)
			return GoRustBuffer{
				inner: res,
			}
		},
		// liftFn
		func(ffi RustBufferI) []Vtxo {
			return FfiConverterSequenceVtxoINSTANCE.Lift(ffi)
		},
		C.uniffi_bark_ffi_fn_method_wallet_vtxos(
			_pointer),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_rust_buffer(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_rust_buffer(handle)
		},
	)

	if err == nil {
		return res, nil
	}

	return res, err
}
func (object *Wallet) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterWallet struct{}

var FfiConverterWalletINSTANCE = FfiConverterWallet{}

func (c FfiConverterWallet) Lift(handle C.uint64_t) *Wallet {
	result := &Wallet{
		newFfiObject(
			handle,
			func(handle C.uint64_t, status *C.RustCallStatus) C.uint64_t {
				return C.uniffi_bark_ffi_fn_clone_wallet(handle, status)
			},
			func(handle C.uint64_t, status *C.RustCallStatus) {
				C.uniffi_bark_ffi_fn_free_wallet(handle, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*Wallet).Destroy)
	return result
}

func (c FfiConverterWallet) Read(reader io.Reader) *Wallet {
	return c.Lift(C.uint64_t(readUint64(reader)))
}

func (c FfiConverterWallet) Lower(value *Wallet) C.uint64_t {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the handle will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked handle.
	handle := value.ffiObject.incrementPointer("*Wallet")
	defer value.ffiObject.decrementPointer()
	return handle
}

func (c FfiConverterWallet) Write(writer io.Writer, value *Wallet) {
	writeUint64(writer, uint64(c.Lower(value)))
}

func LiftFromExternalWallet(handle uint64) *Wallet {
	return FfiConverterWalletINSTANCE.Lift(C.uint64_t(handle))
}

func LowerToExternalWallet(value *Wallet) uint64 {
	return uint64(FfiConverterWalletINSTANCE.Lower(value))
}

type FfiDestroyerWallet struct{}

func (_ FfiDestroyerWallet) Destroy(value *Wallet) {
	value.Destroy()
}

type AddressWithIndex struct {
	Address string
	Index   uint32
}

func (r *AddressWithIndex) Destroy() {
	FfiDestroyerString{}.Destroy(r.Address)
	FfiDestroyerUint32{}.Destroy(r.Index)
}

type FfiConverterAddressWithIndex struct{}

var FfiConverterAddressWithIndexINSTANCE = FfiConverterAddressWithIndex{}

func (c FfiConverterAddressWithIndex) Lift(rb RustBufferI) AddressWithIndex {
	return LiftFromRustBuffer[AddressWithIndex](c, rb)
}

func (c FfiConverterAddressWithIndex) Read(reader io.Reader) AddressWithIndex {
	return AddressWithIndex{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterAddressWithIndex) Lower(value AddressWithIndex) C.RustBuffer {
	return LowerIntoRustBuffer[AddressWithIndex](c, value)
}

func (c FfiConverterAddressWithIndex) LowerExternal(value AddressWithIndex) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[AddressWithIndex](c, value))
}

func (c FfiConverterAddressWithIndex) Write(writer io.Writer, value AddressWithIndex) {
	FfiConverterStringINSTANCE.Write(writer, value.Address)
	FfiConverterUint32INSTANCE.Write(writer, value.Index)
}

type FfiDestroyerAddressWithIndex struct{}

func (_ FfiDestroyerAddressWithIndex) Destroy(value AddressWithIndex) {
	value.Destroy()
}

type ArkInfo struct {
	Network           Network
	ServerPubkey      string
	RoundIntervalSecs uint64
	NbRoundNonces     uint32
	VtxoExitDelta     uint32
	// The number of blocks a VTXO lives before it expires.
	VtxoLifetime uint32
	// The number of blocks a VTXO lives before it expires.
	//
	// Deprecated: upstream renamed this to `vtxo_lifetime`. Both names are
	// exposed and carry the same value; migrate to `vtxo_lifetime`, this one
	// goes away once upstream drops it.
	VtxoExpiryDelta            uint32
	HtlcSendExpiryDelta        uint32
	HtlcExpiryDelta            uint32
	MaxVtxoAmountSats          *uint64
	RequiredBoardConfirmations uint32
	MaxUserInvoiceCltvDelta    uint16
	MinBoardAmountSats         uint64
	LnReceiveAntiDosRequired   bool
	// The server's fee schedule (board, offboard, refresh, lightning fees).
	FeeSchedule FeeSchedule
	// Maximum exit depth (genesis chain length) allowed for a VTXO before the
	// server refuses to cosign further OOR transactions spending it.
	MaxVtxoExitDepth uint16
}

func (r *ArkInfo) Destroy() {
	FfiDestroyerNetwork{}.Destroy(r.Network)
	FfiDestroyerString{}.Destroy(r.ServerPubkey)
	FfiDestroyerUint64{}.Destroy(r.RoundIntervalSecs)
	FfiDestroyerUint32{}.Destroy(r.NbRoundNonces)
	FfiDestroyerUint32{}.Destroy(r.VtxoExitDelta)
	FfiDestroyerUint32{}.Destroy(r.VtxoLifetime)
	FfiDestroyerUint32{}.Destroy(r.VtxoExpiryDelta)
	FfiDestroyerUint32{}.Destroy(r.HtlcSendExpiryDelta)
	FfiDestroyerUint32{}.Destroy(r.HtlcExpiryDelta)
	FfiDestroyerOptionalUint64{}.Destroy(r.MaxVtxoAmountSats)
	FfiDestroyerUint32{}.Destroy(r.RequiredBoardConfirmations)
	FfiDestroyerUint16{}.Destroy(r.MaxUserInvoiceCltvDelta)
	FfiDestroyerUint64{}.Destroy(r.MinBoardAmountSats)
	FfiDestroyerBool{}.Destroy(r.LnReceiveAntiDosRequired)
	FfiDestroyerFeeSchedule{}.Destroy(r.FeeSchedule)
	FfiDestroyerUint16{}.Destroy(r.MaxVtxoExitDepth)
}

type FfiConverterArkInfo struct{}

var FfiConverterArkInfoINSTANCE = FfiConverterArkInfo{}

func (c FfiConverterArkInfo) Lift(rb RustBufferI) ArkInfo {
	return LiftFromRustBuffer[ArkInfo](c, rb)
}

func (c FfiConverterArkInfo) Read(reader io.Reader) ArkInfo {
	return ArkInfo{
		FfiConverterNetworkINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterOptionalUint64INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint16INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterFeeScheduleINSTANCE.Read(reader),
		FfiConverterUint16INSTANCE.Read(reader),
	}
}

func (c FfiConverterArkInfo) Lower(value ArkInfo) C.RustBuffer {
	return LowerIntoRustBuffer[ArkInfo](c, value)
}

func (c FfiConverterArkInfo) LowerExternal(value ArkInfo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ArkInfo](c, value))
}

func (c FfiConverterArkInfo) Write(writer io.Writer, value ArkInfo) {
	FfiConverterNetworkINSTANCE.Write(writer, value.Network)
	FfiConverterStringINSTANCE.Write(writer, value.ServerPubkey)
	FfiConverterUint64INSTANCE.Write(writer, value.RoundIntervalSecs)
	FfiConverterUint32INSTANCE.Write(writer, value.NbRoundNonces)
	FfiConverterUint32INSTANCE.Write(writer, value.VtxoExitDelta)
	FfiConverterUint32INSTANCE.Write(writer, value.VtxoLifetime)
	FfiConverterUint32INSTANCE.Write(writer, value.VtxoExpiryDelta)
	FfiConverterUint32INSTANCE.Write(writer, value.HtlcSendExpiryDelta)
	FfiConverterUint32INSTANCE.Write(writer, value.HtlcExpiryDelta)
	FfiConverterOptionalUint64INSTANCE.Write(writer, value.MaxVtxoAmountSats)
	FfiConverterUint32INSTANCE.Write(writer, value.RequiredBoardConfirmations)
	FfiConverterUint16INSTANCE.Write(writer, value.MaxUserInvoiceCltvDelta)
	FfiConverterUint64INSTANCE.Write(writer, value.MinBoardAmountSats)
	FfiConverterBoolINSTANCE.Write(writer, value.LnReceiveAntiDosRequired)
	FfiConverterFeeScheduleINSTANCE.Write(writer, value.FeeSchedule)
	FfiConverterUint16INSTANCE.Write(writer, value.MaxVtxoExitDepth)
}

type FfiDestroyerArkInfo struct{}

func (_ FfiDestroyerArkInfo) Destroy(value ArkInfo) {
	value.Destroy()
}

type Balance struct {
	SpendableSats                 uint64
	PendingInRoundSats            uint64
	PendingExitSats               uint64
	PendingLightningSendSats      uint64
	ClaimableLightningReceiveSats uint64
	PendingBoardSats              uint64
}

func (r *Balance) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.SpendableSats)
	FfiDestroyerUint64{}.Destroy(r.PendingInRoundSats)
	FfiDestroyerUint64{}.Destroy(r.PendingExitSats)
	FfiDestroyerUint64{}.Destroy(r.PendingLightningSendSats)
	FfiDestroyerUint64{}.Destroy(r.ClaimableLightningReceiveSats)
	FfiDestroyerUint64{}.Destroy(r.PendingBoardSats)
}

type FfiConverterBalance struct{}

var FfiConverterBalanceINSTANCE = FfiConverterBalance{}

func (c FfiConverterBalance) Lift(rb RustBufferI) Balance {
	return LiftFromRustBuffer[Balance](c, rb)
}

func (c FfiConverterBalance) Read(reader io.Reader) Balance {
	return Balance{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterBalance) Lower(value Balance) C.RustBuffer {
	return LowerIntoRustBuffer[Balance](c, value)
}

func (c FfiConverterBalance) LowerExternal(value Balance) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Balance](c, value))
}

func (c FfiConverterBalance) Write(writer io.Writer, value Balance) {
	FfiConverterUint64INSTANCE.Write(writer, value.SpendableSats)
	FfiConverterUint64INSTANCE.Write(writer, value.PendingInRoundSats)
	FfiConverterUint64INSTANCE.Write(writer, value.PendingExitSats)
	FfiConverterUint64INSTANCE.Write(writer, value.PendingLightningSendSats)
	FfiConverterUint64INSTANCE.Write(writer, value.ClaimableLightningReceiveSats)
	FfiConverterUint64INSTANCE.Write(writer, value.PendingBoardSats)
}

type FfiDestroyerBalance struct{}

func (_ FfiDestroyerBalance) Destroy(value Balance) {
	value.Destroy()
}

// Reference to a block in the blockchain
type BlockRef struct {
	Height uint32
	Hash   string
}

func (r *BlockRef) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Height)
	FfiDestroyerString{}.Destroy(r.Hash)
}

type FfiConverterBlockRef struct{}

var FfiConverterBlockRefINSTANCE = FfiConverterBlockRef{}

func (c FfiConverterBlockRef) Lift(rb RustBufferI) BlockRef {
	return LiftFromRustBuffer[BlockRef](c, rb)
}

func (c FfiConverterBlockRef) Read(reader io.Reader) BlockRef {
	return BlockRef{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterBlockRef) Lower(value BlockRef) C.RustBuffer {
	return LowerIntoRustBuffer[BlockRef](c, value)
}

func (c FfiConverterBlockRef) LowerExternal(value BlockRef) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[BlockRef](c, value))
}

func (c FfiConverterBlockRef) Write(writer io.Writer, value BlockRef) {
	FfiConverterUint32INSTANCE.Write(writer, value.Height)
	FfiConverterStringINSTANCE.Write(writer, value.Hash)
}

type FfiDestroyerBlockRef struct{}

func (_ FfiDestroyerBlockRef) Destroy(value BlockRef) {
	value.Destroy()
}

// Fees for boarding onchain funds into the Ark.
type BoardFees struct {
	MinFeeSats  uint64
	BaseFeeSats uint64
	// Parts-per-million fee rate on the boarded amount.
	Ppm uint64
}

func (r *BoardFees) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.MinFeeSats)
	FfiDestroyerUint64{}.Destroy(r.BaseFeeSats)
	FfiDestroyerUint64{}.Destroy(r.Ppm)
}

type FfiConverterBoardFees struct{}

var FfiConverterBoardFeesINSTANCE = FfiConverterBoardFees{}

func (c FfiConverterBoardFees) Lift(rb RustBufferI) BoardFees {
	return LiftFromRustBuffer[BoardFees](c, rb)
}

func (c FfiConverterBoardFees) Read(reader io.Reader) BoardFees {
	return BoardFees{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterBoardFees) Lower(value BoardFees) C.RustBuffer {
	return LowerIntoRustBuffer[BoardFees](c, value)
}

func (c FfiConverterBoardFees) LowerExternal(value BoardFees) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[BoardFees](c, value))
}

func (c FfiConverterBoardFees) Write(writer io.Writer, value BoardFees) {
	FfiConverterUint64INSTANCE.Write(writer, value.MinFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.BaseFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.Ppm)
}

type FfiDestroyerBoardFees struct{}

func (_ FfiDestroyerBoardFees) Destroy(value BoardFees) {
	value.Destroy()
}

// Destination for an externally funded board (e.g. a payjoin receive).
//
// The funding script commits to the derived keypair and expiry height, so
// `keypair_index` and `expiry_height` must be passed back unchanged to
// `board_psbt` once the funding PSBT is available — including on retries
// after the sender changes the transaction.
type BoardFundingInfo struct {
	Address      string
	ExpiryHeight uint32
	KeypairIndex uint32
}

func (r *BoardFundingInfo) Destroy() {
	FfiDestroyerString{}.Destroy(r.Address)
	FfiDestroyerUint32{}.Destroy(r.ExpiryHeight)
	FfiDestroyerUint32{}.Destroy(r.KeypairIndex)
}

type FfiConverterBoardFundingInfo struct{}

var FfiConverterBoardFundingInfoINSTANCE = FfiConverterBoardFundingInfo{}

func (c FfiConverterBoardFundingInfo) Lift(rb RustBufferI) BoardFundingInfo {
	return LiftFromRustBuffer[BoardFundingInfo](c, rb)
}

func (c FfiConverterBoardFundingInfo) Read(reader io.Reader) BoardFundingInfo {
	return BoardFundingInfo{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterBoardFundingInfo) Lower(value BoardFundingInfo) C.RustBuffer {
	return LowerIntoRustBuffer[BoardFundingInfo](c, value)
}

func (c FfiConverterBoardFundingInfo) LowerExternal(value BoardFundingInfo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[BoardFundingInfo](c, value))
}

func (c FfiConverterBoardFundingInfo) Write(writer io.Writer, value BoardFundingInfo) {
	FfiConverterStringINSTANCE.Write(writer, value.Address)
	FfiConverterUint32INSTANCE.Write(writer, value.ExpiryHeight)
	FfiConverterUint32INSTANCE.Write(writer, value.KeypairIndex)
}

type FfiDestroyerBoardFundingInfo struct{}

func (_ FfiDestroyerBoardFundingInfo) Destroy(value BoardFundingInfo) {
	value.Destroy()
}

type Config struct {
	ServerAddress                 string
	ServerAccessToken             *string
	EsploraAddress                *string
	BitcoindAddress               *string
	BitcoindCookiefile            *string
	BitcoindUser                  *string
	BitcoindPass                  *string
	VtxoRefreshExpiryThreshold    *uint16
	VtxoExitMargin                *uint16
	HtlcRecvClaimDelta            *uint16
	FallbackFeeRate               *uint64
	RoundTxRequiredConfirmations  *uint32
	DaemonSyncIntervalSecs        *uint64
	OffboardRequiredConfirmations *uint32
	DaemonManualSync              *bool
	LightningReceiveClaimRetries  *uint8
	UserAgent                     *string
	// How many consecutive unused seed-derived VTXO key indices a scan
	// crosses before concluding a VTXO isn't ours.
	//
	// Used by `Wallet::recover_vtxos` and `Wallet::import_vtxo(s)`, and by
	// the seed-recovery scan that runs at wallet creation. Every match
	// extends the window, so this bounds the run of unused indices, not the
	// total keys derived. Raise it for a wallet that handed out many
	// addresses without receiving into them.
	//
	// Default: 250. Capped at 100_000; a higher value is rejected when the
	// wallet is opened or created.
	VtxoKeyGapLimit *uint32
}

func (r *Config) Destroy() {
	FfiDestroyerString{}.Destroy(r.ServerAddress)
	FfiDestroyerOptionalString{}.Destroy(r.ServerAccessToken)
	FfiDestroyerOptionalString{}.Destroy(r.EsploraAddress)
	FfiDestroyerOptionalString{}.Destroy(r.BitcoindAddress)
	FfiDestroyerOptionalString{}.Destroy(r.BitcoindCookiefile)
	FfiDestroyerOptionalString{}.Destroy(r.BitcoindUser)
	FfiDestroyerOptionalString{}.Destroy(r.BitcoindPass)
	FfiDestroyerOptionalUint16{}.Destroy(r.VtxoRefreshExpiryThreshold)
	FfiDestroyerOptionalUint16{}.Destroy(r.VtxoExitMargin)
	FfiDestroyerOptionalUint16{}.Destroy(r.HtlcRecvClaimDelta)
	FfiDestroyerOptionalUint64{}.Destroy(r.FallbackFeeRate)
	FfiDestroyerOptionalUint32{}.Destroy(r.RoundTxRequiredConfirmations)
	FfiDestroyerOptionalUint64{}.Destroy(r.DaemonSyncIntervalSecs)
	FfiDestroyerOptionalUint32{}.Destroy(r.OffboardRequiredConfirmations)
	FfiDestroyerOptionalBool{}.Destroy(r.DaemonManualSync)
	FfiDestroyerOptionalUint8{}.Destroy(r.LightningReceiveClaimRetries)
	FfiDestroyerOptionalString{}.Destroy(r.UserAgent)
	FfiDestroyerOptionalUint32{}.Destroy(r.VtxoKeyGapLimit)
}

type FfiConverterConfig struct{}

var FfiConverterConfigINSTANCE = FfiConverterConfig{}

func (c FfiConverterConfig) Lift(rb RustBufferI) Config {
	return LiftFromRustBuffer[Config](c, rb)
}

func (c FfiConverterConfig) Read(reader io.Reader) Config {
	return Config{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalUint16INSTANCE.Read(reader),
		FfiConverterOptionalUint16INSTANCE.Read(reader),
		FfiConverterOptionalUint16INSTANCE.Read(reader),
		FfiConverterOptionalUint64INSTANCE.Read(reader),
		FfiConverterOptionalUint32INSTANCE.Read(reader),
		FfiConverterOptionalUint64INSTANCE.Read(reader),
		FfiConverterOptionalUint32INSTANCE.Read(reader),
		FfiConverterOptionalBoolINSTANCE.Read(reader),
		FfiConverterOptionalUint8INSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterConfig) Lower(value Config) C.RustBuffer {
	return LowerIntoRustBuffer[Config](c, value)
}

func (c FfiConverterConfig) LowerExternal(value Config) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Config](c, value))
}

func (c FfiConverterConfig) Write(writer io.Writer, value Config) {
	FfiConverterStringINSTANCE.Write(writer, value.ServerAddress)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.ServerAccessToken)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.EsploraAddress)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.BitcoindAddress)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.BitcoindCookiefile)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.BitcoindUser)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.BitcoindPass)
	FfiConverterOptionalUint16INSTANCE.Write(writer, value.VtxoRefreshExpiryThreshold)
	FfiConverterOptionalUint16INSTANCE.Write(writer, value.VtxoExitMargin)
	FfiConverterOptionalUint16INSTANCE.Write(writer, value.HtlcRecvClaimDelta)
	FfiConverterOptionalUint64INSTANCE.Write(writer, value.FallbackFeeRate)
	FfiConverterOptionalUint32INSTANCE.Write(writer, value.RoundTxRequiredConfirmations)
	FfiConverterOptionalUint64INSTANCE.Write(writer, value.DaemonSyncIntervalSecs)
	FfiConverterOptionalUint32INSTANCE.Write(writer, value.OffboardRequiredConfirmations)
	FfiConverterOptionalBoolINSTANCE.Write(writer, value.DaemonManualSync)
	FfiConverterOptionalUint8INSTANCE.Write(writer, value.LightningReceiveClaimRetries)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.UserAgent)
	FfiConverterOptionalUint32INSTANCE.Write(writer, value.VtxoKeyGapLimit)
}

type FfiDestroyerConfig struct{}

func (_ FfiDestroyerConfig) Destroy(value Config) {
	value.Destroy()
}

// Parameters for creating a CPFP (Child Pays For Parent) transaction
type CpfpParams struct {
	TxHex                    string
	FeesType                 string
	EffectiveFeeRateSatPerVb uint64
	CurrentPackageFeeSats    *uint64
}

func (r *CpfpParams) Destroy() {
	FfiDestroyerString{}.Destroy(r.TxHex)
	FfiDestroyerString{}.Destroy(r.FeesType)
	FfiDestroyerUint64{}.Destroy(r.EffectiveFeeRateSatPerVb)
	FfiDestroyerOptionalUint64{}.Destroy(r.CurrentPackageFeeSats)
}

type FfiConverterCpfpParams struct{}

var FfiConverterCpfpParamsINSTANCE = FfiConverterCpfpParams{}

func (c FfiConverterCpfpParams) Lift(rb RustBufferI) CpfpParams {
	return LiftFromRustBuffer[CpfpParams](c, rb)
}

func (c FfiConverterCpfpParams) Read(reader io.Reader) CpfpParams {
	return CpfpParams{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterOptionalUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterCpfpParams) Lower(value CpfpParams) C.RustBuffer {
	return LowerIntoRustBuffer[CpfpParams](c, value)
}

func (c FfiConverterCpfpParams) LowerExternal(value CpfpParams) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[CpfpParams](c, value))
}

func (c FfiConverterCpfpParams) Write(writer io.Writer, value CpfpParams) {
	FfiConverterStringINSTANCE.Write(writer, value.TxHex)
	FfiConverterStringINSTANCE.Write(writer, value.FeesType)
	FfiConverterUint64INSTANCE.Write(writer, value.EffectiveFeeRateSatPerVb)
	FfiConverterOptionalUint64INSTANCE.Write(writer, value.CurrentPackageFeeSats)
}

type FfiDestroyerCpfpParams struct{}

func (_ FfiDestroyerCpfpParams) Destroy(value CpfpParams) {
	value.Destroy()
}

// A Bitcoin transaction output destination
type Destination struct {
	Address    string
	AmountSats uint64
}

func (r *Destination) Destroy() {
	FfiDestroyerString{}.Destroy(r.Address)
	FfiDestroyerUint64{}.Destroy(r.AmountSats)
}

type FfiConverterDestination struct{}

var FfiConverterDestinationINSTANCE = FfiConverterDestination{}

func (c FfiConverterDestination) Lift(rb RustBufferI) Destination {
	return LiftFromRustBuffer[Destination](c, rb)
}

func (c FfiConverterDestination) Read(reader io.Reader) Destination {
	return Destination{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterDestination) Lower(value Destination) C.RustBuffer {
	return LowerIntoRustBuffer[Destination](c, value)
}

func (c FfiConverterDestination) LowerExternal(value Destination) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Destination](c, value))
}

func (c FfiConverterDestination) Write(writer io.Writer, value Destination) {
	FfiConverterStringINSTANCE.Write(writer, value.Address)
	FfiConverterUint64INSTANCE.Write(writer, value.AmountSats)
}

type FfiDestroyerDestination struct{}

func (_ FfiDestroyerDestination) Destroy(value Destination) {
	value.Destroy()
}

// Estimated onchain cost of unilaterally (emergency) exiting a set of VTXOs.
//
// Mirrors bark-rest's `GET /exits/fee` response. The two legs are paid from
// different pockets: `exit_broadcast_fee_sats` is spent now out of confirmed
// onchain funds to CPFP-bump every not-yet-confirmed exit transaction, while
// `claim_fee_sats` is deducted later from the recovered value when the
// matured outputs are drained.
type EmergencyExitFeeEstimate struct {
	// CPFP fees to broadcast every not-yet-confirmed exit transaction. Paid
	// now from confirmed onchain funds; this is the minimum onchain balance
	// an emergency exit needs.
	ExitBroadcastFeeSats uint64
	// Fee of the single batched transaction that later drains the matured
	// exit outputs. Subtracted from the exited amount.
	ClaimFeeSats uint64
	// `exit_broadcast_fee_sats + claim_fee_sats`.
	TotalFeeSats uint64
	// Fee rate the exit-broadcast leg was priced at (sat/vB, rounded up).
	// Unless an explicit rate was supplied, the claim leg is priced
	// separately at the chain's `regular` rate.
	FeeRateSatPerVb uint64
	// Number of exit transactions that still need to be broadcast and
	// CPFP-bumped. Already-confirmed tree transactions are not counted.
	TxsToBroadcast uint64
	// Whether the wallet's current confirmed onchain balance covers the full
	// serial broadcast walk. Each CPFP child can only spend confirmed coins,
	// so `false` means the exit would stall midway even if a single bump
	// looks affordable.
	Fundable bool
}

func (r *EmergencyExitFeeEstimate) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.ExitBroadcastFeeSats)
	FfiDestroyerUint64{}.Destroy(r.ClaimFeeSats)
	FfiDestroyerUint64{}.Destroy(r.TotalFeeSats)
	FfiDestroyerUint64{}.Destroy(r.FeeRateSatPerVb)
	FfiDestroyerUint64{}.Destroy(r.TxsToBroadcast)
	FfiDestroyerBool{}.Destroy(r.Fundable)
}

type FfiConverterEmergencyExitFeeEstimate struct{}

var FfiConverterEmergencyExitFeeEstimateINSTANCE = FfiConverterEmergencyExitFeeEstimate{}

func (c FfiConverterEmergencyExitFeeEstimate) Lift(rb RustBufferI) EmergencyExitFeeEstimate {
	return LiftFromRustBuffer[EmergencyExitFeeEstimate](c, rb)
}

func (c FfiConverterEmergencyExitFeeEstimate) Read(reader io.Reader) EmergencyExitFeeEstimate {
	return EmergencyExitFeeEstimate{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterEmergencyExitFeeEstimate) Lower(value EmergencyExitFeeEstimate) C.RustBuffer {
	return LowerIntoRustBuffer[EmergencyExitFeeEstimate](c, value)
}

func (c FfiConverterEmergencyExitFeeEstimate) LowerExternal(value EmergencyExitFeeEstimate) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[EmergencyExitFeeEstimate](c, value))
}

func (c FfiConverterEmergencyExitFeeEstimate) Write(writer io.Writer, value EmergencyExitFeeEstimate) {
	FfiConverterUint64INSTANCE.Write(writer, value.ExitBroadcastFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.ClaimFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.TotalFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.FeeRateSatPerVb)
	FfiConverterUint64INSTANCE.Write(writer, value.TxsToBroadcast)
	FfiConverterBoolINSTANCE.Write(writer, value.Fundable)
}

type FfiDestroyerEmergencyExitFeeEstimate struct{}

func (_ FfiDestroyerEmergencyExitFeeEstimate) Destroy(value EmergencyExitFeeEstimate) {
	value.Destroy()
}

// Outcome of a cancellation request.
//
// `canceled` is `true` when the exit is now canceled — including when it was
// already canceled by an earlier call, since cancellation is idempotent.
// When `false`, `reason` says why.
type ExitCancelResult struct {
	Canceled bool
	// Present exactly when `canceled` is `false`.
	Reason *ExitCancelFailure
}

func (r *ExitCancelResult) Destroy() {
	FfiDestroyerBool{}.Destroy(r.Canceled)
	FfiDestroyerOptionalExitCancelFailure{}.Destroy(r.Reason)
}

type FfiConverterExitCancelResult struct{}

var FfiConverterExitCancelResultINSTANCE = FfiConverterExitCancelResult{}

func (c FfiConverterExitCancelResult) Lift(rb RustBufferI) ExitCancelResult {
	return LiftFromRustBuffer[ExitCancelResult](c, rb)
}

func (c FfiConverterExitCancelResult) Read(reader io.Reader) ExitCancelResult {
	return ExitCancelResult{
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterOptionalExitCancelFailureINSTANCE.Read(reader),
	}
}

func (c FfiConverterExitCancelResult) Lower(value ExitCancelResult) C.RustBuffer {
	return LowerIntoRustBuffer[ExitCancelResult](c, value)
}

func (c FfiConverterExitCancelResult) LowerExternal(value ExitCancelResult) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitCancelResult](c, value))
}

func (c FfiConverterExitCancelResult) Write(writer io.Writer, value ExitCancelResult) {
	FfiConverterBoolINSTANCE.Write(writer, value.Canceled)
	FfiConverterOptionalExitCancelFailureINSTANCE.Write(writer, value.Reason)
}

type FfiDestroyerExitCancelResult struct{}

func (_ FfiDestroyerExitCancelResult) Destroy(value ExitCancelResult) {
	value.Destroy()
}

// Claim transaction for exited funds
type ExitClaimTransaction struct {
	PsbtBase64 string
	FeeSats    uint64
}

func (r *ExitClaimTransaction) Destroy() {
	FfiDestroyerString{}.Destroy(r.PsbtBase64)
	FfiDestroyerUint64{}.Destroy(r.FeeSats)
}

type FfiConverterExitClaimTransaction struct{}

var FfiConverterExitClaimTransactionINSTANCE = FfiConverterExitClaimTransaction{}

func (c FfiConverterExitClaimTransaction) Lift(rb RustBufferI) ExitClaimTransaction {
	return LiftFromRustBuffer[ExitClaimTransaction](c, rb)
}

func (c FfiConverterExitClaimTransaction) Read(reader io.Reader) ExitClaimTransaction {
	return ExitClaimTransaction{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterExitClaimTransaction) Lower(value ExitClaimTransaction) C.RustBuffer {
	return LowerIntoRustBuffer[ExitClaimTransaction](c, value)
}

func (c FfiConverterExitClaimTransaction) LowerExternal(value ExitClaimTransaction) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitClaimTransaction](c, value))
}

func (c FfiConverterExitClaimTransaction) Write(writer io.Writer, value ExitClaimTransaction) {
	FfiConverterStringINSTANCE.Write(writer, value.PsbtBase64)
	FfiConverterUint64INSTANCE.Write(writer, value.FeeSats)
}

type FfiDestroyerExitClaimTransaction struct{}

func (_ FfiDestroyerExitClaimTransaction) Destroy(value ExitClaimTransaction) {
	value.Destroy()
}

// Status of an exit progression
type ExitProgressStatus struct {
	VtxoId string
	State  ExitState
	Error  *string
}

func (r *ExitProgressStatus) Destroy() {
	FfiDestroyerString{}.Destroy(r.VtxoId)
	FfiDestroyerExitState{}.Destroy(r.State)
	FfiDestroyerOptionalString{}.Destroy(r.Error)
}

type FfiConverterExitProgressStatus struct{}

var FfiConverterExitProgressStatusINSTANCE = FfiConverterExitProgressStatus{}

func (c FfiConverterExitProgressStatus) Lift(rb RustBufferI) ExitProgressStatus {
	return LiftFromRustBuffer[ExitProgressStatus](c, rb)
}

func (c FfiConverterExitProgressStatus) Read(reader io.Reader) ExitProgressStatus {
	return ExitProgressStatus{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterExitStateINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterExitProgressStatus) Lower(value ExitProgressStatus) C.RustBuffer {
	return LowerIntoRustBuffer[ExitProgressStatus](c, value)
}

func (c FfiConverterExitProgressStatus) LowerExternal(value ExitProgressStatus) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitProgressStatus](c, value))
}

func (c FfiConverterExitProgressStatus) Write(writer io.Writer, value ExitProgressStatus) {
	FfiConverterStringINSTANCE.Write(writer, value.VtxoId)
	FfiConverterExitStateINSTANCE.Write(writer, value.State)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.Error)
}

type FfiDestroyerExitProgressStatus struct{}

func (_ FfiDestroyerExitProgressStatus) Destroy(value ExitProgressStatus) {
	value.Destroy()
}

// Detailed status of an exit transaction
type ExitTransactionStatus struct {
	VtxoId           string
	State            ExitState
	History          *[]ExitState
	TransactionCount uint32
}

func (r *ExitTransactionStatus) Destroy() {
	FfiDestroyerString{}.Destroy(r.VtxoId)
	FfiDestroyerExitState{}.Destroy(r.State)
	FfiDestroyerOptionalSequenceExitState{}.Destroy(r.History)
	FfiDestroyerUint32{}.Destroy(r.TransactionCount)
}

type FfiConverterExitTransactionStatus struct{}

var FfiConverterExitTransactionStatusINSTANCE = FfiConverterExitTransactionStatus{}

func (c FfiConverterExitTransactionStatus) Lift(rb RustBufferI) ExitTransactionStatus {
	return LiftFromRustBuffer[ExitTransactionStatus](c, rb)
}

func (c FfiConverterExitTransactionStatus) Read(reader io.Reader) ExitTransactionStatus {
	return ExitTransactionStatus{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterExitStateINSTANCE.Read(reader),
		FfiConverterOptionalSequenceExitStateINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterExitTransactionStatus) Lower(value ExitTransactionStatus) C.RustBuffer {
	return LowerIntoRustBuffer[ExitTransactionStatus](c, value)
}

func (c FfiConverterExitTransactionStatus) LowerExternal(value ExitTransactionStatus) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitTransactionStatus](c, value))
}

func (c FfiConverterExitTransactionStatus) Write(writer io.Writer, value ExitTransactionStatus) {
	FfiConverterStringINSTANCE.Write(writer, value.VtxoId)
	FfiConverterExitStateINSTANCE.Write(writer, value.State)
	FfiConverterOptionalSequenceExitStateINSTANCE.Write(writer, value.History)
	FfiConverterUint32INSTANCE.Write(writer, value.TransactionCount)
}

type FfiDestroyerExitTransactionStatus struct{}

func (_ FfiDestroyerExitTransactionStatus) Destroy(value ExitTransactionStatus) {
	value.Destroy()
}

// One transaction in an exit's unilateral broadcast chain.
type ExitTx struct {
	Txid   string
	Status ExitTxStatus
}

func (r *ExitTx) Destroy() {
	FfiDestroyerString{}.Destroy(r.Txid)
	FfiDestroyerExitTxStatus{}.Destroy(r.Status)
}

type FfiConverterExitTx struct{}

var FfiConverterExitTxINSTANCE = FfiConverterExitTx{}

func (c FfiConverterExitTx) Lift(rb RustBufferI) ExitTx {
	return LiftFromRustBuffer[ExitTx](c, rb)
}

func (c FfiConverterExitTx) Read(reader io.Reader) ExitTx {
	return ExitTx{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterExitTxStatusINSTANCE.Read(reader),
	}
}

func (c FfiConverterExitTx) Lower(value ExitTx) C.RustBuffer {
	return LowerIntoRustBuffer[ExitTx](c, value)
}

func (c FfiConverterExitTx) LowerExternal(value ExitTx) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitTx](c, value))
}

func (c FfiConverterExitTx) Write(writer io.Writer, value ExitTx) {
	FfiConverterStringINSTANCE.Write(writer, value.Txid)
	FfiConverterExitTxStatusINSTANCE.Write(writer, value.Status)
}

type FfiDestroyerExitTx struct{}

func (_ FfiDestroyerExitTx) Destroy(value ExitTx) {
	value.Destroy()
}

// A VTXO that is being unilaterally exited
type ExitVtxo struct {
	VtxoId      string
	AmountSats  uint64
	State       ExitState
	IsClaimable bool
}

func (r *ExitVtxo) Destroy() {
	FfiDestroyerString{}.Destroy(r.VtxoId)
	FfiDestroyerUint64{}.Destroy(r.AmountSats)
	FfiDestroyerExitState{}.Destroy(r.State)
	FfiDestroyerBool{}.Destroy(r.IsClaimable)
}

type FfiConverterExitVtxo struct{}

var FfiConverterExitVtxoINSTANCE = FfiConverterExitVtxo{}

func (c FfiConverterExitVtxo) Lift(rb RustBufferI) ExitVtxo {
	return LiftFromRustBuffer[ExitVtxo](c, rb)
}

func (c FfiConverterExitVtxo) Read(reader io.Reader) ExitVtxo {
	return ExitVtxo{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterExitStateINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterExitVtxo) Lower(value ExitVtxo) C.RustBuffer {
	return LowerIntoRustBuffer[ExitVtxo](c, value)
}

func (c FfiConverterExitVtxo) LowerExternal(value ExitVtxo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitVtxo](c, value))
}

func (c FfiConverterExitVtxo) Write(writer io.Writer, value ExitVtxo) {
	FfiConverterStringINSTANCE.Write(writer, value.VtxoId)
	FfiConverterUint64INSTANCE.Write(writer, value.AmountSats)
	FfiConverterExitStateINSTANCE.Write(writer, value.State)
	FfiConverterBoolINSTANCE.Write(writer, value.IsClaimable)
}

type FfiDestroyerExitVtxo struct{}

func (_ FfiDestroyerExitVtxo) Destroy(value ExitVtxo) {
	value.Destroy()
}

type FeeEstimate struct {
	GrossAmountSats uint64
	FeeSats         uint64
	NetAmountSats   uint64
	VtxosSpent      []string
}

func (r *FeeEstimate) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.GrossAmountSats)
	FfiDestroyerUint64{}.Destroy(r.FeeSats)
	FfiDestroyerUint64{}.Destroy(r.NetAmountSats)
	FfiDestroyerSequenceString{}.Destroy(r.VtxosSpent)
}

type FfiConverterFeeEstimate struct{}

var FfiConverterFeeEstimateINSTANCE = FfiConverterFeeEstimate{}

func (c FfiConverterFeeEstimate) Lift(rb RustBufferI) FeeEstimate {
	return LiftFromRustBuffer[FeeEstimate](c, rb)
}

func (c FfiConverterFeeEstimate) Read(reader io.Reader) FeeEstimate {
	return FeeEstimate{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterSequenceStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterFeeEstimate) Lower(value FeeEstimate) C.RustBuffer {
	return LowerIntoRustBuffer[FeeEstimate](c, value)
}

func (c FfiConverterFeeEstimate) LowerExternal(value FeeEstimate) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[FeeEstimate](c, value))
}

func (c FfiConverterFeeEstimate) Write(writer io.Writer, value FeeEstimate) {
	FfiConverterUint64INSTANCE.Write(writer, value.GrossAmountSats)
	FfiConverterUint64INSTANCE.Write(writer, value.FeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.NetAmountSats)
	FfiConverterSequenceStringINSTANCE.Write(writer, value.VtxosSpent)
}

type FfiDestroyerFeeEstimate struct{}

func (_ FfiDestroyerFeeEstimate) Destroy(value FeeEstimate) {
	value.Destroy()
}

// Network fee rates by urgency, mirroring `bark::chain::FeeRates`.
//
// Rates are in sat/kwu (satoshis per 1000 weight units) — divide by 250 for
// sat/vB.
type FeeRates struct {
	FastSatPerKwu    uint64
	RegularSatPerKwu uint64
	SlowSatPerKwu    uint64
}

func (r *FeeRates) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.FastSatPerKwu)
	FfiDestroyerUint64{}.Destroy(r.RegularSatPerKwu)
	FfiDestroyerUint64{}.Destroy(r.SlowSatPerKwu)
}

type FfiConverterFeeRates struct{}

var FfiConverterFeeRatesINSTANCE = FfiConverterFeeRates{}

func (c FfiConverterFeeRates) Lift(rb RustBufferI) FeeRates {
	return LiftFromRustBuffer[FeeRates](c, rb)
}

func (c FfiConverterFeeRates) Read(reader io.Reader) FeeRates {
	return FeeRates{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterFeeRates) Lower(value FeeRates) C.RustBuffer {
	return LowerIntoRustBuffer[FeeRates](c, value)
}

func (c FfiConverterFeeRates) LowerExternal(value FeeRates) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[FeeRates](c, value))
}

func (c FfiConverterFeeRates) Write(writer io.Writer, value FeeRates) {
	FfiConverterUint64INSTANCE.Write(writer, value.FastSatPerKwu)
	FfiConverterUint64INSTANCE.Write(writer, value.RegularSatPerKwu)
	FfiConverterUint64INSTANCE.Write(writer, value.SlowSatPerKwu)
}

type FfiDestroyerFeeRates struct{}

func (_ FfiDestroyerFeeRates) Destroy(value FeeRates) {
	value.Destroy()
}

// The Ark server's complete fee schedule, mirroring `ark::fees::FeeSchedule`.
type FeeSchedule struct {
	Board            BoardFees
	Offboard         OffboardFees
	Refresh          RefreshFees
	LightningReceive LightningReceiveFees
	LightningSend    LightningSendFees
}

func (r *FeeSchedule) Destroy() {
	FfiDestroyerBoardFees{}.Destroy(r.Board)
	FfiDestroyerOffboardFees{}.Destroy(r.Offboard)
	FfiDestroyerRefreshFees{}.Destroy(r.Refresh)
	FfiDestroyerLightningReceiveFees{}.Destroy(r.LightningReceive)
	FfiDestroyerLightningSendFees{}.Destroy(r.LightningSend)
}

type FfiConverterFeeSchedule struct{}

var FfiConverterFeeScheduleINSTANCE = FfiConverterFeeSchedule{}

func (c FfiConverterFeeSchedule) Lift(rb RustBufferI) FeeSchedule {
	return LiftFromRustBuffer[FeeSchedule](c, rb)
}

func (c FfiConverterFeeSchedule) Read(reader io.Reader) FeeSchedule {
	return FeeSchedule{
		FfiConverterBoardFeesINSTANCE.Read(reader),
		FfiConverterOffboardFeesINSTANCE.Read(reader),
		FfiConverterRefreshFeesINSTANCE.Read(reader),
		FfiConverterLightningReceiveFeesINSTANCE.Read(reader),
		FfiConverterLightningSendFeesINSTANCE.Read(reader),
	}
}

func (c FfiConverterFeeSchedule) Lower(value FeeSchedule) C.RustBuffer {
	return LowerIntoRustBuffer[FeeSchedule](c, value)
}

func (c FfiConverterFeeSchedule) LowerExternal(value FeeSchedule) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[FeeSchedule](c, value))
}

func (c FfiConverterFeeSchedule) Write(writer io.Writer, value FeeSchedule) {
	FfiConverterBoardFeesINSTANCE.Write(writer, value.Board)
	FfiConverterOffboardFeesINSTANCE.Write(writer, value.Offboard)
	FfiConverterRefreshFeesINSTANCE.Write(writer, value.Refresh)
	FfiConverterLightningReceiveFeesINSTANCE.Write(writer, value.LightningReceive)
	FfiConverterLightningSendFeesINSTANCE.Write(writer, value.LightningSend)
}

type FfiDestroyerFeeSchedule struct{}

func (_ FfiDestroyerFeeSchedule) Destroy(value FeeSchedule) {
	value.Destroy()
}

// Arguments for `Wallet::import_vtxo` / `Wallet::import_vtxos`, mirroring
// `bark::ImportVtxoArgs`.
//
// Every field has a default, so callers only set what they need.
type ImportVtxoArgs struct {
	// Gap limit for the key scan that decides whether we own the VTXOs,
	// overriding `Config.vtxo_key_gap_limit` for this call.
	//
	// Default: none (use the wallet's configured limit)
	GapLimit *uint32
	// Import as spendable without asking the server for each VTXO's state.
	// Skipping the check is faster but can leave the wallet holding spent
	// VTXOs marked spendable, which then fail when selected as inputs.
	//
	// Default: false
	SkipStatusCheck bool
	// Keep the VTXOs that import successfully even when another one in the
	// batch fails; the returned ids are the ones that were kept.
	//
	// Default: false, so a single failure discards the whole batch.
	AllowPartial bool
}

func (r *ImportVtxoArgs) Destroy() {
	FfiDestroyerOptionalUint32{}.Destroy(r.GapLimit)
	FfiDestroyerBool{}.Destroy(r.SkipStatusCheck)
	FfiDestroyerBool{}.Destroy(r.AllowPartial)
}

type FfiConverterImportVtxoArgs struct{}

var FfiConverterImportVtxoArgsINSTANCE = FfiConverterImportVtxoArgs{}

func (c FfiConverterImportVtxoArgs) Lift(rb RustBufferI) ImportVtxoArgs {
	return LiftFromRustBuffer[ImportVtxoArgs](c, rb)
}

func (c FfiConverterImportVtxoArgs) Read(reader io.Reader) ImportVtxoArgs {
	return ImportVtxoArgs{
		FfiConverterOptionalUint32INSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterImportVtxoArgs) Lower(value ImportVtxoArgs) C.RustBuffer {
	return LowerIntoRustBuffer[ImportVtxoArgs](c, value)
}

func (c FfiConverterImportVtxoArgs) LowerExternal(value ImportVtxoArgs) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ImportVtxoArgs](c, value))
}

func (c FfiConverterImportVtxoArgs) Write(writer io.Writer, value ImportVtxoArgs) {
	FfiConverterOptionalUint32INSTANCE.Write(writer, value.GapLimit)
	FfiConverterBoolINSTANCE.Write(writer, value.SkipStatusCheck)
	FfiConverterBoolINSTANCE.Write(writer, value.AllowPartial)
}

type FfiDestroyerImportVtxoArgs struct{}

func (_ FfiDestroyerImportVtxoArgs) Destroy(value ImportVtxoArgs) {
	value.Destroy()
}

type LightningInvoice struct {
	Invoice     string
	PaymentHash string
	AmountSats  uint64
}

func (r *LightningInvoice) Destroy() {
	FfiDestroyerString{}.Destroy(r.Invoice)
	FfiDestroyerString{}.Destroy(r.PaymentHash)
	FfiDestroyerUint64{}.Destroy(r.AmountSats)
}

type FfiConverterLightningInvoice struct{}

var FfiConverterLightningInvoiceINSTANCE = FfiConverterLightningInvoice{}

func (c FfiConverterLightningInvoice) Lift(rb RustBufferI) LightningInvoice {
	return LiftFromRustBuffer[LightningInvoice](c, rb)
}

func (c FfiConverterLightningInvoice) Read(reader io.Reader) LightningInvoice {
	return LightningInvoice{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterLightningInvoice) Lower(value LightningInvoice) C.RustBuffer {
	return LowerIntoRustBuffer[LightningInvoice](c, value)
}

func (c FfiConverterLightningInvoice) LowerExternal(value LightningInvoice) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[LightningInvoice](c, value))
}

func (c FfiConverterLightningInvoice) Write(writer io.Writer, value LightningInvoice) {
	FfiConverterStringINSTANCE.Write(writer, value.Invoice)
	FfiConverterStringINSTANCE.Write(writer, value.PaymentHash)
	FfiConverterUint64INSTANCE.Write(writer, value.AmountSats)
}

type FfiDestroyerLightningInvoice struct{}

func (_ FfiDestroyerLightningInvoice) Destroy(value LightningInvoice) {
	value.Destroy()
}

type LightningReceive struct {
	PaymentHash string
	Invoice     string
	// `None` for an amountless invoice that has not settled yet. It used to
	// report 0, which a UI cannot tell from a genuine zero.
	AmountSats *uint64
	// Receive progress: "awaiting-payment" | "htlcs-ready" |
	// "preimage-revealed" | "delivering" | "settled"
	State string
	// Known while in-progress; present when settled.
	PaymentPreimage *string
	// Unix timestamp (seconds), set only when the receive is settled.
	SettledAt *int64
	// Ark address the claimed VTXO is delivered to, for receives created with
	// `bolt11_invoice_for_address`. `None` for ordinary receives claimed by
	// this wallet, and always `None` once settled — the settled record does
	// not carry the destination.
	ClaimDestination *string
}

func (r *LightningReceive) Destroy() {
	FfiDestroyerString{}.Destroy(r.PaymentHash)
	FfiDestroyerString{}.Destroy(r.Invoice)
	FfiDestroyerOptionalUint64{}.Destroy(r.AmountSats)
	FfiDestroyerString{}.Destroy(r.State)
	FfiDestroyerOptionalString{}.Destroy(r.PaymentPreimage)
	FfiDestroyerOptionalInt64{}.Destroy(r.SettledAt)
	FfiDestroyerOptionalString{}.Destroy(r.ClaimDestination)
}

type FfiConverterLightningReceive struct{}

var FfiConverterLightningReceiveINSTANCE = FfiConverterLightningReceive{}

func (c FfiConverterLightningReceive) Lift(rb RustBufferI) LightningReceive {
	return LiftFromRustBuffer[LightningReceive](c, rb)
}

func (c FfiConverterLightningReceive) Read(reader io.Reader) LightningReceive {
	return LightningReceive{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalUint64INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalInt64INSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterLightningReceive) Lower(value LightningReceive) C.RustBuffer {
	return LowerIntoRustBuffer[LightningReceive](c, value)
}

func (c FfiConverterLightningReceive) LowerExternal(value LightningReceive) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[LightningReceive](c, value))
}

func (c FfiConverterLightningReceive) Write(writer io.Writer, value LightningReceive) {
	FfiConverterStringINSTANCE.Write(writer, value.PaymentHash)
	FfiConverterStringINSTANCE.Write(writer, value.Invoice)
	FfiConverterOptionalUint64INSTANCE.Write(writer, value.AmountSats)
	FfiConverterStringINSTANCE.Write(writer, value.State)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.PaymentPreimage)
	FfiConverterOptionalInt64INSTANCE.Write(writer, value.SettledAt)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.ClaimDestination)
}

type FfiDestroyerLightningReceive struct{}

func (_ FfiDestroyerLightningReceive) Destroy(value LightningReceive) {
	value.Destroy()
}

// Fees for receiving over lightning.
type LightningReceiveFees struct {
	BaseFeeSats uint64
	// Parts-per-million fee rate on the received amount.
	Ppm uint64
}

func (r *LightningReceiveFees) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.BaseFeeSats)
	FfiDestroyerUint64{}.Destroy(r.Ppm)
}

type FfiConverterLightningReceiveFees struct{}

var FfiConverterLightningReceiveFeesINSTANCE = FfiConverterLightningReceiveFees{}

func (c FfiConverterLightningReceiveFees) Lift(rb RustBufferI) LightningReceiveFees {
	return LiftFromRustBuffer[LightningReceiveFees](c, rb)
}

func (c FfiConverterLightningReceiveFees) Read(reader io.Reader) LightningReceiveFees {
	return LightningReceiveFees{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterLightningReceiveFees) Lower(value LightningReceiveFees) C.RustBuffer {
	return LowerIntoRustBuffer[LightningReceiveFees](c, value)
}

func (c FfiConverterLightningReceiveFees) LowerExternal(value LightningReceiveFees) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[LightningReceiveFees](c, value))
}

func (c FfiConverterLightningReceiveFees) Write(writer io.Writer, value LightningReceiveFees) {
	FfiConverterUint64INSTANCE.Write(writer, value.BaseFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.Ppm)
}

type FfiDestroyerLightningReceiveFees struct{}

func (_ FfiDestroyerLightningReceiveFees) Destroy(value LightningReceiveFees) {
	value.Destroy()
}

type LightningSend struct {
	Invoice string
	// Amount being paid, in sats (`payment_amount`).
	AmountSats uint64
	// Routing/ark fee for the send, in sats.
	FeeSats uint64
	// Number of input VTXOs locked into the in-flight HTLC.
	HtlcVtxoCount uint32
	// Whether the send is stuck in the revocation-failed state: the payment
	// failed but revoking the HTLC also failed
	HasFailedRevocation bool
}

func (r *LightningSend) Destroy() {
	FfiDestroyerString{}.Destroy(r.Invoice)
	FfiDestroyerUint64{}.Destroy(r.AmountSats)
	FfiDestroyerUint64{}.Destroy(r.FeeSats)
	FfiDestroyerUint32{}.Destroy(r.HtlcVtxoCount)
	FfiDestroyerBool{}.Destroy(r.HasFailedRevocation)
}

type FfiConverterLightningSend struct{}

var FfiConverterLightningSendINSTANCE = FfiConverterLightningSend{}

func (c FfiConverterLightningSend) Lift(rb RustBufferI) LightningSend {
	return LiftFromRustBuffer[LightningSend](c, rb)
}

func (c FfiConverterLightningSend) Read(reader io.Reader) LightningSend {
	return LightningSend{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterLightningSend) Lower(value LightningSend) C.RustBuffer {
	return LowerIntoRustBuffer[LightningSend](c, value)
}

func (c FfiConverterLightningSend) LowerExternal(value LightningSend) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[LightningSend](c, value))
}

func (c FfiConverterLightningSend) Write(writer io.Writer, value LightningSend) {
	FfiConverterStringINSTANCE.Write(writer, value.Invoice)
	FfiConverterUint64INSTANCE.Write(writer, value.AmountSats)
	FfiConverterUint64INSTANCE.Write(writer, value.FeeSats)
	FfiConverterUint32INSTANCE.Write(writer, value.HtlcVtxoCount)
	FfiConverterBoolINSTANCE.Write(writer, value.HasFailedRevocation)
}

type FfiDestroyerLightningSend struct{}

func (_ FfiDestroyerLightningSend) Destroy(value LightningSend) {
	value.Destroy()
}

// Fees for sending over lightning.
type LightningSendFees struct {
	MinFeeSats     uint64
	BaseFeeSats    uint64
	PpmExpiryTable []PpmExpiryFeeEntry
}

func (r *LightningSendFees) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.MinFeeSats)
	FfiDestroyerUint64{}.Destroy(r.BaseFeeSats)
	FfiDestroyerSequencePpmExpiryFeeEntry{}.Destroy(r.PpmExpiryTable)
}

type FfiConverterLightningSendFees struct{}

var FfiConverterLightningSendFeesINSTANCE = FfiConverterLightningSendFees{}

func (c FfiConverterLightningSendFees) Lift(rb RustBufferI) LightningSendFees {
	return LiftFromRustBuffer[LightningSendFees](c, rb)
}

func (c FfiConverterLightningSendFees) Read(reader io.Reader) LightningSendFees {
	return LightningSendFees{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterSequencePpmExpiryFeeEntryINSTANCE.Read(reader),
	}
}

func (c FfiConverterLightningSendFees) Lower(value LightningSendFees) C.RustBuffer {
	return LowerIntoRustBuffer[LightningSendFees](c, value)
}

func (c FfiConverterLightningSendFees) LowerExternal(value LightningSendFees) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[LightningSendFees](c, value))
}

func (c FfiConverterLightningSendFees) Write(writer io.Writer, value LightningSendFees) {
	FfiConverterUint64INSTANCE.Write(writer, value.MinFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.BaseFeeSats)
	FfiConverterSequencePpmExpiryFeeEntryINSTANCE.Write(writer, value.PpmExpiryTable)
}

type FfiDestroyerLightningSendFees struct{}

func (_ FfiDestroyerLightningSendFees) Destroy(value LightningSendFees) {
	value.Destroy()
}

type Movement struct {
	Id                   uint32
	Status               string
	SubsystemName        string
	SubsystemKind        string
	MetadataJson         string
	IntendedBalanceSats  int64
	EffectiveBalanceSats int64
	OffchainFeeSats      uint64
	SentToAddresses      []string
	ReceivedOnAddresses  []string
	InputVtxoIds         []string
	OutputVtxoIds        []string
	ExitedVtxoIds        []string
	CreatedAt            string
	UpdatedAt            string
	CompletedAt          *string
	PaymentHash          *string
	LightningInvoice     *string
	LightningOffer       *string
}

func (r *Movement) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Id)
	FfiDestroyerString{}.Destroy(r.Status)
	FfiDestroyerString{}.Destroy(r.SubsystemName)
	FfiDestroyerString{}.Destroy(r.SubsystemKind)
	FfiDestroyerString{}.Destroy(r.MetadataJson)
	FfiDestroyerInt64{}.Destroy(r.IntendedBalanceSats)
	FfiDestroyerInt64{}.Destroy(r.EffectiveBalanceSats)
	FfiDestroyerUint64{}.Destroy(r.OffchainFeeSats)
	FfiDestroyerSequenceString{}.Destroy(r.SentToAddresses)
	FfiDestroyerSequenceString{}.Destroy(r.ReceivedOnAddresses)
	FfiDestroyerSequenceString{}.Destroy(r.InputVtxoIds)
	FfiDestroyerSequenceString{}.Destroy(r.OutputVtxoIds)
	FfiDestroyerSequenceString{}.Destroy(r.ExitedVtxoIds)
	FfiDestroyerString{}.Destroy(r.CreatedAt)
	FfiDestroyerString{}.Destroy(r.UpdatedAt)
	FfiDestroyerOptionalString{}.Destroy(r.CompletedAt)
	FfiDestroyerOptionalString{}.Destroy(r.PaymentHash)
	FfiDestroyerOptionalString{}.Destroy(r.LightningInvoice)
	FfiDestroyerOptionalString{}.Destroy(r.LightningOffer)
}

type FfiConverterMovement struct{}

var FfiConverterMovementINSTANCE = FfiConverterMovement{}

func (c FfiConverterMovement) Lift(rb RustBufferI) Movement {
	return LiftFromRustBuffer[Movement](c, rb)
}

func (c FfiConverterMovement) Read(reader io.Reader) Movement {
	return Movement{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterInt64INSTANCE.Read(reader),
		FfiConverterInt64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterSequenceStringINSTANCE.Read(reader),
		FfiConverterSequenceStringINSTANCE.Read(reader),
		FfiConverterSequenceStringINSTANCE.Read(reader),
		FfiConverterSequenceStringINSTANCE.Read(reader),
		FfiConverterSequenceStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterMovement) Lower(value Movement) C.RustBuffer {
	return LowerIntoRustBuffer[Movement](c, value)
}

func (c FfiConverterMovement) LowerExternal(value Movement) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Movement](c, value))
}

func (c FfiConverterMovement) Write(writer io.Writer, value Movement) {
	FfiConverterUint32INSTANCE.Write(writer, value.Id)
	FfiConverterStringINSTANCE.Write(writer, value.Status)
	FfiConverterStringINSTANCE.Write(writer, value.SubsystemName)
	FfiConverterStringINSTANCE.Write(writer, value.SubsystemKind)
	FfiConverterStringINSTANCE.Write(writer, value.MetadataJson)
	FfiConverterInt64INSTANCE.Write(writer, value.IntendedBalanceSats)
	FfiConverterInt64INSTANCE.Write(writer, value.EffectiveBalanceSats)
	FfiConverterUint64INSTANCE.Write(writer, value.OffchainFeeSats)
	FfiConverterSequenceStringINSTANCE.Write(writer, value.SentToAddresses)
	FfiConverterSequenceStringINSTANCE.Write(writer, value.ReceivedOnAddresses)
	FfiConverterSequenceStringINSTANCE.Write(writer, value.InputVtxoIds)
	FfiConverterSequenceStringINSTANCE.Write(writer, value.OutputVtxoIds)
	FfiConverterSequenceStringINSTANCE.Write(writer, value.ExitedVtxoIds)
	FfiConverterStringINSTANCE.Write(writer, value.CreatedAt)
	FfiConverterStringINSTANCE.Write(writer, value.UpdatedAt)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.CompletedAt)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.PaymentHash)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.LightningInvoice)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.LightningOffer)
}

type FfiDestroyerMovement struct{}

func (_ FfiDestroyerMovement) Destroy(value Movement) {
	value.Destroy()
}

// Fees for offboarding VTXOs to an onchain address.
type OffboardFees struct {
	BaseFeeSats uint64
	// Fixed number of virtual bytes charged on top of the output size.
	FixedAdditionalVb uint64
	PpmExpiryTable    []PpmExpiryFeeEntry
}

func (r *OffboardFees) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.BaseFeeSats)
	FfiDestroyerUint64{}.Destroy(r.FixedAdditionalVb)
	FfiDestroyerSequencePpmExpiryFeeEntry{}.Destroy(r.PpmExpiryTable)
}

type FfiConverterOffboardFees struct{}

var FfiConverterOffboardFeesINSTANCE = FfiConverterOffboardFees{}

func (c FfiConverterOffboardFees) Lift(rb RustBufferI) OffboardFees {
	return LiftFromRustBuffer[OffboardFees](c, rb)
}

func (c FfiConverterOffboardFees) Read(reader io.Reader) OffboardFees {
	return OffboardFees{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterSequencePpmExpiryFeeEntryINSTANCE.Read(reader),
	}
}

func (c FfiConverterOffboardFees) Lower(value OffboardFees) C.RustBuffer {
	return LowerIntoRustBuffer[OffboardFees](c, value)
}

func (c FfiConverterOffboardFees) LowerExternal(value OffboardFees) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[OffboardFees](c, value))
}

func (c FfiConverterOffboardFees) Write(writer io.Writer, value OffboardFees) {
	FfiConverterUint64INSTANCE.Write(writer, value.BaseFeeSats)
	FfiConverterUint64INSTANCE.Write(writer, value.FixedAdditionalVb)
	FfiConverterSequencePpmExpiryFeeEntryINSTANCE.Write(writer, value.PpmExpiryTable)
}

type FfiDestroyerOffboardFees struct{}

func (_ FfiDestroyerOffboardFees) Destroy(value OffboardFees) {
	value.Destroy()
}

type OffboardResult struct {
	Txid string
}

func (r *OffboardResult) Destroy() {
	FfiDestroyerString{}.Destroy(r.Txid)
}

type FfiConverterOffboardResult struct{}

var FfiConverterOffboardResultINSTANCE = FfiConverterOffboardResult{}

func (c FfiConverterOffboardResult) Lift(rb RustBufferI) OffboardResult {
	return LiftFromRustBuffer[OffboardResult](c, rb)
}

func (c FfiConverterOffboardResult) Read(reader io.Reader) OffboardResult {
	return OffboardResult{
		FfiConverterStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterOffboardResult) Lower(value OffboardResult) C.RustBuffer {
	return LowerIntoRustBuffer[OffboardResult](c, value)
}

func (c FfiConverterOffboardResult) LowerExternal(value OffboardResult) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[OffboardResult](c, value))
}

func (c FfiConverterOffboardResult) Write(writer io.Writer, value OffboardResult) {
	FfiConverterStringINSTANCE.Write(writer, value.Txid)
}

type FfiDestroyerOffboardResult struct{}

func (_ FfiDestroyerOffboardResult) Destroy(value OffboardResult) {
	value.Destroy()
}

type OnchainBalance struct {
	ConfirmedSats uint64
	PendingSats   uint64
	TotalSats     uint64
}

func (r *OnchainBalance) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.ConfirmedSats)
	FfiDestroyerUint64{}.Destroy(r.PendingSats)
	FfiDestroyerUint64{}.Destroy(r.TotalSats)
}

type FfiConverterOnchainBalance struct{}

var FfiConverterOnchainBalanceINSTANCE = FfiConverterOnchainBalance{}

func (c FfiConverterOnchainBalance) Lift(rb RustBufferI) OnchainBalance {
	return LiftFromRustBuffer[OnchainBalance](c, rb)
}

func (c FfiConverterOnchainBalance) Read(reader io.Reader) OnchainBalance {
	return OnchainBalance{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterOnchainBalance) Lower(value OnchainBalance) C.RustBuffer {
	return LowerIntoRustBuffer[OnchainBalance](c, value)
}

func (c FfiConverterOnchainBalance) LowerExternal(value OnchainBalance) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[OnchainBalance](c, value))
}

func (c FfiConverterOnchainBalance) Write(writer io.Writer, value OnchainBalance) {
	FfiConverterUint64INSTANCE.Write(writer, value.ConfirmedSats)
	FfiConverterUint64INSTANCE.Write(writer, value.PendingSats)
	FfiConverterUint64INSTANCE.Write(writer, value.TotalSats)
}

type FfiDestroyerOnchainBalance struct{}

func (_ FfiDestroyerOnchainBalance) Destroy(value OnchainBalance) {
	value.Destroy()
}

// A Bitcoin transaction outpoint (reference to a previous output)
type OutPoint struct {
	Txid string
	Vout uint32
}

func (r *OutPoint) Destroy() {
	FfiDestroyerString{}.Destroy(r.Txid)
	FfiDestroyerUint32{}.Destroy(r.Vout)
}

type FfiConverterOutPoint struct{}

var FfiConverterOutPointINSTANCE = FfiConverterOutPoint{}

func (c FfiConverterOutPoint) Lift(rb RustBufferI) OutPoint {
	return LiftFromRustBuffer[OutPoint](c, rb)
}

func (c FfiConverterOutPoint) Read(reader io.Reader) OutPoint {
	return OutPoint{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterOutPoint) Lower(value OutPoint) C.RustBuffer {
	return LowerIntoRustBuffer[OutPoint](c, value)
}

func (c FfiConverterOutPoint) LowerExternal(value OutPoint) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[OutPoint](c, value))
}

func (c FfiConverterOutPoint) Write(writer io.Writer, value OutPoint) {
	FfiConverterStringINSTANCE.Write(writer, value.Txid)
	FfiConverterUint32INSTANCE.Write(writer, value.Vout)
}

type FfiDestroyerOutPoint struct{}

func (_ FfiDestroyerOutPoint) Destroy(value OutPoint) {
	value.Destroy()
}

type PendingBoard struct {
	VtxoId     string
	AmountSats uint64
	Txid       string
}

func (r *PendingBoard) Destroy() {
	FfiDestroyerString{}.Destroy(r.VtxoId)
	FfiDestroyerUint64{}.Destroy(r.AmountSats)
	FfiDestroyerString{}.Destroy(r.Txid)
}

type FfiConverterPendingBoard struct{}

var FfiConverterPendingBoardINSTANCE = FfiConverterPendingBoard{}

func (c FfiConverterPendingBoard) Lift(rb RustBufferI) PendingBoard {
	return LiftFromRustBuffer[PendingBoard](c, rb)
}

func (c FfiConverterPendingBoard) Read(reader io.Reader) PendingBoard {
	return PendingBoard{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterPendingBoard) Lower(value PendingBoard) C.RustBuffer {
	return LowerIntoRustBuffer[PendingBoard](c, value)
}

func (c FfiConverterPendingBoard) LowerExternal(value PendingBoard) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[PendingBoard](c, value))
}

func (c FfiConverterPendingBoard) Write(writer io.Writer, value PendingBoard) {
	FfiConverterStringINSTANCE.Write(writer, value.VtxoId)
	FfiConverterUint64INSTANCE.Write(writer, value.AmountSats)
	FfiConverterStringINSTANCE.Write(writer, value.Txid)
}

type FfiDestroyerPendingBoard struct{}

func (_ FfiDestroyerPendingBoard) Destroy(value PendingBoard) {
	value.Destroy()
}

// One tier of a PPM-by-expiry fee table.
//
// The entry applies when a VTXO expires in at most `expiry_blocks_threshold`
// blocks and no other entry has a threshold between this one and the VTXO's
// actual expiry distance. Tables are sorted ascending by threshold.
type PpmExpiryFeeEntry struct {
	ExpiryBlocksThreshold uint32
	// Parts-per-million fee rate applied for this expiry period.
	Ppm uint64
}

func (r *PpmExpiryFeeEntry) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.ExpiryBlocksThreshold)
	FfiDestroyerUint64{}.Destroy(r.Ppm)
}

type FfiConverterPpmExpiryFeeEntry struct{}

var FfiConverterPpmExpiryFeeEntryINSTANCE = FfiConverterPpmExpiryFeeEntry{}

func (c FfiConverterPpmExpiryFeeEntry) Lift(rb RustBufferI) PpmExpiryFeeEntry {
	return LiftFromRustBuffer[PpmExpiryFeeEntry](c, rb)
}

func (c FfiConverterPpmExpiryFeeEntry) Read(reader io.Reader) PpmExpiryFeeEntry {
	return PpmExpiryFeeEntry{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterPpmExpiryFeeEntry) Lower(value PpmExpiryFeeEntry) C.RustBuffer {
	return LowerIntoRustBuffer[PpmExpiryFeeEntry](c, value)
}

func (c FfiConverterPpmExpiryFeeEntry) LowerExternal(value PpmExpiryFeeEntry) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[PpmExpiryFeeEntry](c, value))
}

func (c FfiConverterPpmExpiryFeeEntry) Write(writer io.Writer, value PpmExpiryFeeEntry) {
	FfiConverterUint32INSTANCE.Write(writer, value.ExpiryBlocksThreshold)
	FfiConverterUint64INSTANCE.Write(writer, value.Ppm)
}

type FfiDestroyerPpmExpiryFeeEntry struct{}

func (_ FfiDestroyerPpmExpiryFeeEntry) Destroy(value PpmExpiryFeeEntry) {
	value.Destroy()
}

// One bucket of a [`RecoveryReport`].
//
// `total_sats` only sums the VTXOs whose amount is known, so it can
// under-count `failed`, where a VTXO may have failed before being fetched.
// `vtxo_ids` is sorted, since upstream buckets them unordered.
type RecoveryBucket struct {
	VtxoIds   []string
	TotalSats uint64
}

func (r *RecoveryBucket) Destroy() {
	FfiDestroyerSequenceString{}.Destroy(r.VtxoIds)
	FfiDestroyerUint64{}.Destroy(r.TotalSats)
}

type FfiConverterRecoveryBucket struct{}

var FfiConverterRecoveryBucketINSTANCE = FfiConverterRecoveryBucket{}

func (c FfiConverterRecoveryBucket) Lift(rb RustBufferI) RecoveryBucket {
	return LiftFromRustBuffer[RecoveryBucket](c, rb)
}

func (c FfiConverterRecoveryBucket) Read(reader io.Reader) RecoveryBucket {
	return RecoveryBucket{
		FfiConverterSequenceStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterRecoveryBucket) Lower(value RecoveryBucket) C.RustBuffer {
	return LowerIntoRustBuffer[RecoveryBucket](c, value)
}

func (c FfiConverterRecoveryBucket) LowerExternal(value RecoveryBucket) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[RecoveryBucket](c, value))
}

func (c FfiConverterRecoveryBucket) Write(writer io.Writer, value RecoveryBucket) {
	FfiConverterSequenceStringINSTANCE.Write(writer, value.VtxoIds)
	FfiConverterUint64INSTANCE.Write(writer, value.TotalSats)
}

type FfiDestroyerRecoveryBucket struct{}

func (_ FfiDestroyerRecoveryBucket) Destroy(value RecoveryBucket) {
	value.Destroy()
}

// Outcome of a recovery scan: every VTXO id the scan looked at, bucketed by
// what was decided about it.
//
// `skipped` vs `failed` is the load-bearing distinction: a `skipped` VTXO was
// decided not to be spendable (spent, exited, or reported non-spendable),
// while a `failed` one could not be decided because of an error, so its funds
// may still be missing. `failed` is retryable via `recover_vtxos`; a `foreign`
// id instead sits beyond the key-derivation gap limit and needs a wider scan.
type RecoveryReport struct {
	// Spendable VTXOs that were successfully re-imported.
	Recovered RecoveryBucket
	// Deliberately left out: spent into a newer recovered VTXO, exited
	// on-chain, or reported non-spendable by the server.
	Skipped RecoveryBucket
	// No matching key could be derived within the gap limit (the configured
	// run of consecutive unused key indices, 250 by default). For a mailbox scan these are most likely this wallet's
	// own VTXOs, keyed beyond the limit, so funds may be missing; retrying
	// won't help, only a wider gap limit (see `Config.vtxo_key_gap_limit` and
	// the `gap_limit` override on `recover_vtxos`). For `recover_vtxos` it
	// just means the caller passed an id this wallet doesn't own.
	Foreign RecoveryBucket
	// Could not be decided due to an error. Not known to be spent, so funds
	// may be missing. Retryable.
	Failed RecoveryBucket
	// Already fully exited on-chain.
	Exited RecoveryBucket
	// Whether the scan accounted for every VTXO: no `failed`, no `foreign`.
	IsComplete bool
}

func (r *RecoveryReport) Destroy() {
	FfiDestroyerRecoveryBucket{}.Destroy(r.Recovered)
	FfiDestroyerRecoveryBucket{}.Destroy(r.Skipped)
	FfiDestroyerRecoveryBucket{}.Destroy(r.Foreign)
	FfiDestroyerRecoveryBucket{}.Destroy(r.Failed)
	FfiDestroyerRecoveryBucket{}.Destroy(r.Exited)
	FfiDestroyerBool{}.Destroy(r.IsComplete)
}

type FfiConverterRecoveryReport struct{}

var FfiConverterRecoveryReportINSTANCE = FfiConverterRecoveryReport{}

func (c FfiConverterRecoveryReport) Lift(rb RustBufferI) RecoveryReport {
	return LiftFromRustBuffer[RecoveryReport](c, rb)
}

func (c FfiConverterRecoveryReport) Read(reader io.Reader) RecoveryReport {
	return RecoveryReport{
		FfiConverterRecoveryBucketINSTANCE.Read(reader),
		FfiConverterRecoveryBucketINSTANCE.Read(reader),
		FfiConverterRecoveryBucketINSTANCE.Read(reader),
		FfiConverterRecoveryBucketINSTANCE.Read(reader),
		FfiConverterRecoveryBucketINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterRecoveryReport) Lower(value RecoveryReport) C.RustBuffer {
	return LowerIntoRustBuffer[RecoveryReport](c, value)
}

func (c FfiConverterRecoveryReport) LowerExternal(value RecoveryReport) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[RecoveryReport](c, value))
}

func (c FfiConverterRecoveryReport) Write(writer io.Writer, value RecoveryReport) {
	FfiConverterRecoveryBucketINSTANCE.Write(writer, value.Recovered)
	FfiConverterRecoveryBucketINSTANCE.Write(writer, value.Skipped)
	FfiConverterRecoveryBucketINSTANCE.Write(writer, value.Foreign)
	FfiConverterRecoveryBucketINSTANCE.Write(writer, value.Failed)
	FfiConverterRecoveryBucketINSTANCE.Write(writer, value.Exited)
	FfiConverterBoolINSTANCE.Write(writer, value.IsComplete)
}

type FfiDestroyerRecoveryReport struct{}

func (_ FfiDestroyerRecoveryReport) Destroy(value RecoveryReport) {
	value.Destroy()
}

// Fees for refreshing VTXOs in a round.
type RefreshFees struct {
	BaseFeeSats    uint64
	PpmExpiryTable []PpmExpiryFeeEntry
}

func (r *RefreshFees) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.BaseFeeSats)
	FfiDestroyerSequencePpmExpiryFeeEntry{}.Destroy(r.PpmExpiryTable)
}

type FfiConverterRefreshFees struct{}

var FfiConverterRefreshFeesINSTANCE = FfiConverterRefreshFees{}

func (c FfiConverterRefreshFees) Lift(rb RustBufferI) RefreshFees {
	return LiftFromRustBuffer[RefreshFees](c, rb)
}

func (c FfiConverterRefreshFees) Read(reader io.Reader) RefreshFees {
	return RefreshFees{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterSequencePpmExpiryFeeEntryINSTANCE.Read(reader),
	}
}

func (c FfiConverterRefreshFees) Lower(value RefreshFees) C.RustBuffer {
	return LowerIntoRustBuffer[RefreshFees](c, value)
}

func (c FfiConverterRefreshFees) LowerExternal(value RefreshFees) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[RefreshFees](c, value))
}

func (c FfiConverterRefreshFees) Write(writer io.Writer, value RefreshFees) {
	FfiConverterUint64INSTANCE.Write(writer, value.BaseFeeSats)
	FfiConverterSequencePpmExpiryFeeEntryINSTANCE.Write(writer, value.PpmExpiryTable)
}

type FfiDestroyerRefreshFees struct{}

func (_ FfiDestroyerRefreshFees) Destroy(value RefreshFees) {
	value.Destroy()
}

// A pending round state
type RoundState struct {
	Id uint32
	// Whether the interactive part of the round is ongoing. Equivalent to
	// `state` being `Pending` or `Ongoing`; kept for compatibility.
	Ongoing bool
	// Lifecycle phase of the participation.
	State RoundFlowKind
	// Block height a delegated participation waits for, if it asked the
	// server to schedule one. Only set while `state` is `DelegatedPending`.
	ScheduledHeight *uint32
}

func (r *RoundState) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Id)
	FfiDestroyerBool{}.Destroy(r.Ongoing)
	FfiDestroyerRoundFlowKind{}.Destroy(r.State)
	FfiDestroyerOptionalUint32{}.Destroy(r.ScheduledHeight)
}

type FfiConverterRoundState struct{}

var FfiConverterRoundStateINSTANCE = FfiConverterRoundState{}

func (c FfiConverterRoundState) Lift(rb RustBufferI) RoundState {
	return LiftFromRustBuffer[RoundState](c, rb)
}

func (c FfiConverterRoundState) Read(reader io.Reader) RoundState {
	return RoundState{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterRoundFlowKindINSTANCE.Read(reader),
		FfiConverterOptionalUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterRoundState) Lower(value RoundState) C.RustBuffer {
	return LowerIntoRustBuffer[RoundState](c, value)
}

func (c FfiConverterRoundState) LowerExternal(value RoundState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[RoundState](c, value))
}

func (c FfiConverterRoundState) Write(writer io.Writer, value RoundState) {
	FfiConverterUint32INSTANCE.Write(writer, value.Id)
	FfiConverterBoolINSTANCE.Write(writer, value.Ongoing)
	FfiConverterRoundFlowKindINSTANCE.Write(writer, value.State)
	FfiConverterOptionalUint32INSTANCE.Write(writer, value.ScheduledHeight)
}

type FfiDestroyerRoundState struct{}

func (_ FfiDestroyerRoundState) Destroy(value RoundState) {
	value.Destroy()
}

type Vtxo struct {
	Id           string
	AmountSats   uint64
	ExpiryHeight uint32
	Kind         string
	State        VtxoState
	// Genesis chain length. Compare against `ArkInfo.max_vtxo_exit_depth` to
	// detect VTXOs nearing the server's OOR-cosign refusal threshold.
	ExitDepth uint32
	// Weight units of the unilateral exit transaction chain. Lets clients
	// estimate exit cost without loading the full genesis.
	ExitTxWeightWu uint64
	// Whether this VTXO's recovery state has been asserted with the server:
	// its id posted to the recovery mailbox and its signed transaction chain
	// registered. Only ever moves from `false` to `true`; the sync-time
	// catch-up re-uploads the ones still `false`.
	Registered bool
}

func (r *Vtxo) Destroy() {
	FfiDestroyerString{}.Destroy(r.Id)
	FfiDestroyerUint64{}.Destroy(r.AmountSats)
	FfiDestroyerUint32{}.Destroy(r.ExpiryHeight)
	FfiDestroyerString{}.Destroy(r.Kind)
	FfiDestroyerVtxoState{}.Destroy(r.State)
	FfiDestroyerUint32{}.Destroy(r.ExitDepth)
	FfiDestroyerUint64{}.Destroy(r.ExitTxWeightWu)
	FfiDestroyerBool{}.Destroy(r.Registered)
}

type FfiConverterVtxo struct{}

var FfiConverterVtxoINSTANCE = FfiConverterVtxo{}

func (c FfiConverterVtxo) Lift(rb RustBufferI) Vtxo {
	return LiftFromRustBuffer[Vtxo](c, rb)
}

func (c FfiConverterVtxo) Read(reader io.Reader) Vtxo {
	return Vtxo{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterVtxoStateINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterVtxo) Lower(value Vtxo) C.RustBuffer {
	return LowerIntoRustBuffer[Vtxo](c, value)
}

func (c FfiConverterVtxo) LowerExternal(value Vtxo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Vtxo](c, value))
}

func (c FfiConverterVtxo) Write(writer io.Writer, value Vtxo) {
	FfiConverterStringINSTANCE.Write(writer, value.Id)
	FfiConverterUint64INSTANCE.Write(writer, value.AmountSats)
	FfiConverterUint32INSTANCE.Write(writer, value.ExpiryHeight)
	FfiConverterStringINSTANCE.Write(writer, value.Kind)
	FfiConverterVtxoStateINSTANCE.Write(writer, value.State)
	FfiConverterUint32INSTANCE.Write(writer, value.ExitDepth)
	FfiConverterUint64INSTANCE.Write(writer, value.ExitTxWeightWu)
	FfiConverterBoolINSTANCE.Write(writer, value.Registered)
}

type FfiDestroyerVtxo struct{}

func (_ FfiDestroyerVtxo) Destroy(value Vtxo) {
	value.Destroy()
}

// Optional arguments for [`Wallet::open`], mirroring [`bark::OpenWalletArgs`].
//
// Every field has a default, so callers only set what they need.
type WalletOpenArgs struct {
	// Whether to run the background daemon
	//
	// When disabled, you must manually call `Wallet::sync` to sync the wallet.
	//
	// Default: true
	RunDaemon bool
	// The data directory to use for this wallet
	Datadir string
	// The onchain wallet to use, if any
	//
	// Default: none
	Onchain **OnchainWallet
	// Whether to create a new wallet if no wallet exists
	//
	// Default: true
	CreateIfNotExists bool
	// Whether to create a new wallet even if the Ark server cannot be reached
	//
	// Default: false
	CreateWithoutServer bool
	// Whether to skip the seed-recovery mailbox scan
	//
	// The scan runs on the open that creates the wallet locally and makes
	// network calls; its outcome is available from `Wallet::recovery_status`.
	// Set this to open without it.
	//
	// Default: false
	SkipRecovery bool
}

func (r *WalletOpenArgs) Destroy() {
	FfiDestroyerBool{}.Destroy(r.RunDaemon)
	FfiDestroyerString{}.Destroy(r.Datadir)
	FfiDestroyerOptionalOnchainWallet{}.Destroy(r.Onchain)
	FfiDestroyerBool{}.Destroy(r.CreateIfNotExists)
	FfiDestroyerBool{}.Destroy(r.CreateWithoutServer)
	FfiDestroyerBool{}.Destroy(r.SkipRecovery)
}

type FfiConverterWalletOpenArgs struct{}

var FfiConverterWalletOpenArgsINSTANCE = FfiConverterWalletOpenArgs{}

func (c FfiConverterWalletOpenArgs) Lift(rb RustBufferI) WalletOpenArgs {
	return LiftFromRustBuffer[WalletOpenArgs](c, rb)
}

func (c FfiConverterWalletOpenArgs) Read(reader io.Reader) WalletOpenArgs {
	return WalletOpenArgs{
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalOnchainWalletINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterWalletOpenArgs) Lower(value WalletOpenArgs) C.RustBuffer {
	return LowerIntoRustBuffer[WalletOpenArgs](c, value)
}

func (c FfiConverterWalletOpenArgs) LowerExternal(value WalletOpenArgs) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[WalletOpenArgs](c, value))
}

func (c FfiConverterWalletOpenArgs) Write(writer io.Writer, value WalletOpenArgs) {
	FfiConverterBoolINSTANCE.Write(writer, value.RunDaemon)
	FfiConverterStringINSTANCE.Write(writer, value.Datadir)
	FfiConverterOptionalOnchainWalletINSTANCE.Write(writer, value.Onchain)
	FfiConverterBoolINSTANCE.Write(writer, value.CreateIfNotExists)
	FfiConverterBoolINSTANCE.Write(writer, value.CreateWithoutServer)
	FfiConverterBoolINSTANCE.Write(writer, value.SkipRecovery)
}

type FfiDestroyerWalletOpenArgs struct{}

func (_ FfiDestroyerWalletOpenArgs) Destroy(value WalletOpenArgs) {
	value.Destroy()
}

type WalletProperties struct {
	Network     Network
	Fingerprint string
}

func (r *WalletProperties) Destroy() {
	FfiDestroyerNetwork{}.Destroy(r.Network)
	FfiDestroyerString{}.Destroy(r.Fingerprint)
}

type FfiConverterWalletProperties struct{}

var FfiConverterWalletPropertiesINSTANCE = FfiConverterWalletProperties{}

func (c FfiConverterWalletProperties) Lift(rb RustBufferI) WalletProperties {
	return LiftFromRustBuffer[WalletProperties](c, rb)
}

func (c FfiConverterWalletProperties) Read(reader io.Reader) WalletProperties {
	return WalletProperties{
		FfiConverterNetworkINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterWalletProperties) Lower(value WalletProperties) C.RustBuffer {
	return LowerIntoRustBuffer[WalletProperties](c, value)
}

func (c FfiConverterWalletProperties) LowerExternal(value WalletProperties) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[WalletProperties](c, value))
}

func (c FfiConverterWalletProperties) Write(writer io.Writer, value WalletProperties) {
	FfiConverterNetworkINSTANCE.Write(writer, value.Network)
	FfiConverterStringINSTANCE.Write(writer, value.Fingerprint)
}

type FfiDestroyerWalletProperties struct{}

func (_ FfiDestroyerWalletProperties) Destroy(value WalletProperties) {
	value.Destroy()
}

// Summary of one onchain wallet transaction, mirroring
// `bark::onchain::WalletTxInfo`.
type WalletTransaction struct {
	Txid string
	// The raw transaction, consensus-serialized as hex.
	TxHex string
	// Total fee paid by the transaction, when computable. `None` for
	// inbound or collaboratively-funded txs whose foreign prevouts the
	// wallet has not indexed.
	OnchainFeeSats *uint64
	// Net change to the wallet's balance: received minus sent over
	// wallet-owned outputs.
	BalanceChangeSats int64
	// `Some` if confirmed in a block, `None` if still in the mempool.
	Confirmation *BlockRef
	// `true` when this tx spends a P2A fee anchor — i.e. it is a CPFP
	// child bumping the parent that created the anchor (exit fee txs).
	IsCpfp bool
}

func (r *WalletTransaction) Destroy() {
	FfiDestroyerString{}.Destroy(r.Txid)
	FfiDestroyerString{}.Destroy(r.TxHex)
	FfiDestroyerOptionalUint64{}.Destroy(r.OnchainFeeSats)
	FfiDestroyerInt64{}.Destroy(r.BalanceChangeSats)
	FfiDestroyerOptionalBlockRef{}.Destroy(r.Confirmation)
	FfiDestroyerBool{}.Destroy(r.IsCpfp)
}

type FfiConverterWalletTransaction struct{}

var FfiConverterWalletTransactionINSTANCE = FfiConverterWalletTransaction{}

func (c FfiConverterWalletTransaction) Lift(rb RustBufferI) WalletTransaction {
	return LiftFromRustBuffer[WalletTransaction](c, rb)
}

func (c FfiConverterWalletTransaction) Read(reader io.Reader) WalletTransaction {
	return WalletTransaction{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalUint64INSTANCE.Read(reader),
		FfiConverterInt64INSTANCE.Read(reader),
		FfiConverterOptionalBlockRefINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
	}
}

func (c FfiConverterWalletTransaction) Lower(value WalletTransaction) C.RustBuffer {
	return LowerIntoRustBuffer[WalletTransaction](c, value)
}

func (c FfiConverterWalletTransaction) LowerExternal(value WalletTransaction) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[WalletTransaction](c, value))
}

func (c FfiConverterWalletTransaction) Write(writer io.Writer, value WalletTransaction) {
	FfiConverterStringINSTANCE.Write(writer, value.Txid)
	FfiConverterStringINSTANCE.Write(writer, value.TxHex)
	FfiConverterOptionalUint64INSTANCE.Write(writer, value.OnchainFeeSats)
	FfiConverterInt64INSTANCE.Write(writer, value.BalanceChangeSats)
	FfiConverterOptionalBlockRefINSTANCE.Write(writer, value.Confirmation)
	FfiConverterBoolINSTANCE.Write(writer, value.IsCpfp)
}

type FfiDestroyerWalletTransaction struct{}

func (_ FfiDestroyerWalletTransaction) Destroy(value WalletTransaction) {
	value.Destroy()
}

// The single error type surfaced across the Bark FFI.
//
// It is a thin, single-variant wrapper around [`anyhow::Error`]. uniffi error
// types must be enums (callback interfaces require `ConvertError`, which
// objects cannot provide), and `flat_error` tells uniffi to carry only the
// `Display` string across the boundary — so the rich anyhow context collapses
// to a single message on the foreign side.
type Error struct {
	err error
}

// Convenience method to turn *Error into error
// Avoiding treating nil pointer as non nil error interface
func (err *Error) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err Error) Error() string {
	return fmt.Sprintf("Error: %s", err.err.Error())
}

func (err Error) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrErrorInner = fmt.Errorf("ErrorInner")

// Variant structs
type ErrorInner struct {
	message string
}

func NewErrorInner() *Error {
	return &Error{err: &ErrorInner{}}
}

func (e ErrorInner) destroy() {
}

func (err ErrorInner) Error() string {
	return fmt.Sprintf("Inner: %s", err.message)
}

func (self ErrorInner) Is(target error) bool {
	return target == ErrErrorInner
}

type FfiConverterError struct{}

var FfiConverterErrorINSTANCE = FfiConverterError{}

func (c FfiConverterError) Lift(eb RustBufferI) *Error {
	return LiftFromRustBuffer[*Error](c, eb)
}

func (c FfiConverterError) Lower(value *Error) C.RustBuffer {
	return LowerIntoRustBuffer[*Error](c, value)
}

func (c FfiConverterError) LowerExternal(value *Error) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*Error](c, value))
}

func (c FfiConverterError) Read(reader io.Reader) *Error {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &Error{&ErrorInner{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterError.Read()", errorID))
	}

}

func (c FfiConverterError) Write(writer io.Writer, value *Error) {
	switch variantValue := value.err.(type) {
	case *ErrorInner:
		writeInt32(writer, 1)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterError.Write", value))
	}
}

type FfiDestroyerError struct{}

func (_ FfiDestroyerError) Destroy(value *Error) {
	switch variantValue := value.err.(type) {
	case ErrorInner:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerError.Destroy", value))
	}
}

// Why a [`Wallet::cancel_exit`](crate::core::Wallet::cancel_exit) did not
// cancel the exit.
//
// These are the *expected* negative outcomes of asking to cancel, not faults:
// each is a normal state the exit can legitimately be in. Genuine failures
// (database, chain source unreachable) still surface as `Err`.
type ExitCancelFailure interface {
	Destroy()
}

// No exit was ever started for this VTXO.
type ExitCancelFailureNotExiting struct {
}

func (e ExitCancelFailureNotExiting) Destroy() {
}

// The exit has progressed past its abortable window. `state` is the
// state that blocked the cancellation.
type ExitCancelFailureTooLate struct {
	State ExitStateKind
}

func (e ExitCancelFailureTooLate) Destroy() {
	FfiDestroyerExitStateKind{}.Destroy(e.State)
}

// The final exit transaction is already in the mempool or a block, so
// the exit can no longer be called off.
type ExitCancelFailureAlreadyBroadcast struct {
	Txid string
}

func (e ExitCancelFailureAlreadyBroadcast) Destroy() {
	FfiDestroyerString{}.Destroy(e.Txid)
}

type FfiConverterExitCancelFailure struct{}

var FfiConverterExitCancelFailureINSTANCE = FfiConverterExitCancelFailure{}

func (c FfiConverterExitCancelFailure) Lift(rb RustBufferI) ExitCancelFailure {
	return LiftFromRustBuffer[ExitCancelFailure](c, rb)
}

func (c FfiConverterExitCancelFailure) Lower(value ExitCancelFailure) C.RustBuffer {
	return LowerIntoRustBuffer[ExitCancelFailure](c, value)
}

func (c FfiConverterExitCancelFailure) LowerExternal(value ExitCancelFailure) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitCancelFailure](c, value))
}
func (FfiConverterExitCancelFailure) Read(reader io.Reader) ExitCancelFailure {
	id := readInt32(reader)
	switch id {
	case 1:
		return ExitCancelFailureNotExiting{}
	case 2:
		return ExitCancelFailureTooLate{
			FfiConverterExitStateKindINSTANCE.Read(reader),
		}
	case 3:
		return ExitCancelFailureAlreadyBroadcast{
			FfiConverterStringINSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterExitCancelFailure.Read()", id))
	}
}

func (FfiConverterExitCancelFailure) Write(writer io.Writer, value ExitCancelFailure) {
	switch variant_value := value.(type) {
	case ExitCancelFailureNotExiting:
		writeInt32(writer, 1)
	case ExitCancelFailureTooLate:
		writeInt32(writer, 2)
		FfiConverterExitStateKindINSTANCE.Write(writer, variant_value.State)
	case ExitCancelFailureAlreadyBroadcast:
		writeInt32(writer, 3)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Txid)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterExitCancelFailure.Write", value))
	}
}

type FfiDestroyerExitCancelFailure struct{}

func (_ FfiDestroyerExitCancelFailure) Destroy(value ExitCancelFailure) {
	value.Destroy()
}

// State of a unilateral exit, mirroring `bark::exit::ExitState`.
//
// Serde/TS tags match upstream's kebab-case serialization (`"start"`,
// `"processing"`, `"awaiting-delta"`, `"claimable"`, `"claim-in-progress"`,
// `"claimed"`, `"vtxo-already-spent"`, `"canceled"`), with camelCase fields.
type ExitState interface {
	Destroy()
}

// The exit was requested at the given tip.
type ExitStateStart struct {
	TipHeight uint32
}

func (e ExitStateStart) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
}

// The exit transaction chain is being broadcast and confirmed.
type ExitStateProcessing struct {
	TipHeight    uint32
	Transactions []ExitTx
}

func (e ExitStateProcessing) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
	FfiDestroyerSequenceExitTx{}.Destroy(e.Transactions)
}

// Fully confirmed; waiting out the exit delta until claimable.
type ExitStateAwaitingDelta struct {
	TipHeight       uint32
	ConfirmedBlock  BlockRef
	ClaimableHeight uint32
}

func (e ExitStateAwaitingDelta) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
	FfiDestroyerBlockRef{}.Destroy(e.ConfirmedBlock)
	FfiDestroyerUint32{}.Destroy(e.ClaimableHeight)
}

// The exit output can be claimed.
type ExitStateClaimable struct {
	TipHeight        uint32
	ClaimableSince   BlockRef
	LastScannedBlock *BlockRef
}

func (e ExitStateClaimable) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
	FfiDestroyerBlockRef{}.Destroy(e.ClaimableSince)
	FfiDestroyerOptionalBlockRef{}.Destroy(e.LastScannedBlock)
}

// A claim transaction has been broadcast.
type ExitStateClaimInProgress struct {
	TipHeight      uint32
	ClaimableSince BlockRef
	ClaimTxid      string
}

func (e ExitStateClaimInProgress) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
	FfiDestroyerBlockRef{}.Destroy(e.ClaimableSince)
	FfiDestroyerString{}.Destroy(e.ClaimTxid)
}

// Terminal: the exit output was claimed (or spent deeper in the tree).
type ExitStateClaimed struct {
	TipHeight uint32
	Txid      string
	Block     BlockRef
}

func (e ExitStateClaimed) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
	FfiDestroyerString{}.Destroy(e.Txid)
	FfiDestroyerBlockRef{}.Destroy(e.Block)
}

// Terminal: the VTXO was already spent offchain, so the exit cannot
// proceed.
type ExitStateVtxoAlreadySpent struct {
	TipHeight uint32
}

func (e ExitStateVtxoAlreadySpent) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
}

// Resumable: the user canceled the exit before its final transaction
// was broadcast. The VTXO stays spendable.
type ExitStateCanceled struct {
	TipHeight uint32
}

func (e ExitStateCanceled) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.TipHeight)
}

type FfiConverterExitState struct{}

var FfiConverterExitStateINSTANCE = FfiConverterExitState{}

func (c FfiConverterExitState) Lift(rb RustBufferI) ExitState {
	return LiftFromRustBuffer[ExitState](c, rb)
}

func (c FfiConverterExitState) Lower(value ExitState) C.RustBuffer {
	return LowerIntoRustBuffer[ExitState](c, value)
}

func (c FfiConverterExitState) LowerExternal(value ExitState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitState](c, value))
}
func (FfiConverterExitState) Read(reader io.Reader) ExitState {
	id := readInt32(reader)
	switch id {
	case 1:
		return ExitStateStart{
			FfiConverterUint32INSTANCE.Read(reader),
		}
	case 2:
		return ExitStateProcessing{
			FfiConverterUint32INSTANCE.Read(reader),
			FfiConverterSequenceExitTxINSTANCE.Read(reader),
		}
	case 3:
		return ExitStateAwaitingDelta{
			FfiConverterUint32INSTANCE.Read(reader),
			FfiConverterBlockRefINSTANCE.Read(reader),
			FfiConverterUint32INSTANCE.Read(reader),
		}
	case 4:
		return ExitStateClaimable{
			FfiConverterUint32INSTANCE.Read(reader),
			FfiConverterBlockRefINSTANCE.Read(reader),
			FfiConverterOptionalBlockRefINSTANCE.Read(reader),
		}
	case 5:
		return ExitStateClaimInProgress{
			FfiConverterUint32INSTANCE.Read(reader),
			FfiConverterBlockRefINSTANCE.Read(reader),
			FfiConverterStringINSTANCE.Read(reader),
		}
	case 6:
		return ExitStateClaimed{
			FfiConverterUint32INSTANCE.Read(reader),
			FfiConverterStringINSTANCE.Read(reader),
			FfiConverterBlockRefINSTANCE.Read(reader),
		}
	case 7:
		return ExitStateVtxoAlreadySpent{
			FfiConverterUint32INSTANCE.Read(reader),
		}
	case 8:
		return ExitStateCanceled{
			FfiConverterUint32INSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterExitState.Read()", id))
	}
}

func (FfiConverterExitState) Write(writer io.Writer, value ExitState) {
	switch variant_value := value.(type) {
	case ExitStateStart:
		writeInt32(writer, 1)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
	case ExitStateProcessing:
		writeInt32(writer, 2)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
		FfiConverterSequenceExitTxINSTANCE.Write(writer, variant_value.Transactions)
	case ExitStateAwaitingDelta:
		writeInt32(writer, 3)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
		FfiConverterBlockRefINSTANCE.Write(writer, variant_value.ConfirmedBlock)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.ClaimableHeight)
	case ExitStateClaimable:
		writeInt32(writer, 4)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
		FfiConverterBlockRefINSTANCE.Write(writer, variant_value.ClaimableSince)
		FfiConverterOptionalBlockRefINSTANCE.Write(writer, variant_value.LastScannedBlock)
	case ExitStateClaimInProgress:
		writeInt32(writer, 5)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
		FfiConverterBlockRefINSTANCE.Write(writer, variant_value.ClaimableSince)
		FfiConverterStringINSTANCE.Write(writer, variant_value.ClaimTxid)
	case ExitStateClaimed:
		writeInt32(writer, 6)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Txid)
		FfiConverterBlockRefINSTANCE.Write(writer, variant_value.Block)
	case ExitStateVtxoAlreadySpent:
		writeInt32(writer, 7)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
	case ExitStateCanceled:
		writeInt32(writer, 8)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.TipHeight)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterExitState.Write", value))
	}
}

type FfiDestroyerExitState struct{}

func (_ FfiDestroyerExitState) Destroy(value ExitState) {
	value.Destroy()
}

// Lightweight discriminator for [`ExitState`], mirroring
// `bark::exit::ExitStateKind`.
//
// Carries no payload — use it where only the variant matters (e.g. reporting
// which state blocked a cancellation). Serde/TS tags match upstream's
// kebab-case serialization.
type ExitStateKind uint

const (
	ExitStateKindStart            ExitStateKind = 1
	ExitStateKindProcessing       ExitStateKind = 2
	ExitStateKindAwaitingDelta    ExitStateKind = 3
	ExitStateKindClaimable        ExitStateKind = 4
	ExitStateKindClaimInProgress  ExitStateKind = 5
	ExitStateKindClaimed          ExitStateKind = 6
	ExitStateKindVtxoAlreadySpent ExitStateKind = 7
	ExitStateKindCanceled         ExitStateKind = 8
)

type FfiConverterExitStateKind struct{}

var FfiConverterExitStateKindINSTANCE = FfiConverterExitStateKind{}

func (c FfiConverterExitStateKind) Lift(rb RustBufferI) ExitStateKind {
	return LiftFromRustBuffer[ExitStateKind](c, rb)
}

func (c FfiConverterExitStateKind) Lower(value ExitStateKind) C.RustBuffer {
	return LowerIntoRustBuffer[ExitStateKind](c, value)
}

func (c FfiConverterExitStateKind) LowerExternal(value ExitStateKind) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitStateKind](c, value))
}
func (FfiConverterExitStateKind) Read(reader io.Reader) ExitStateKind {
	id := readInt32(reader)
	return ExitStateKind(id)
}

func (FfiConverterExitStateKind) Write(writer io.Writer, value ExitStateKind) {
	writeInt32(writer, int32(value))
}

type FfiDestroyerExitStateKind struct{}

func (_ FfiDestroyerExitStateKind) Destroy(value ExitStateKind) {
}

// Where an exit transaction was first seen, mirroring `bark::exit::ExitTxOrigin`.
type ExitTxOrigin interface {
	Destroy()
}

// Broadcast by this wallet.
type ExitTxOriginWallet struct {
	ConfirmedIn *BlockRef
}

func (e ExitTxOriginWallet) Destroy() {
	FfiDestroyerOptionalBlockRef{}.Destroy(e.ConfirmedIn)
}

// Seen in the mempool.
type ExitTxOriginMempool struct {
}

func (e ExitTxOriginMempool) Destroy() {
}

// Seen confirmed in a block.
type ExitTxOriginBlock struct {
	ConfirmedIn BlockRef
}

func (e ExitTxOriginBlock) Destroy() {
	FfiDestroyerBlockRef{}.Destroy(e.ConfirmedIn)
}

type FfiConverterExitTxOrigin struct{}

var FfiConverterExitTxOriginINSTANCE = FfiConverterExitTxOrigin{}

func (c FfiConverterExitTxOrigin) Lift(rb RustBufferI) ExitTxOrigin {
	return LiftFromRustBuffer[ExitTxOrigin](c, rb)
}

func (c FfiConverterExitTxOrigin) Lower(value ExitTxOrigin) C.RustBuffer {
	return LowerIntoRustBuffer[ExitTxOrigin](c, value)
}

func (c FfiConverterExitTxOrigin) LowerExternal(value ExitTxOrigin) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitTxOrigin](c, value))
}
func (FfiConverterExitTxOrigin) Read(reader io.Reader) ExitTxOrigin {
	id := readInt32(reader)
	switch id {
	case 1:
		return ExitTxOriginWallet{
			FfiConverterOptionalBlockRefINSTANCE.Read(reader),
		}
	case 2:
		return ExitTxOriginMempool{}
	case 3:
		return ExitTxOriginBlock{
			FfiConverterBlockRefINSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterExitTxOrigin.Read()", id))
	}
}

func (FfiConverterExitTxOrigin) Write(writer io.Writer, value ExitTxOrigin) {
	switch variant_value := value.(type) {
	case ExitTxOriginWallet:
		writeInt32(writer, 1)
		FfiConverterOptionalBlockRefINSTANCE.Write(writer, variant_value.ConfirmedIn)
	case ExitTxOriginMempool:
		writeInt32(writer, 2)
	case ExitTxOriginBlock:
		writeInt32(writer, 3)
		FfiConverterBlockRefINSTANCE.Write(writer, variant_value.ConfirmedIn)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterExitTxOrigin.Write", value))
	}
}

type FfiDestroyerExitTxOrigin struct{}

func (_ FfiDestroyerExitTxOrigin) Destroy(value ExitTxOrigin) {
	value.Destroy()
}

// Broadcast/confirmation status of one transaction in an exit chain,
// mirroring `bark::exit::ExitTxStatus`.
type ExitTxStatus interface {
	Destroy()
}

// Inputs are still being verified.
type ExitTxStatusVerifyInputs struct {
}

func (e ExitTxStatusVerifyInputs) Destroy() {
}

// Waiting for the given input txids to confirm. Sorted for a stable
// order (upstream keeps them in a set).
type ExitTxStatusAwaitingInputConfirmation struct {
	Txids []string
}

func (e ExitTxStatusAwaitingInputConfirmation) Destroy() {
	FfiDestroyerSequenceString{}.Destroy(e.Txids)
}

// Ready for its CPFP child to be broadcast.
type ExitTxStatusAwaitingCpfpBroadcast struct {
}

func (e ExitTxStatusAwaitingCpfpBroadcast) Destroy() {
}

// CPFP child broadcast; waiting for confirmation.
type ExitTxStatusAwaitingConfirmation struct {
	ChildTxid string
	Origin    ExitTxOrigin
}

func (e ExitTxStatusAwaitingConfirmation) Destroy() {
	FfiDestroyerString{}.Destroy(e.ChildTxid)
	FfiDestroyerExitTxOrigin{}.Destroy(e.Origin)
}

// Confirmed in a block.
type ExitTxStatusConfirmed struct {
	ChildTxid string
	Block     BlockRef
	Origin    ExitTxOrigin
}

func (e ExitTxStatusConfirmed) Destroy() {
	FfiDestroyerString{}.Destroy(e.ChildTxid)
	FfiDestroyerBlockRef{}.Destroy(e.Block)
	FfiDestroyerExitTxOrigin{}.Destroy(e.Origin)
}

type FfiConverterExitTxStatus struct{}

var FfiConverterExitTxStatusINSTANCE = FfiConverterExitTxStatus{}

func (c FfiConverterExitTxStatus) Lift(rb RustBufferI) ExitTxStatus {
	return LiftFromRustBuffer[ExitTxStatus](c, rb)
}

func (c FfiConverterExitTxStatus) Lower(value ExitTxStatus) C.RustBuffer {
	return LowerIntoRustBuffer[ExitTxStatus](c, value)
}

func (c FfiConverterExitTxStatus) LowerExternal(value ExitTxStatus) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[ExitTxStatus](c, value))
}
func (FfiConverterExitTxStatus) Read(reader io.Reader) ExitTxStatus {
	id := readInt32(reader)
	switch id {
	case 1:
		return ExitTxStatusVerifyInputs{}
	case 2:
		return ExitTxStatusAwaitingInputConfirmation{
			FfiConverterSequenceStringINSTANCE.Read(reader),
		}
	case 3:
		return ExitTxStatusAwaitingCpfpBroadcast{}
	case 4:
		return ExitTxStatusAwaitingConfirmation{
			FfiConverterStringINSTANCE.Read(reader),
			FfiConverterExitTxOriginINSTANCE.Read(reader),
		}
	case 5:
		return ExitTxStatusConfirmed{
			FfiConverterStringINSTANCE.Read(reader),
			FfiConverterBlockRefINSTANCE.Read(reader),
			FfiConverterExitTxOriginINSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterExitTxStatus.Read()", id))
	}
}

func (FfiConverterExitTxStatus) Write(writer io.Writer, value ExitTxStatus) {
	switch variant_value := value.(type) {
	case ExitTxStatusVerifyInputs:
		writeInt32(writer, 1)
	case ExitTxStatusAwaitingInputConfirmation:
		writeInt32(writer, 2)
		FfiConverterSequenceStringINSTANCE.Write(writer, variant_value.Txids)
	case ExitTxStatusAwaitingCpfpBroadcast:
		writeInt32(writer, 3)
	case ExitTxStatusAwaitingConfirmation:
		writeInt32(writer, 4)
		FfiConverterStringINSTANCE.Write(writer, variant_value.ChildTxid)
		FfiConverterExitTxOriginINSTANCE.Write(writer, variant_value.Origin)
	case ExitTxStatusConfirmed:
		writeInt32(writer, 5)
		FfiConverterStringINSTANCE.Write(writer, variant_value.ChildTxid)
		FfiConverterBlockRefINSTANCE.Write(writer, variant_value.Block)
		FfiConverterExitTxOriginINSTANCE.Write(writer, variant_value.Origin)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterExitTxStatus.Write", value))
	}
}

type FfiDestroyerExitTxStatus struct{}

func (_ FfiDestroyerExitTxStatus) Destroy(value ExitTxStatus) {
	value.Destroy()
}

// Terminal/in-flight state of an outgoing lightning send.
//
// Mirrors `bark`'s `LightningSendState`. `pay_lightning_*` and
// `check_lightning_payment` now return this so callers can drive the
// crash-safe send flow themselves (initiate with `wait = false`, then poll).
type LightningSendStatus interface {
	Destroy()
}

// No record of this payment (never started, or already pruned).
type LightningSendStatusUnknown struct {
}

func (e LightningSendStatusUnknown) Destroy() {
}

// Send is in flight; HTLC VTXOs are locked.
type LightningSendStatusInProgress struct {
	Send LightningSend
}

func (e LightningSendStatusInProgress) Destroy() {
	FfiDestroyerLightningSend{}.Destroy(e.Send)
}

// Send has settled; the preimage proves payment.
type LightningSendStatusPaid struct {
	PaymentHash string
	Preimage    string
}

func (e LightningSendStatusPaid) Destroy() {
	FfiDestroyerString{}.Destroy(e.PaymentHash)
	FfiDestroyerString{}.Destroy(e.Preimage)
}

type FfiConverterLightningSendStatus struct{}

var FfiConverterLightningSendStatusINSTANCE = FfiConverterLightningSendStatus{}

func (c FfiConverterLightningSendStatus) Lift(rb RustBufferI) LightningSendStatus {
	return LiftFromRustBuffer[LightningSendStatus](c, rb)
}

func (c FfiConverterLightningSendStatus) Lower(value LightningSendStatus) C.RustBuffer {
	return LowerIntoRustBuffer[LightningSendStatus](c, value)
}

func (c FfiConverterLightningSendStatus) LowerExternal(value LightningSendStatus) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[LightningSendStatus](c, value))
}
func (FfiConverterLightningSendStatus) Read(reader io.Reader) LightningSendStatus {
	id := readInt32(reader)
	switch id {
	case 1:
		return LightningSendStatusUnknown{}
	case 2:
		return LightningSendStatusInProgress{
			FfiConverterLightningSendINSTANCE.Read(reader),
		}
	case 3:
		return LightningSendStatusPaid{
			FfiConverterStringINSTANCE.Read(reader),
			FfiConverterStringINSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterLightningSendStatus.Read()", id))
	}
}

func (FfiConverterLightningSendStatus) Write(writer io.Writer, value LightningSendStatus) {
	switch variant_value := value.(type) {
	case LightningSendStatusUnknown:
		writeInt32(writer, 1)
	case LightningSendStatusInProgress:
		writeInt32(writer, 2)
		FfiConverterLightningSendINSTANCE.Write(writer, variant_value.Send)
	case LightningSendStatusPaid:
		writeInt32(writer, 3)
		FfiConverterStringINSTANCE.Write(writer, variant_value.PaymentHash)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Preimage)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterLightningSendStatus.Write", value))
	}
}

type FfiDestroyerLightningSendStatus struct{}

func (_ FfiDestroyerLightningSendStatus) Destroy(value LightningSendStatus) {
	value.Destroy()
}

// Severity of a log record.
type LogLevel uint

const (
	LogLevelError LogLevel = 1
	LogLevelWarn  LogLevel = 2
	LogLevelInfo  LogLevel = 3
	LogLevelDebug LogLevel = 4
	LogLevelTrace LogLevel = 5
)

type FfiConverterLogLevel struct{}

var FfiConverterLogLevelINSTANCE = FfiConverterLogLevel{}

func (c FfiConverterLogLevel) Lift(rb RustBufferI) LogLevel {
	return LiftFromRustBuffer[LogLevel](c, rb)
}

func (c FfiConverterLogLevel) Lower(value LogLevel) C.RustBuffer {
	return LowerIntoRustBuffer[LogLevel](c, value)
}

func (c FfiConverterLogLevel) LowerExternal(value LogLevel) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[LogLevel](c, value))
}
func (FfiConverterLogLevel) Read(reader io.Reader) LogLevel {
	id := readInt32(reader)
	return LogLevel(id)
}

func (FfiConverterLogLevel) Write(writer io.Writer, value LogLevel) {
	writeInt32(writer, int32(value))
}

type FfiDestroyerLogLevel struct{}

func (_ FfiDestroyerLogLevel) Destroy(value LogLevel) {
}

type Network uint

const (
	NetworkBitcoin Network = 1
	NetworkTestnet Network = 2
	NetworkSignet  Network = 3
	NetworkRegtest Network = 4
)

type FfiConverterNetwork struct{}

var FfiConverterNetworkINSTANCE = FfiConverterNetwork{}

func (c FfiConverterNetwork) Lift(rb RustBufferI) Network {
	return LiftFromRustBuffer[Network](c, rb)
}

func (c FfiConverterNetwork) Lower(value Network) C.RustBuffer {
	return LowerIntoRustBuffer[Network](c, value)
}

func (c FfiConverterNetwork) LowerExternal(value Network) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Network](c, value))
}
func (FfiConverterNetwork) Read(reader io.Reader) Network {
	id := readInt32(reader)
	return Network(id)
}

func (FfiConverterNetwork) Write(writer io.Writer, value Network) {
	writeInt32(writer, int32(value))
}

type FfiDestroyerNetwork struct{}

func (_ FfiDestroyerNetwork) Destroy(value Network) {
}

// An onchain UTXO known to the wallet, mirroring `bark::onchain::Utxo`.
type OnchainUtxo interface {
	Destroy()
}

// A standard wallet UTXO.
type OnchainUtxoLocal struct {
	Outpoint           OutPoint
	AmountSats         uint64
	ConfirmationHeight *uint32
}

func (e OnchainUtxoLocal) Destroy() {
	FfiDestroyerOutPoint{}.Destroy(e.Outpoint)
	FfiDestroyerUint64{}.Destroy(e.AmountSats)
	FfiDestroyerOptionalUint32{}.Destroy(e.ConfirmationHeight)
}

// A spendable unilateral-exit output claimed from a VTXO.
type OnchainUtxoExit struct {
	VtxoId     string
	AmountSats uint64
	Height     uint32
}

func (e OnchainUtxoExit) Destroy() {
	FfiDestroyerString{}.Destroy(e.VtxoId)
	FfiDestroyerUint64{}.Destroy(e.AmountSats)
	FfiDestroyerUint32{}.Destroy(e.Height)
}

type FfiConverterOnchainUtxo struct{}

var FfiConverterOnchainUtxoINSTANCE = FfiConverterOnchainUtxo{}

func (c FfiConverterOnchainUtxo) Lift(rb RustBufferI) OnchainUtxo {
	return LiftFromRustBuffer[OnchainUtxo](c, rb)
}

func (c FfiConverterOnchainUtxo) Lower(value OnchainUtxo) C.RustBuffer {
	return LowerIntoRustBuffer[OnchainUtxo](c, value)
}

func (c FfiConverterOnchainUtxo) LowerExternal(value OnchainUtxo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[OnchainUtxo](c, value))
}
func (FfiConverterOnchainUtxo) Read(reader io.Reader) OnchainUtxo {
	id := readInt32(reader)
	switch id {
	case 1:
		return OnchainUtxoLocal{
			FfiConverterOutPointINSTANCE.Read(reader),
			FfiConverterUint64INSTANCE.Read(reader),
			FfiConverterOptionalUint32INSTANCE.Read(reader),
		}
	case 2:
		return OnchainUtxoExit{
			FfiConverterStringINSTANCE.Read(reader),
			FfiConverterUint64INSTANCE.Read(reader),
			FfiConverterUint32INSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterOnchainUtxo.Read()", id))
	}
}

func (FfiConverterOnchainUtxo) Write(writer io.Writer, value OnchainUtxo) {
	switch variant_value := value.(type) {
	case OnchainUtxoLocal:
		writeInt32(writer, 1)
		FfiConverterOutPointINSTANCE.Write(writer, variant_value.Outpoint)
		FfiConverterUint64INSTANCE.Write(writer, variant_value.AmountSats)
		FfiConverterOptionalUint32INSTANCE.Write(writer, variant_value.ConfirmationHeight)
	case OnchainUtxoExit:
		writeInt32(writer, 2)
		FfiConverterStringINSTANCE.Write(writer, variant_value.VtxoId)
		FfiConverterUint64INSTANCE.Write(writer, variant_value.AmountSats)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.Height)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterOnchainUtxo.Write", value))
	}
}

type FfiDestroyerOnchainUtxo struct{}

func (_ FfiDestroyerOnchainUtxo) Destroy(value OnchainUtxo) {
	value.Destroy()
}

// Outcome of the seed-recovery scan `Wallet::open` runs, mirroring
// `bark::RecoveryStatus`.
//
// The three variants are distinguishable on purpose: `NotRun` means nothing
// was attempted (wallet already existed locally, or `skip_recovery` was set),
// `Failed` means the scan errored before producing a report so funds may be
// missing until a retry succeeds, and `Completed` carries the report — whose
// `is_complete` can still be `false` for individual VTXOs.
//
// Serde/TS tags: `"not-run"` | `"failed"` | `"completed"`.
type RecoveryStatus interface {
	Destroy()
}

// No scan was attempted.
type RecoveryStatusNotRun struct {
}

func (e RecoveryStatusNotRun) Destroy() {
}

// The scan errored before it could produce a report.
type RecoveryStatusFailed struct {
	Message string
}

func (e RecoveryStatusFailed) Destroy() {
	FfiDestroyerString{}.Destroy(e.Message)
}

// The scan ran to the end.
type RecoveryStatusCompleted struct {
	Report RecoveryReport
}

func (e RecoveryStatusCompleted) Destroy() {
	FfiDestroyerRecoveryReport{}.Destroy(e.Report)
}

type FfiConverterRecoveryStatus struct{}

var FfiConverterRecoveryStatusINSTANCE = FfiConverterRecoveryStatus{}

func (c FfiConverterRecoveryStatus) Lift(rb RustBufferI) RecoveryStatus {
	return LiftFromRustBuffer[RecoveryStatus](c, rb)
}

func (c FfiConverterRecoveryStatus) Lower(value RecoveryStatus) C.RustBuffer {
	return LowerIntoRustBuffer[RecoveryStatus](c, value)
}

func (c FfiConverterRecoveryStatus) LowerExternal(value RecoveryStatus) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[RecoveryStatus](c, value))
}
func (FfiConverterRecoveryStatus) Read(reader io.Reader) RecoveryStatus {
	id := readInt32(reader)
	switch id {
	case 1:
		return RecoveryStatusNotRun{}
	case 2:
		return RecoveryStatusFailed{
			FfiConverterStringINSTANCE.Read(reader),
		}
	case 3:
		return RecoveryStatusCompleted{
			FfiConverterRecoveryReportINSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterRecoveryStatus.Read()", id))
	}
}

func (FfiConverterRecoveryStatus) Write(writer io.Writer, value RecoveryStatus) {
	switch variant_value := value.(type) {
	case RecoveryStatusNotRun:
		writeInt32(writer, 1)
	case RecoveryStatusFailed:
		writeInt32(writer, 2)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Message)
	case RecoveryStatusCompleted:
		writeInt32(writer, 3)
		FfiConverterRecoveryReportINSTANCE.Write(writer, variant_value.Report)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterRecoveryStatus.Write", value))
	}
}

type FfiDestroyerRecoveryStatus struct{}

func (_ FfiDestroyerRecoveryStatus) Destroy(value RecoveryStatus) {
	value.Destroy()
}

// Lifecycle phase of a round participation, mirroring
// `bark::round::RoundFlowKind`.
//
// Serde/TS tags match upstream's `Display` form: `"delegated-pending"` |
// `"pending"` | `"ongoing"` | `"awaiting-confirmations"` | `"failed"` |
// `"canceled"`.
type RoundFlowKind uint

const (
	// Delegated participation waiting for its scheduled round. See
	// [`RoundState::scheduled_height`].
	RoundFlowKindDelegatedPending RoundFlowKind = 1
	// Interactive participation waiting for its round.
	RoundFlowKindPending RoundFlowKind = 2
	// The interactive part is being played out with the server.
	RoundFlowKindOngoing RoundFlowKind = 3
	// The round finished; waiting for its funding tx to confirm.
	RoundFlowKindAwaitingConfirmations RoundFlowKind = 4
	// The participation failed.
	RoundFlowKindFailed RoundFlowKind = 5
	// The user canceled the participation.
	RoundFlowKindCanceled RoundFlowKind = 6
)

type FfiConverterRoundFlowKind struct{}

var FfiConverterRoundFlowKindINSTANCE = FfiConverterRoundFlowKind{}

func (c FfiConverterRoundFlowKind) Lift(rb RustBufferI) RoundFlowKind {
	return LiftFromRustBuffer[RoundFlowKind](c, rb)
}

func (c FfiConverterRoundFlowKind) Lower(value RoundFlowKind) C.RustBuffer {
	return LowerIntoRustBuffer[RoundFlowKind](c, value)
}

func (c FfiConverterRoundFlowKind) LowerExternal(value RoundFlowKind) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[RoundFlowKind](c, value))
}
func (FfiConverterRoundFlowKind) Read(reader io.Reader) RoundFlowKind {
	id := readInt32(reader)
	return RoundFlowKind(id)
}

func (FfiConverterRoundFlowKind) Write(writer io.Writer, value RoundFlowKind) {
	writeInt32(writer, int32(value))
}

type FfiDestroyerRoundFlowKind struct{}

func (_ FfiDestroyerRoundFlowKind) Destroy(value RoundFlowKind) {
}

// Who holds the lock on a [`VtxoState::Locked`] VTXO.
//
// Mirrors `bark::vtxo::VtxoLockHolder`. Action-based subsystems lock with
// `Action`; pre-action subsystems (round, offboard, board, lightning
// receive) lock with `Movement`. As upstream converts subsystems to
// actions, new locks migrate from `Movement` to `Action` per-subsystem.
//
// Serde/TS tags match upstream's serialization: `"action"` / `"movement"`.
type VtxoLockHolder interface {
	Destroy()
}

// A wallet action checkpoint. `id` is upstream's `WalletActionId`
// (an opaque string).
type VtxoLockHolderAction struct {
	Id string
}

func (e VtxoLockHolderAction) Destroy() {
	FfiDestroyerString{}.Destroy(e.Id)
}

// A pre-action subsystem, keyed by its movement.
type VtxoLockHolderMovement struct {
	Id uint32
}

func (e VtxoLockHolderMovement) Destroy() {
	FfiDestroyerUint32{}.Destroy(e.Id)
}

type FfiConverterVtxoLockHolder struct{}

var FfiConverterVtxoLockHolderINSTANCE = FfiConverterVtxoLockHolder{}

func (c FfiConverterVtxoLockHolder) Lift(rb RustBufferI) VtxoLockHolder {
	return LiftFromRustBuffer[VtxoLockHolder](c, rb)
}

func (c FfiConverterVtxoLockHolder) Lower(value VtxoLockHolder) C.RustBuffer {
	return LowerIntoRustBuffer[VtxoLockHolder](c, value)
}

func (c FfiConverterVtxoLockHolder) LowerExternal(value VtxoLockHolder) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[VtxoLockHolder](c, value))
}
func (FfiConverterVtxoLockHolder) Read(reader io.Reader) VtxoLockHolder {
	id := readInt32(reader)
	switch id {
	case 1:
		return VtxoLockHolderAction{
			FfiConverterStringINSTANCE.Read(reader),
		}
	case 2:
		return VtxoLockHolderMovement{
			FfiConverterUint32INSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterVtxoLockHolder.Read()", id))
	}
}

func (FfiConverterVtxoLockHolder) Write(writer io.Writer, value VtxoLockHolder) {
	switch variant_value := value.(type) {
	case VtxoLockHolderAction:
		writeInt32(writer, 1)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Id)
	case VtxoLockHolderMovement:
		writeInt32(writer, 2)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.Id)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterVtxoLockHolder.Write", value))
	}
}

type FfiDestroyerVtxoLockHolder struct{}

func (_ FfiDestroyerVtxoLockHolder) Destroy(value VtxoLockHolder) {
	value.Destroy()
}

// Rich VTXO state, mirroring `bark::vtxo::VtxoState`.
//
// Serde/TS tags match upstream's kebab-case serialization:
// `"spendable"` | `"locked"` | `"spent"` | `"exited"`.
type VtxoState interface {
	Destroy()
}

// Available; can be spent in a future round.
type VtxoStateSpendable struct {
}

func (e VtxoStateSpendable) Destroy() {
}

// Locked by an operation. `holder` is `None` only for the narrow
// window between creating a fresh locked VTXO and pinning it to a
// specific operation, so a locked VTXO can legitimately carry no
// lock reason yet.
type VtxoStateLocked struct {
	Holder *VtxoLockHolder
}

func (e VtxoStateLocked) Destroy() {
	FfiDestroyerOptionalVtxoLockHolder{}.Destroy(e.Holder)
}

// Consumed.
type VtxoStateSpent struct {
}

func (e VtxoStateSpent) Destroy() {
}

// In (or completed) a unilateral exit.
type VtxoStateExited struct {
}

func (e VtxoStateExited) Destroy() {
}

type FfiConverterVtxoState struct{}

var FfiConverterVtxoStateINSTANCE = FfiConverterVtxoState{}

func (c FfiConverterVtxoState) Lift(rb RustBufferI) VtxoState {
	return LiftFromRustBuffer[VtxoState](c, rb)
}

func (c FfiConverterVtxoState) Lower(value VtxoState) C.RustBuffer {
	return LowerIntoRustBuffer[VtxoState](c, value)
}

func (c FfiConverterVtxoState) LowerExternal(value VtxoState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[VtxoState](c, value))
}
func (FfiConverterVtxoState) Read(reader io.Reader) VtxoState {
	id := readInt32(reader)
	switch id {
	case 1:
		return VtxoStateSpendable{}
	case 2:
		return VtxoStateLocked{
			FfiConverterOptionalVtxoLockHolderINSTANCE.Read(reader),
		}
	case 3:
		return VtxoStateSpent{}
	case 4:
		return VtxoStateExited{}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterVtxoState.Read()", id))
	}
}

func (FfiConverterVtxoState) Write(writer io.Writer, value VtxoState) {
	switch variant_value := value.(type) {
	case VtxoStateSpendable:
		writeInt32(writer, 1)
	case VtxoStateLocked:
		writeInt32(writer, 2)
		FfiConverterOptionalVtxoLockHolderINSTANCE.Write(writer, variant_value.Holder)
	case VtxoStateSpent:
		writeInt32(writer, 3)
	case VtxoStateExited:
		writeInt32(writer, 4)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterVtxoState.Write", value))
	}
}

type FfiDestroyerVtxoState struct{}

func (_ FfiDestroyerVtxoState) Destroy(value VtxoState) {
	value.Destroy()
}

// A notification event from the wallet
type WalletNotification interface {
	Destroy()
}

// A new movement was created
type WalletNotificationMovementCreated struct {
	Movement Movement
}

func (e WalletNotificationMovementCreated) Destroy() {
	FfiDestroyerMovement{}.Destroy(e.Movement)
}

// An existing movement was updated
type WalletNotificationMovementUpdated struct {
	Movement Movement
}

func (e WalletNotificationMovementUpdated) Destroy() {
	FfiDestroyerMovement{}.Destroy(e.Movement)
}

// The notification channel is lagging (notifications were dropped)
type WalletNotificationChannelLagging struct {
}

func (e WalletNotificationChannelLagging) Destroy() {
}

type FfiConverterWalletNotification struct{}

var FfiConverterWalletNotificationINSTANCE = FfiConverterWalletNotification{}

func (c FfiConverterWalletNotification) Lift(rb RustBufferI) WalletNotification {
	return LiftFromRustBuffer[WalletNotification](c, rb)
}

func (c FfiConverterWalletNotification) Lower(value WalletNotification) C.RustBuffer {
	return LowerIntoRustBuffer[WalletNotification](c, value)
}

func (c FfiConverterWalletNotification) LowerExternal(value WalletNotification) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[WalletNotification](c, value))
}
func (FfiConverterWalletNotification) Read(reader io.Reader) WalletNotification {
	id := readInt32(reader)
	switch id {
	case 1:
		return WalletNotificationMovementCreated{
			FfiConverterMovementINSTANCE.Read(reader),
		}
	case 2:
		return WalletNotificationMovementUpdated{
			FfiConverterMovementINSTANCE.Read(reader),
		}
	case 3:
		return WalletNotificationChannelLagging{}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterWalletNotification.Read()", id))
	}
}

func (FfiConverterWalletNotification) Write(writer io.Writer, value WalletNotification) {
	switch variant_value := value.(type) {
	case WalletNotificationMovementCreated:
		writeInt32(writer, 1)
		FfiConverterMovementINSTANCE.Write(writer, variant_value.Movement)
	case WalletNotificationMovementUpdated:
		writeInt32(writer, 2)
		FfiConverterMovementINSTANCE.Write(writer, variant_value.Movement)
	case WalletNotificationChannelLagging:
		writeInt32(writer, 3)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterWalletNotification.Write", value))
	}
}

type FfiDestroyerWalletNotification struct{}

func (_ FfiDestroyerWalletNotification) Destroy(value WalletNotification) {
	value.Destroy()
}

type FfiConverterOptionalUint8 struct{}

var FfiConverterOptionalUint8INSTANCE = FfiConverterOptionalUint8{}

func (c FfiConverterOptionalUint8) Lift(rb RustBufferI) *uint8 {
	return LiftFromRustBuffer[*uint8](c, rb)
}

func (_ FfiConverterOptionalUint8) Read(reader io.Reader) *uint8 {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterUint8INSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalUint8) Lower(value *uint8) C.RustBuffer {
	return LowerIntoRustBuffer[*uint8](c, value)
}

func (c FfiConverterOptionalUint8) LowerExternal(value *uint8) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*uint8](c, value))
}

func (_ FfiConverterOptionalUint8) Write(writer io.Writer, value *uint8) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterUint8INSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalUint8 struct{}

func (_ FfiDestroyerOptionalUint8) Destroy(value *uint8) {
	if value != nil {
		FfiDestroyerUint8{}.Destroy(*value)
	}
}

type FfiConverterOptionalUint16 struct{}

var FfiConverterOptionalUint16INSTANCE = FfiConverterOptionalUint16{}

func (c FfiConverterOptionalUint16) Lift(rb RustBufferI) *uint16 {
	return LiftFromRustBuffer[*uint16](c, rb)
}

func (_ FfiConverterOptionalUint16) Read(reader io.Reader) *uint16 {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterUint16INSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalUint16) Lower(value *uint16) C.RustBuffer {
	return LowerIntoRustBuffer[*uint16](c, value)
}

func (c FfiConverterOptionalUint16) LowerExternal(value *uint16) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*uint16](c, value))
}

func (_ FfiConverterOptionalUint16) Write(writer io.Writer, value *uint16) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterUint16INSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalUint16 struct{}

func (_ FfiDestroyerOptionalUint16) Destroy(value *uint16) {
	if value != nil {
		FfiDestroyerUint16{}.Destroy(*value)
	}
}

type FfiConverterOptionalUint32 struct{}

var FfiConverterOptionalUint32INSTANCE = FfiConverterOptionalUint32{}

func (c FfiConverterOptionalUint32) Lift(rb RustBufferI) *uint32 {
	return LiftFromRustBuffer[*uint32](c, rb)
}

func (_ FfiConverterOptionalUint32) Read(reader io.Reader) *uint32 {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterUint32INSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalUint32) Lower(value *uint32) C.RustBuffer {
	return LowerIntoRustBuffer[*uint32](c, value)
}

func (c FfiConverterOptionalUint32) LowerExternal(value *uint32) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*uint32](c, value))
}

func (_ FfiConverterOptionalUint32) Write(writer io.Writer, value *uint32) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterUint32INSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalUint32 struct{}

func (_ FfiDestroyerOptionalUint32) Destroy(value *uint32) {
	if value != nil {
		FfiDestroyerUint32{}.Destroy(*value)
	}
}

type FfiConverterOptionalUint64 struct{}

var FfiConverterOptionalUint64INSTANCE = FfiConverterOptionalUint64{}

func (c FfiConverterOptionalUint64) Lift(rb RustBufferI) *uint64 {
	return LiftFromRustBuffer[*uint64](c, rb)
}

func (_ FfiConverterOptionalUint64) Read(reader io.Reader) *uint64 {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterUint64INSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalUint64) Lower(value *uint64) C.RustBuffer {
	return LowerIntoRustBuffer[*uint64](c, value)
}

func (c FfiConverterOptionalUint64) LowerExternal(value *uint64) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*uint64](c, value))
}

func (_ FfiConverterOptionalUint64) Write(writer io.Writer, value *uint64) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterUint64INSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalUint64 struct{}

func (_ FfiDestroyerOptionalUint64) Destroy(value *uint64) {
	if value != nil {
		FfiDestroyerUint64{}.Destroy(*value)
	}
}

type FfiConverterOptionalInt64 struct{}

var FfiConverterOptionalInt64INSTANCE = FfiConverterOptionalInt64{}

func (c FfiConverterOptionalInt64) Lift(rb RustBufferI) *int64 {
	return LiftFromRustBuffer[*int64](c, rb)
}

func (_ FfiConverterOptionalInt64) Read(reader io.Reader) *int64 {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterInt64INSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalInt64) Lower(value *int64) C.RustBuffer {
	return LowerIntoRustBuffer[*int64](c, value)
}

func (c FfiConverterOptionalInt64) LowerExternal(value *int64) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*int64](c, value))
}

func (_ FfiConverterOptionalInt64) Write(writer io.Writer, value *int64) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterInt64INSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalInt64 struct{}

func (_ FfiDestroyerOptionalInt64) Destroy(value *int64) {
	if value != nil {
		FfiDestroyerInt64{}.Destroy(*value)
	}
}

type FfiConverterOptionalBool struct{}

var FfiConverterOptionalBoolINSTANCE = FfiConverterOptionalBool{}

func (c FfiConverterOptionalBool) Lift(rb RustBufferI) *bool {
	return LiftFromRustBuffer[*bool](c, rb)
}

func (_ FfiConverterOptionalBool) Read(reader io.Reader) *bool {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterBoolINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalBool) Lower(value *bool) C.RustBuffer {
	return LowerIntoRustBuffer[*bool](c, value)
}

func (c FfiConverterOptionalBool) LowerExternal(value *bool) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*bool](c, value))
}

func (_ FfiConverterOptionalBool) Write(writer io.Writer, value *bool) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterBoolINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalBool struct{}

func (_ FfiDestroyerOptionalBool) Destroy(value *bool) {
	if value != nil {
		FfiDestroyerBool{}.Destroy(*value)
	}
}

type FfiConverterOptionalString struct{}

var FfiConverterOptionalStringINSTANCE = FfiConverterOptionalString{}

func (c FfiConverterOptionalString) Lift(rb RustBufferI) *string {
	return LiftFromRustBuffer[*string](c, rb)
}

func (_ FfiConverterOptionalString) Read(reader io.Reader) *string {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterStringINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalString) Lower(value *string) C.RustBuffer {
	return LowerIntoRustBuffer[*string](c, value)
}

func (c FfiConverterOptionalString) LowerExternal(value *string) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*string](c, value))
}

func (_ FfiConverterOptionalString) Write(writer io.Writer, value *string) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterStringINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalString struct{}

func (_ FfiDestroyerOptionalString) Destroy(value *string) {
	if value != nil {
		FfiDestroyerString{}.Destroy(*value)
	}
}

type FfiConverterOptionalOnchainWallet struct{}

var FfiConverterOptionalOnchainWalletINSTANCE = FfiConverterOptionalOnchainWallet{}

func (c FfiConverterOptionalOnchainWallet) Lift(rb RustBufferI) **OnchainWallet {
	return LiftFromRustBuffer[**OnchainWallet](c, rb)
}

func (_ FfiConverterOptionalOnchainWallet) Read(reader io.Reader) **OnchainWallet {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterOnchainWalletINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalOnchainWallet) Lower(value **OnchainWallet) C.RustBuffer {
	return LowerIntoRustBuffer[**OnchainWallet](c, value)
}

func (c FfiConverterOptionalOnchainWallet) LowerExternal(value **OnchainWallet) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[**OnchainWallet](c, value))
}

func (_ FfiConverterOptionalOnchainWallet) Write(writer io.Writer, value **OnchainWallet) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterOnchainWalletINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalOnchainWallet struct{}

func (_ FfiDestroyerOptionalOnchainWallet) Destroy(value **OnchainWallet) {
	if value != nil {
		FfiDestroyerOnchainWallet{}.Destroy(*value)
	}
}

type FfiConverterOptionalArkInfo struct{}

var FfiConverterOptionalArkInfoINSTANCE = FfiConverterOptionalArkInfo{}

func (c FfiConverterOptionalArkInfo) Lift(rb RustBufferI) *ArkInfo {
	return LiftFromRustBuffer[*ArkInfo](c, rb)
}

func (_ FfiConverterOptionalArkInfo) Read(reader io.Reader) *ArkInfo {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterArkInfoINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalArkInfo) Lower(value *ArkInfo) C.RustBuffer {
	return LowerIntoRustBuffer[*ArkInfo](c, value)
}

func (c FfiConverterOptionalArkInfo) LowerExternal(value *ArkInfo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*ArkInfo](c, value))
}

func (_ FfiConverterOptionalArkInfo) Write(writer io.Writer, value *ArkInfo) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterArkInfoINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalArkInfo struct{}

func (_ FfiDestroyerOptionalArkInfo) Destroy(value *ArkInfo) {
	if value != nil {
		FfiDestroyerArkInfo{}.Destroy(*value)
	}
}

type FfiConverterOptionalBlockRef struct{}

var FfiConverterOptionalBlockRefINSTANCE = FfiConverterOptionalBlockRef{}

func (c FfiConverterOptionalBlockRef) Lift(rb RustBufferI) *BlockRef {
	return LiftFromRustBuffer[*BlockRef](c, rb)
}

func (_ FfiConverterOptionalBlockRef) Read(reader io.Reader) *BlockRef {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterBlockRefINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalBlockRef) Lower(value *BlockRef) C.RustBuffer {
	return LowerIntoRustBuffer[*BlockRef](c, value)
}

func (c FfiConverterOptionalBlockRef) LowerExternal(value *BlockRef) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*BlockRef](c, value))
}

func (_ FfiConverterOptionalBlockRef) Write(writer io.Writer, value *BlockRef) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterBlockRefINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalBlockRef struct{}

func (_ FfiDestroyerOptionalBlockRef) Destroy(value *BlockRef) {
	if value != nil {
		FfiDestroyerBlockRef{}.Destroy(*value)
	}
}

type FfiConverterOptionalExitTransactionStatus struct{}

var FfiConverterOptionalExitTransactionStatusINSTANCE = FfiConverterOptionalExitTransactionStatus{}

func (c FfiConverterOptionalExitTransactionStatus) Lift(rb RustBufferI) *ExitTransactionStatus {
	return LiftFromRustBuffer[*ExitTransactionStatus](c, rb)
}

func (_ FfiConverterOptionalExitTransactionStatus) Read(reader io.Reader) *ExitTransactionStatus {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterExitTransactionStatusINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalExitTransactionStatus) Lower(value *ExitTransactionStatus) C.RustBuffer {
	return LowerIntoRustBuffer[*ExitTransactionStatus](c, value)
}

func (c FfiConverterOptionalExitTransactionStatus) LowerExternal(value *ExitTransactionStatus) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*ExitTransactionStatus](c, value))
}

func (_ FfiConverterOptionalExitTransactionStatus) Write(writer io.Writer, value *ExitTransactionStatus) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterExitTransactionStatusINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalExitTransactionStatus struct{}

func (_ FfiDestroyerOptionalExitTransactionStatus) Destroy(value *ExitTransactionStatus) {
	if value != nil {
		FfiDestroyerExitTransactionStatus{}.Destroy(*value)
	}
}

type FfiConverterOptionalImportVtxoArgs struct{}

var FfiConverterOptionalImportVtxoArgsINSTANCE = FfiConverterOptionalImportVtxoArgs{}

func (c FfiConverterOptionalImportVtxoArgs) Lift(rb RustBufferI) *ImportVtxoArgs {
	return LiftFromRustBuffer[*ImportVtxoArgs](c, rb)
}

func (_ FfiConverterOptionalImportVtxoArgs) Read(reader io.Reader) *ImportVtxoArgs {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterImportVtxoArgsINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalImportVtxoArgs) Lower(value *ImportVtxoArgs) C.RustBuffer {
	return LowerIntoRustBuffer[*ImportVtxoArgs](c, value)
}

func (c FfiConverterOptionalImportVtxoArgs) LowerExternal(value *ImportVtxoArgs) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*ImportVtxoArgs](c, value))
}

func (_ FfiConverterOptionalImportVtxoArgs) Write(writer io.Writer, value *ImportVtxoArgs) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterImportVtxoArgsINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalImportVtxoArgs struct{}

func (_ FfiDestroyerOptionalImportVtxoArgs) Destroy(value *ImportVtxoArgs) {
	if value != nil {
		FfiDestroyerImportVtxoArgs{}.Destroy(*value)
	}
}

type FfiConverterOptionalRecoveryReport struct{}

var FfiConverterOptionalRecoveryReportINSTANCE = FfiConverterOptionalRecoveryReport{}

func (c FfiConverterOptionalRecoveryReport) Lift(rb RustBufferI) *RecoveryReport {
	return LiftFromRustBuffer[*RecoveryReport](c, rb)
}

func (_ FfiConverterOptionalRecoveryReport) Read(reader io.Reader) *RecoveryReport {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterRecoveryReportINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalRecoveryReport) Lower(value *RecoveryReport) C.RustBuffer {
	return LowerIntoRustBuffer[*RecoveryReport](c, value)
}

func (c FfiConverterOptionalRecoveryReport) LowerExternal(value *RecoveryReport) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*RecoveryReport](c, value))
}

func (_ FfiConverterOptionalRecoveryReport) Write(writer io.Writer, value *RecoveryReport) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterRecoveryReportINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalRecoveryReport struct{}

func (_ FfiDestroyerOptionalRecoveryReport) Destroy(value *RecoveryReport) {
	if value != nil {
		FfiDestroyerRecoveryReport{}.Destroy(*value)
	}
}

type FfiConverterOptionalRoundState struct{}

var FfiConverterOptionalRoundStateINSTANCE = FfiConverterOptionalRoundState{}

func (c FfiConverterOptionalRoundState) Lift(rb RustBufferI) *RoundState {
	return LiftFromRustBuffer[*RoundState](c, rb)
}

func (_ FfiConverterOptionalRoundState) Read(reader io.Reader) *RoundState {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterRoundStateINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalRoundState) Lower(value *RoundState) C.RustBuffer {
	return LowerIntoRustBuffer[*RoundState](c, value)
}

func (c FfiConverterOptionalRoundState) LowerExternal(value *RoundState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*RoundState](c, value))
}

func (_ FfiConverterOptionalRoundState) Write(writer io.Writer, value *RoundState) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterRoundStateINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalRoundState struct{}

func (_ FfiDestroyerOptionalRoundState) Destroy(value *RoundState) {
	if value != nil {
		FfiDestroyerRoundState{}.Destroy(*value)
	}
}

type FfiConverterOptionalExitCancelFailure struct{}

var FfiConverterOptionalExitCancelFailureINSTANCE = FfiConverterOptionalExitCancelFailure{}

func (c FfiConverterOptionalExitCancelFailure) Lift(rb RustBufferI) *ExitCancelFailure {
	return LiftFromRustBuffer[*ExitCancelFailure](c, rb)
}

func (_ FfiConverterOptionalExitCancelFailure) Read(reader io.Reader) *ExitCancelFailure {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterExitCancelFailureINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalExitCancelFailure) Lower(value *ExitCancelFailure) C.RustBuffer {
	return LowerIntoRustBuffer[*ExitCancelFailure](c, value)
}

func (c FfiConverterOptionalExitCancelFailure) LowerExternal(value *ExitCancelFailure) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*ExitCancelFailure](c, value))
}

func (_ FfiConverterOptionalExitCancelFailure) Write(writer io.Writer, value *ExitCancelFailure) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterExitCancelFailureINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalExitCancelFailure struct{}

func (_ FfiDestroyerOptionalExitCancelFailure) Destroy(value *ExitCancelFailure) {
	if value != nil {
		FfiDestroyerExitCancelFailure{}.Destroy(*value)
	}
}

type FfiConverterOptionalVtxoLockHolder struct{}

var FfiConverterOptionalVtxoLockHolderINSTANCE = FfiConverterOptionalVtxoLockHolder{}

func (c FfiConverterOptionalVtxoLockHolder) Lift(rb RustBufferI) *VtxoLockHolder {
	return LiftFromRustBuffer[*VtxoLockHolder](c, rb)
}

func (_ FfiConverterOptionalVtxoLockHolder) Read(reader io.Reader) *VtxoLockHolder {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterVtxoLockHolderINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalVtxoLockHolder) Lower(value *VtxoLockHolder) C.RustBuffer {
	return LowerIntoRustBuffer[*VtxoLockHolder](c, value)
}

func (c FfiConverterOptionalVtxoLockHolder) LowerExternal(value *VtxoLockHolder) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*VtxoLockHolder](c, value))
}

func (_ FfiConverterOptionalVtxoLockHolder) Write(writer io.Writer, value *VtxoLockHolder) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterVtxoLockHolderINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalVtxoLockHolder struct{}

func (_ FfiDestroyerOptionalVtxoLockHolder) Destroy(value *VtxoLockHolder) {
	if value != nil {
		FfiDestroyerVtxoLockHolder{}.Destroy(*value)
	}
}

type FfiConverterOptionalWalletNotification struct{}

var FfiConverterOptionalWalletNotificationINSTANCE = FfiConverterOptionalWalletNotification{}

func (c FfiConverterOptionalWalletNotification) Lift(rb RustBufferI) *WalletNotification {
	return LiftFromRustBuffer[*WalletNotification](c, rb)
}

func (_ FfiConverterOptionalWalletNotification) Read(reader io.Reader) *WalletNotification {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterWalletNotificationINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalWalletNotification) Lower(value *WalletNotification) C.RustBuffer {
	return LowerIntoRustBuffer[*WalletNotification](c, value)
}

func (c FfiConverterOptionalWalletNotification) LowerExternal(value *WalletNotification) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*WalletNotification](c, value))
}

func (_ FfiConverterOptionalWalletNotification) Write(writer io.Writer, value *WalletNotification) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterWalletNotificationINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalWalletNotification struct{}

func (_ FfiDestroyerOptionalWalletNotification) Destroy(value *WalletNotification) {
	if value != nil {
		FfiDestroyerWalletNotification{}.Destroy(*value)
	}
}

type FfiConverterOptionalSequenceExitState struct{}

var FfiConverterOptionalSequenceExitStateINSTANCE = FfiConverterOptionalSequenceExitState{}

func (c FfiConverterOptionalSequenceExitState) Lift(rb RustBufferI) *[]ExitState {
	return LiftFromRustBuffer[*[]ExitState](c, rb)
}

func (_ FfiConverterOptionalSequenceExitState) Read(reader io.Reader) *[]ExitState {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterSequenceExitStateINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalSequenceExitState) Lower(value *[]ExitState) C.RustBuffer {
	return LowerIntoRustBuffer[*[]ExitState](c, value)
}

func (c FfiConverterOptionalSequenceExitState) LowerExternal(value *[]ExitState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*[]ExitState](c, value))
}

func (_ FfiConverterOptionalSequenceExitState) Write(writer io.Writer, value *[]ExitState) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterSequenceExitStateINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalSequenceExitState struct{}

func (_ FfiDestroyerOptionalSequenceExitState) Destroy(value *[]ExitState) {
	if value != nil {
		FfiDestroyerSequenceExitState{}.Destroy(*value)
	}
}

type FfiConverterSequenceString struct{}

var FfiConverterSequenceStringINSTANCE = FfiConverterSequenceString{}

func (c FfiConverterSequenceString) Lift(rb RustBufferI) []string {
	return LiftFromRustBuffer[[]string](c, rb)
}

func (c FfiConverterSequenceString) Read(reader io.Reader) []string {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]string, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterStringINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceString) Lower(value []string) C.RustBuffer {
	return LowerIntoRustBuffer[[]string](c, value)
}

func (c FfiConverterSequenceString) LowerExternal(value []string) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]string](c, value))
}

func (c FfiConverterSequenceString) Write(writer io.Writer, value []string) {
	if len(value) > math.MaxInt32 {
		panic("[]string is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterStringINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceString struct{}

func (FfiDestroyerSequenceString) Destroy(sequence []string) {
	for _, value := range sequence {
		FfiDestroyerString{}.Destroy(value)
	}
}

type FfiConverterSequenceDestination struct{}

var FfiConverterSequenceDestinationINSTANCE = FfiConverterSequenceDestination{}

func (c FfiConverterSequenceDestination) Lift(rb RustBufferI) []Destination {
	return LiftFromRustBuffer[[]Destination](c, rb)
}

func (c FfiConverterSequenceDestination) Read(reader io.Reader) []Destination {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Destination, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterDestinationINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceDestination) Lower(value []Destination) C.RustBuffer {
	return LowerIntoRustBuffer[[]Destination](c, value)
}

func (c FfiConverterSequenceDestination) LowerExternal(value []Destination) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]Destination](c, value))
}

func (c FfiConverterSequenceDestination) Write(writer io.Writer, value []Destination) {
	if len(value) > math.MaxInt32 {
		panic("[]Destination is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterDestinationINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceDestination struct{}

func (FfiDestroyerSequenceDestination) Destroy(sequence []Destination) {
	for _, value := range sequence {
		FfiDestroyerDestination{}.Destroy(value)
	}
}

type FfiConverterSequenceExitProgressStatus struct{}

var FfiConverterSequenceExitProgressStatusINSTANCE = FfiConverterSequenceExitProgressStatus{}

func (c FfiConverterSequenceExitProgressStatus) Lift(rb RustBufferI) []ExitProgressStatus {
	return LiftFromRustBuffer[[]ExitProgressStatus](c, rb)
}

func (c FfiConverterSequenceExitProgressStatus) Read(reader io.Reader) []ExitProgressStatus {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]ExitProgressStatus, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterExitProgressStatusINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceExitProgressStatus) Lower(value []ExitProgressStatus) C.RustBuffer {
	return LowerIntoRustBuffer[[]ExitProgressStatus](c, value)
}

func (c FfiConverterSequenceExitProgressStatus) LowerExternal(value []ExitProgressStatus) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]ExitProgressStatus](c, value))
}

func (c FfiConverterSequenceExitProgressStatus) Write(writer io.Writer, value []ExitProgressStatus) {
	if len(value) > math.MaxInt32 {
		panic("[]ExitProgressStatus is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterExitProgressStatusINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceExitProgressStatus struct{}

func (FfiDestroyerSequenceExitProgressStatus) Destroy(sequence []ExitProgressStatus) {
	for _, value := range sequence {
		FfiDestroyerExitProgressStatus{}.Destroy(value)
	}
}

type FfiConverterSequenceExitTx struct{}

var FfiConverterSequenceExitTxINSTANCE = FfiConverterSequenceExitTx{}

func (c FfiConverterSequenceExitTx) Lift(rb RustBufferI) []ExitTx {
	return LiftFromRustBuffer[[]ExitTx](c, rb)
}

func (c FfiConverterSequenceExitTx) Read(reader io.Reader) []ExitTx {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]ExitTx, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterExitTxINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceExitTx) Lower(value []ExitTx) C.RustBuffer {
	return LowerIntoRustBuffer[[]ExitTx](c, value)
}

func (c FfiConverterSequenceExitTx) LowerExternal(value []ExitTx) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]ExitTx](c, value))
}

func (c FfiConverterSequenceExitTx) Write(writer io.Writer, value []ExitTx) {
	if len(value) > math.MaxInt32 {
		panic("[]ExitTx is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterExitTxINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceExitTx struct{}

func (FfiDestroyerSequenceExitTx) Destroy(sequence []ExitTx) {
	for _, value := range sequence {
		FfiDestroyerExitTx{}.Destroy(value)
	}
}

type FfiConverterSequenceExitVtxo struct{}

var FfiConverterSequenceExitVtxoINSTANCE = FfiConverterSequenceExitVtxo{}

func (c FfiConverterSequenceExitVtxo) Lift(rb RustBufferI) []ExitVtxo {
	return LiftFromRustBuffer[[]ExitVtxo](c, rb)
}

func (c FfiConverterSequenceExitVtxo) Read(reader io.Reader) []ExitVtxo {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]ExitVtxo, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterExitVtxoINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceExitVtxo) Lower(value []ExitVtxo) C.RustBuffer {
	return LowerIntoRustBuffer[[]ExitVtxo](c, value)
}

func (c FfiConverterSequenceExitVtxo) LowerExternal(value []ExitVtxo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]ExitVtxo](c, value))
}

func (c FfiConverterSequenceExitVtxo) Write(writer io.Writer, value []ExitVtxo) {
	if len(value) > math.MaxInt32 {
		panic("[]ExitVtxo is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterExitVtxoINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceExitVtxo struct{}

func (FfiDestroyerSequenceExitVtxo) Destroy(sequence []ExitVtxo) {
	for _, value := range sequence {
		FfiDestroyerExitVtxo{}.Destroy(value)
	}
}

type FfiConverterSequenceLightningReceive struct{}

var FfiConverterSequenceLightningReceiveINSTANCE = FfiConverterSequenceLightningReceive{}

func (c FfiConverterSequenceLightningReceive) Lift(rb RustBufferI) []LightningReceive {
	return LiftFromRustBuffer[[]LightningReceive](c, rb)
}

func (c FfiConverterSequenceLightningReceive) Read(reader io.Reader) []LightningReceive {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]LightningReceive, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterLightningReceiveINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceLightningReceive) Lower(value []LightningReceive) C.RustBuffer {
	return LowerIntoRustBuffer[[]LightningReceive](c, value)
}

func (c FfiConverterSequenceLightningReceive) LowerExternal(value []LightningReceive) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]LightningReceive](c, value))
}

func (c FfiConverterSequenceLightningReceive) Write(writer io.Writer, value []LightningReceive) {
	if len(value) > math.MaxInt32 {
		panic("[]LightningReceive is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterLightningReceiveINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceLightningReceive struct{}

func (FfiDestroyerSequenceLightningReceive) Destroy(sequence []LightningReceive) {
	for _, value := range sequence {
		FfiDestroyerLightningReceive{}.Destroy(value)
	}
}

type FfiConverterSequenceLightningSend struct{}

var FfiConverterSequenceLightningSendINSTANCE = FfiConverterSequenceLightningSend{}

func (c FfiConverterSequenceLightningSend) Lift(rb RustBufferI) []LightningSend {
	return LiftFromRustBuffer[[]LightningSend](c, rb)
}

func (c FfiConverterSequenceLightningSend) Read(reader io.Reader) []LightningSend {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]LightningSend, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterLightningSendINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceLightningSend) Lower(value []LightningSend) C.RustBuffer {
	return LowerIntoRustBuffer[[]LightningSend](c, value)
}

func (c FfiConverterSequenceLightningSend) LowerExternal(value []LightningSend) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]LightningSend](c, value))
}

func (c FfiConverterSequenceLightningSend) Write(writer io.Writer, value []LightningSend) {
	if len(value) > math.MaxInt32 {
		panic("[]LightningSend is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterLightningSendINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceLightningSend struct{}

func (FfiDestroyerSequenceLightningSend) Destroy(sequence []LightningSend) {
	for _, value := range sequence {
		FfiDestroyerLightningSend{}.Destroy(value)
	}
}

type FfiConverterSequenceMovement struct{}

var FfiConverterSequenceMovementINSTANCE = FfiConverterSequenceMovement{}

func (c FfiConverterSequenceMovement) Lift(rb RustBufferI) []Movement {
	return LiftFromRustBuffer[[]Movement](c, rb)
}

func (c FfiConverterSequenceMovement) Read(reader io.Reader) []Movement {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Movement, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterMovementINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceMovement) Lower(value []Movement) C.RustBuffer {
	return LowerIntoRustBuffer[[]Movement](c, value)
}

func (c FfiConverterSequenceMovement) LowerExternal(value []Movement) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]Movement](c, value))
}

func (c FfiConverterSequenceMovement) Write(writer io.Writer, value []Movement) {
	if len(value) > math.MaxInt32 {
		panic("[]Movement is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterMovementINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceMovement struct{}

func (FfiDestroyerSequenceMovement) Destroy(sequence []Movement) {
	for _, value := range sequence {
		FfiDestroyerMovement{}.Destroy(value)
	}
}

type FfiConverterSequencePendingBoard struct{}

var FfiConverterSequencePendingBoardINSTANCE = FfiConverterSequencePendingBoard{}

func (c FfiConverterSequencePendingBoard) Lift(rb RustBufferI) []PendingBoard {
	return LiftFromRustBuffer[[]PendingBoard](c, rb)
}

func (c FfiConverterSequencePendingBoard) Read(reader io.Reader) []PendingBoard {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]PendingBoard, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterPendingBoardINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequencePendingBoard) Lower(value []PendingBoard) C.RustBuffer {
	return LowerIntoRustBuffer[[]PendingBoard](c, value)
}

func (c FfiConverterSequencePendingBoard) LowerExternal(value []PendingBoard) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]PendingBoard](c, value))
}

func (c FfiConverterSequencePendingBoard) Write(writer io.Writer, value []PendingBoard) {
	if len(value) > math.MaxInt32 {
		panic("[]PendingBoard is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterPendingBoardINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequencePendingBoard struct{}

func (FfiDestroyerSequencePendingBoard) Destroy(sequence []PendingBoard) {
	for _, value := range sequence {
		FfiDestroyerPendingBoard{}.Destroy(value)
	}
}

type FfiConverterSequencePpmExpiryFeeEntry struct{}

var FfiConverterSequencePpmExpiryFeeEntryINSTANCE = FfiConverterSequencePpmExpiryFeeEntry{}

func (c FfiConverterSequencePpmExpiryFeeEntry) Lift(rb RustBufferI) []PpmExpiryFeeEntry {
	return LiftFromRustBuffer[[]PpmExpiryFeeEntry](c, rb)
}

func (c FfiConverterSequencePpmExpiryFeeEntry) Read(reader io.Reader) []PpmExpiryFeeEntry {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]PpmExpiryFeeEntry, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterPpmExpiryFeeEntryINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequencePpmExpiryFeeEntry) Lower(value []PpmExpiryFeeEntry) C.RustBuffer {
	return LowerIntoRustBuffer[[]PpmExpiryFeeEntry](c, value)
}

func (c FfiConverterSequencePpmExpiryFeeEntry) LowerExternal(value []PpmExpiryFeeEntry) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]PpmExpiryFeeEntry](c, value))
}

func (c FfiConverterSequencePpmExpiryFeeEntry) Write(writer io.Writer, value []PpmExpiryFeeEntry) {
	if len(value) > math.MaxInt32 {
		panic("[]PpmExpiryFeeEntry is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterPpmExpiryFeeEntryINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequencePpmExpiryFeeEntry struct{}

func (FfiDestroyerSequencePpmExpiryFeeEntry) Destroy(sequence []PpmExpiryFeeEntry) {
	for _, value := range sequence {
		FfiDestroyerPpmExpiryFeeEntry{}.Destroy(value)
	}
}

type FfiConverterSequenceRoundState struct{}

var FfiConverterSequenceRoundStateINSTANCE = FfiConverterSequenceRoundState{}

func (c FfiConverterSequenceRoundState) Lift(rb RustBufferI) []RoundState {
	return LiftFromRustBuffer[[]RoundState](c, rb)
}

func (c FfiConverterSequenceRoundState) Read(reader io.Reader) []RoundState {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]RoundState, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterRoundStateINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceRoundState) Lower(value []RoundState) C.RustBuffer {
	return LowerIntoRustBuffer[[]RoundState](c, value)
}

func (c FfiConverterSequenceRoundState) LowerExternal(value []RoundState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]RoundState](c, value))
}

func (c FfiConverterSequenceRoundState) Write(writer io.Writer, value []RoundState) {
	if len(value) > math.MaxInt32 {
		panic("[]RoundState is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterRoundStateINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceRoundState struct{}

func (FfiDestroyerSequenceRoundState) Destroy(sequence []RoundState) {
	for _, value := range sequence {
		FfiDestroyerRoundState{}.Destroy(value)
	}
}

type FfiConverterSequenceVtxo struct{}

var FfiConverterSequenceVtxoINSTANCE = FfiConverterSequenceVtxo{}

func (c FfiConverterSequenceVtxo) Lift(rb RustBufferI) []Vtxo {
	return LiftFromRustBuffer[[]Vtxo](c, rb)
}

func (c FfiConverterSequenceVtxo) Read(reader io.Reader) []Vtxo {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Vtxo, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterVtxoINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceVtxo) Lower(value []Vtxo) C.RustBuffer {
	return LowerIntoRustBuffer[[]Vtxo](c, value)
}

func (c FfiConverterSequenceVtxo) LowerExternal(value []Vtxo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]Vtxo](c, value))
}

func (c FfiConverterSequenceVtxo) Write(writer io.Writer, value []Vtxo) {
	if len(value) > math.MaxInt32 {
		panic("[]Vtxo is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterVtxoINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceVtxo struct{}

func (FfiDestroyerSequenceVtxo) Destroy(sequence []Vtxo) {
	for _, value := range sequence {
		FfiDestroyerVtxo{}.Destroy(value)
	}
}

type FfiConverterSequenceWalletTransaction struct{}

var FfiConverterSequenceWalletTransactionINSTANCE = FfiConverterSequenceWalletTransaction{}

func (c FfiConverterSequenceWalletTransaction) Lift(rb RustBufferI) []WalletTransaction {
	return LiftFromRustBuffer[[]WalletTransaction](c, rb)
}

func (c FfiConverterSequenceWalletTransaction) Read(reader io.Reader) []WalletTransaction {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]WalletTransaction, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterWalletTransactionINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceWalletTransaction) Lower(value []WalletTransaction) C.RustBuffer {
	return LowerIntoRustBuffer[[]WalletTransaction](c, value)
}

func (c FfiConverterSequenceWalletTransaction) LowerExternal(value []WalletTransaction) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]WalletTransaction](c, value))
}

func (c FfiConverterSequenceWalletTransaction) Write(writer io.Writer, value []WalletTransaction) {
	if len(value) > math.MaxInt32 {
		panic("[]WalletTransaction is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterWalletTransactionINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceWalletTransaction struct{}

func (FfiDestroyerSequenceWalletTransaction) Destroy(sequence []WalletTransaction) {
	for _, value := range sequence {
		FfiDestroyerWalletTransaction{}.Destroy(value)
	}
}

type FfiConverterSequenceExitState struct{}

var FfiConverterSequenceExitStateINSTANCE = FfiConverterSequenceExitState{}

func (c FfiConverterSequenceExitState) Lift(rb RustBufferI) []ExitState {
	return LiftFromRustBuffer[[]ExitState](c, rb)
}

func (c FfiConverterSequenceExitState) Read(reader io.Reader) []ExitState {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]ExitState, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterExitStateINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceExitState) Lower(value []ExitState) C.RustBuffer {
	return LowerIntoRustBuffer[[]ExitState](c, value)
}

func (c FfiConverterSequenceExitState) LowerExternal(value []ExitState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]ExitState](c, value))
}

func (c FfiConverterSequenceExitState) Write(writer io.Writer, value []ExitState) {
	if len(value) > math.MaxInt32 {
		panic("[]ExitState is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterExitStateINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceExitState struct{}

func (FfiDestroyerSequenceExitState) Destroy(sequence []ExitState) {
	for _, value := range sequence {
		FfiDestroyerExitState{}.Destroy(value)
	}
}

type FfiConverterSequenceOnchainUtxo struct{}

var FfiConverterSequenceOnchainUtxoINSTANCE = FfiConverterSequenceOnchainUtxo{}

func (c FfiConverterSequenceOnchainUtxo) Lift(rb RustBufferI) []OnchainUtxo {
	return LiftFromRustBuffer[[]OnchainUtxo](c, rb)
}

func (c FfiConverterSequenceOnchainUtxo) Read(reader io.Reader) []OnchainUtxo {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]OnchainUtxo, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterOnchainUtxoINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceOnchainUtxo) Lower(value []OnchainUtxo) C.RustBuffer {
	return LowerIntoRustBuffer[[]OnchainUtxo](c, value)
}

func (c FfiConverterSequenceOnchainUtxo) LowerExternal(value []OnchainUtxo) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]OnchainUtxo](c, value))
}

func (c FfiConverterSequenceOnchainUtxo) Write(writer io.Writer, value []OnchainUtxo) {
	if len(value) > math.MaxInt32 {
		panic("[]OnchainUtxo is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterOnchainUtxoINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceOnchainUtxo struct{}

func (FfiDestroyerSequenceOnchainUtxo) Destroy(sequence []OnchainUtxo) {
	for _, value := range sequence {
		FfiDestroyerOnchainUtxo{}.Destroy(value)
	}
}

const (
	uniffiRustFuturePollReady      int8 = 0
	uniffiRustFuturePollMaybeReady int8 = 1
)

type rustFuturePollFunc func(C.uint64_t, C.UniffiRustFutureContinuationCallback, C.uint64_t)
type rustFutureCompleteFunc[T any] func(C.uint64_t, *C.RustCallStatus) T
type rustFutureFreeFunc func(C.uint64_t)

//export bark_uniffiFutureContinuationCallback
func bark_uniffiFutureContinuationCallback(data C.uint64_t, pollResult C.int8_t) {
	h := cgo.Handle(uintptr(data))
	waiter := h.Value().(chan int8)
	waiter <- int8(pollResult)
}

func uniffiRustCallAsync[E any, T any, F any](
	errConverter BufReader[E],
	completeFunc rustFutureCompleteFunc[F],
	liftFunc func(F) T,
	rustFuture C.uint64_t,
	pollFunc rustFuturePollFunc,
	freeFunc rustFutureFreeFunc,
) (T, E) {
	defer freeFunc(rustFuture)

	pollResult := int8(-1)
	waiter := make(chan int8, 1)

	chanHandle := cgo.NewHandle(waiter)
	defer chanHandle.Delete()

	for pollResult != uniffiRustFuturePollReady {
		pollFunc(
			rustFuture,
			(C.UniffiRustFutureContinuationCallback)(C.bark_uniffiFutureContinuationCallback),
			C.uint64_t(chanHandle),
		)
		pollResult = <-waiter
	}

	var goValue T
	ffiValue, err := rustCallWithError(errConverter, func(status *C.RustCallStatus) F {
		return completeFunc(rustFuture, status)
	})
	if value := reflect.ValueOf(err); value.IsValid() && !value.IsZero() {
		return goValue, err
	}
	return liftFunc(ffiValue), err
}

//export bark_uniffiFreeGorutine
func bark_uniffiFreeGorutine(data C.uint64_t) {
	handle := cgo.Handle(uintptr(data))
	defer handle.Delete()

	guard := handle.Value().(chan struct{})
	guard <- struct{}{}
}

// Default for `Config.vtxo_key_gap_limit`: the run of consecutive unused
// seed-derived VTXO key indices a scan crosses before concluding a VTXO
// isn't ours.
func DefaultVtxoKeyGapLimit() uint32 {
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_bark_ffi_fn_func_default_vtxo_key_gap_limit(_uniffiStatus)
	}))
}

// Extract a signed transaction from a PSBT
//
// Takes a base64-encoded PSBT and extracts the final signed transaction.
// This is useful after signing a PSBT (e.g., from drain_exits) before broadcasting.
//
// # Arguments
//
// * `psbt_base64` - Base64-encoded PSBT string
//
// # Returns
//
// Hex-encoded signed transaction ready for broadcasting
func ExtractTxFromPsbt(psbtBase64 string) (string, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_func_extract_tx_from_psbt(FfiConverterStringINSTANCE.Lower(psbtBase64), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Generate a new 12-word BIP39 mnemonic
func GenerateMnemonic() (string, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bark_ffi_fn_func_generate_mnemonic(_uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Largest value `Config.vtxo_key_gap_limit` (or an import/recovery
// `gap_limit` override) accepts. A scan that matches nothing runs the limit
// to its end, so an unbounded limit is unbounded work.
func MaxVtxoKeyGapLimit() uint32 {
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_bark_ffi_fn_func_max_vtxo_key_gap_limit(_uniffiStatus)
	}))
}

// Validate an Ark address (basic format check only)
//
// This only validates the format of the address, not whether it belongs
// to a specific Ark server. For full validation against a connected server,
// use Wallet::validate_arkoor_address() instead.
func ValidateArkAddress(address string) (bool, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_bark_ffi_fn_func_validate_ark_address(FfiConverterStringINSTANCE.Lower(address), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue bool
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBoolINSTANCE.Lift(_uniffiRV), nil
	}
}

// Validate a BIP39 mnemonic phrase
func ValidateMnemonic(mnemonic string) (bool, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_bark_ffi_fn_func_validate_mnemonic(FfiConverterStringINSTANCE.Lower(mnemonic), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue bool
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBoolINSTANCE.Lift(_uniffiRV), nil
	}
}

// Install the foreign logger.
//
// On first call, installs the bridge with `log::set_logger`. Subsequent calls
// will return an error.
func SetLogger(logger BarkLogger, maxLevel LogLevel) error {
	_, _uniffiErr := rustCallWithError[*Error](FfiConverterError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bark_ffi_fn_func_set_logger(FfiConverterBarkLoggerINSTANCE.Lower(logger), FfiConverterLogLevelINSTANCE.Lower(maxLevel), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Low-level function to initialize a wallet
//
// You probably want to use [`Wallet::open`] instead.
func InitWallet(network Network, mnemonicOrSeed string, config Config, datadir string, allowUnreachableServer bool) error {
	_, err := uniffiRustCallAsync[*Error](
		FfiConverterErrorINSTANCE,
		// completeFn
		func(handle C.uint64_t, status *C.RustCallStatus) struct{} {
			C.ffi_bark_ffi_rust_future_complete_void(handle, status)
			return struct{}{}
		},
		// liftFn
		func(_ struct{}) struct{} { return struct{}{} },
		C.uniffi_bark_ffi_fn_func_init_wallet(FfiConverterNetworkINSTANCE.Lower(network), FfiConverterStringINSTANCE.Lower(mnemonicOrSeed), FfiConverterConfigINSTANCE.Lower(config), FfiConverterStringINSTANCE.Lower(datadir), FfiConverterBoolINSTANCE.Lower(allowUnreachableServer)),
		// pollFn
		func(handle C.uint64_t, continuation C.UniffiRustFutureContinuationCallback, data C.uint64_t) {
			C.ffi_bark_ffi_rust_future_poll_void(handle, continuation, data)
		},
		// freeFn
		func(handle C.uint64_t) {
			C.ffi_bark_ffi_rust_future_free_void(handle)
		},
	)

	if err == nil {
		return nil
	}

	return err
}
