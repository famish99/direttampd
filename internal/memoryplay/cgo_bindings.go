package memoryplay

/*
#cgo CFLAGS: -I${SRCDIR}/../../MemoryPlayController
#cgo LDFLAGS: ${SRCDIR}/../../MemoryPlayController/libmemoryplaycontroller.a ${SRCDIR}/../../MemoryPlayController/flac/src/libFLAC++/.libs/libFLAC++.a ${SRCDIR}/../../MemoryPlayController/flac/src/libFLAC/.libs/libFLAC.a ${SRCDIR}/../../MemoryPlayController/libFind_x64-linux-15v2.a ${SRCDIR}/../../MemoryPlayController/libACQUA_x64-linux-15v2.a -lstdc++ -lm -lpthread
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

// Session represents a control session to a MemoryPlay host
type Session struct {
	handle C.MPCSessionHandle
}

// CreateSession creates a control session
func CreateSession(hostAddress string, interfaceNumber uint32) (*Session, error) {
	cHostAddr := C.CString(hostAddress)
	defer C.free(unsafe.Pointer(cHostAddr))

	var handle C.MPCSessionHandle
	ret := C.mpc_session_create(cHostAddr, C.uint32_t(interfaceNumber), &handle)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_session_create failed: %s", C.GoString(C.mpc_error_string(ret)))
	}

	return &Session{handle: handle}, nil
}

// Close closes the session
func (s *Session) Close() {
	if s.handle != nil {
		C.mpc_session_close(s.handle)
		s.handle = nil
	}
}

// ConnectTarget connects to a specific Diretta target
func (s *Session) ConnectTarget(targetAddress string, interfaceNumber uint32) error {
	cTargetAddr := C.CString(targetAddress)
	defer C.free(unsafe.Pointer(cTargetAddr))

	ret := C.mpc_session_connect_target(s.handle, cTargetAddr, C.uint32_t(interfaceNumber))
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_connect_target failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Play starts or resumes playback
func (s *Session) Play() error {
	ret := C.mpc_session_play(s.handle)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_play failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Pause pauses playback
func (s *Session) Pause() error {
	ret := C.mpc_session_pause(s.handle)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_pause failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Seek seeks forward or backward by seconds
func (s *Session) Seek(offsetSeconds int64) error {
	ret := C.mpc_session_seek(s.handle, C.int64_t(offsetSeconds))
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_seek failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// SeekToStart seeks to the beginning
func (s *Session) SeekToStart() error {
	ret := C.mpc_session_seek_to_start(s.handle)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_seek_to_start failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// SeekAbsolute seeks to an absolute position in seconds
func (s *Session) SeekAbsolute(positionSeconds int64) error {
	ret := C.mpc_session_seek_absolute(s.handle, C.int64_t(positionSeconds))
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_seek_absolute failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Quit stops playback and disconnects
func (s *Session) Quit() error {
	ret := C.mpc_session_quit(s.handle)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_quit failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// PlaybackStatus represents playback status
type PlaybackStatus int

const (
	StatusDisconnected PlaybackStatus = 0
	StatusPlaying      PlaybackStatus = 1
	StatusPaused       PlaybackStatus = 2
)

// GetPlayStatus returns current playback status
func (s *Session) GetPlayStatus() (PlaybackStatus, error) {
	var status C.MPCPlaybackStatus
	ret := C.mpc_session_get_play_status(s.handle, &status)
	if ret != C.MPC_SUCCESS {
		return StatusDisconnected, fmt.Errorf("mpc_session_get_play_status failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return PlaybackStatus(status), nil
}

// GetCurrentTime returns current playback time in seconds
func (s *Session) GetCurrentTime() (int64, error) {
	var timeSeconds C.int64_t
	ret := C.mpc_session_get_current_time(s.handle, &timeSeconds)
	if ret != C.MPC_SUCCESS {
		return -1, fmt.Errorf("mpc_session_get_current_time failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return int64(timeSeconds), nil
}

// TagInfo represents a tag entry
type TagInfo struct {
	Tag string // Format: "INDEX:TIME:NAME"
}

// GetTagList returns the list of tags
func (s *Session) GetTagList() ([]TagInfo, error) {
	var tagList *C.MPCTagList
	ret := C.mpc_session_get_tag_list(s.handle, &tagList)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_session_get_tag_list failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	defer C.mpc_free_tag_list(tagList)

	if tagList == nil || tagList.count == 0 {
		return []TagInfo{}, nil
	}

	tags := make([]TagInfo, tagList.count)
	cTags := (*[1 << 30]C.MPCTagInfo)(unsafe.Pointer(tagList.tags))[:tagList.count:tagList.count]

	for i, ct := range cTags {
		tags[i] = TagInfo{
			Tag: C.GoString(&ct.tag[0]),
		}
	}

	return tags, nil
}