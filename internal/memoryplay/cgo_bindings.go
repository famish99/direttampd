package memoryplay

/*
#cgo CFLAGS: -I${SRCDIR}/../../MemoryPlayController
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/../../MemoryPlayController/libmemoryplaycontroller.a ${SRCDIR}/../../MemoryPlayController/libDirettaHost_x64-linux-15v2.a ${SRCDIR}/../../MemoryPlayController/libACQUA_x64-linux-15v2.a -lstdc++ -lm -lpthread
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/../../MemoryPlayController/libmemoryplaycontroller.a ${SRCDIR}/../../MemoryPlayController/libDirettaHost_aarch64-linux-musl15.a ${SRCDIR}/../../MemoryPlayController/libACQUA_aarch64-linux-musl15.a -lstdc++ -lm -lpthread
#include "lib_memory_play_controller.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// InitLibrary initializes the MemoryPlay controller library
func InitLibrary(enableLogging, verboseMode bool) error {
	config := C.MPCConfig{
		enable_logging: C.bool(enableLogging),
		verbose_mode:   C.bool(verboseMode),
	}

	ret := C.mpc_init(&config)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_init failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// CleanupLibrary releases library resources
func CleanupLibrary() {
	C.mpc_cleanup()
}

// HostInfo represents a discovered MemoryPlay host
type HostInfo struct {
	IPAddress       string
	InterfaceNumber uint32
	TargetName      string
	OutputName      string
	IsLoopback      bool
}

// ListHosts discovers available MemoryPlay hosts on the network
func ListHosts() ([]HostInfo, error) {
	var hostList *C.MPCHostList
	ret := C.mpc_list_hosts(&hostList)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_list_hosts failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	defer C.mpc_free_host_list(hostList)

	if hostList == nil || hostList.count == 0 {
		return []HostInfo{}, nil
	}

	// Convert C array to Go slice
	hosts := make([]HostInfo, hostList.count)
	cHosts := (*[1 << 30]C.MPCHostInfo)(unsafe.Pointer(hostList.hosts))[:hostList.count:hostList.count]

	for i, ch := range cHosts {
		hosts[i] = HostInfo{
			IPAddress:       C.GoString(&ch.ip_address[0]),
			InterfaceNumber: uint32(ch.interface_number),
			TargetName:      C.GoString(&ch.target_name[0]),
			OutputName:      C.GoString(&ch.output_name[0]),
			IsLoopback:      bool(ch.is_loopback),
		}
	}

	return hosts, nil
}

// TargetInfo represents a Diretta target device
type TargetInfo struct {
	IPAddress       string
	InterfaceNumber uint32
	TargetName      string
}

// ListTargets lists available Diretta targets from a host
func ListTargets(hostAddress string, interfaceNumber uint32) ([]TargetInfo, error) {
	cHostAddr := C.CString(hostAddress)
	defer C.free(unsafe.Pointer(cHostAddr))

	var targetList *C.MPCTargetList
	ret := C.mpc_list_targets(cHostAddr, C.uint32_t(interfaceNumber), &targetList)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_list_targets failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	defer C.mpc_free_target_list(targetList)

	if targetList == nil || targetList.count == 0 {
		return []TargetInfo{}, nil
	}

	targets := make([]TargetInfo, targetList.count)
	cTargets := (*[1 << 30]C.MPCTargetInfo)(unsafe.Pointer(targetList.targets))[:targetList.count:targetList.count]

	for i, ct := range cTargets {
		targets[i] = TargetInfo{
			IPAddress:       C.GoString(&ct.ip_address[0]),
			InterfaceNumber: uint32(ct.interface_number),
			TargetName:      C.GoString(&ct.target_name[0]),
		}
	}

	return targets, nil
}

// WavFile represents an opened audio file
type WavFile struct {
	handle C.MPCWavHandle
}

// OpenWavFile opens an audio file (WAV/FLAC/DSF/DFF/AIFF)
func OpenWavFile(filename string) (*WavFile, error) {
	cFilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cFilename))

	var handle C.MPCWavHandle
	ret := C.mpc_wav_open(cFilename, &handle)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_wav_open failed: %s", C.GoString(C.mpc_error_string(ret)))
	}

	return &WavFile{handle: handle}, nil
}

// Close closes the audio file
func (w *WavFile) Close() {
	if w.handle != nil {
		C.mpc_wav_close(w.handle)
		w.handle = nil
	}
}

// GetFormat returns the audio format handle
func (w *WavFile) GetFormat() (C.MPCFormatHandle, error) {
	var format C.MPCFormatHandle
	ret := C.mpc_wav_get_format(w.handle, &format)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_wav_get_format failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return format, nil
}

// FreeFormat releases a format handle
func FreeFormat(format C.MPCFormatHandle) {
	if format != nil {
		C.mpc_free_format(format)
	}
}

// GetTitle returns the title/metadata
func (w *WavFile) GetTitle() string {
	return C.GoString(C.mpc_wav_get_title(w.handle))
}

// GetIndex returns the track index
func (w *WavFile) GetIndex() int {
	return int(C.mpc_wav_get_index(w.handle))
}

// UploadAudio uploads audio files to a MemoryPlay host
func UploadAudio(hostAddress string, interfaceNumber uint32, wavFiles []*WavFile, format C.MPCFormatHandle, loopMode bool) error {
	if len(wavFiles) == 0 {
		return fmt.Errorf("no audio files provided")
	}

	cHostAddr := C.CString(hostAddress)
	defer C.free(unsafe.Pointer(cHostAddr))

	// Convert Go slice to C array
	cHandles := make([]C.MPCWavHandle, len(wavFiles))
	for i, wf := range wavFiles {
		cHandles[i] = wf.handle
	}

	ret := C.mpc_upload_audio(
		cHostAddr,
		C.uint32_t(interfaceNumber),
		&cHandles[0],
		C.size_t(len(wavFiles)),
		format,
		C.bool(loopMode),
	)

	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_upload_audio failed: %s", C.GoString(C.mpc_error_string(ret)))
	}

	return nil
}
